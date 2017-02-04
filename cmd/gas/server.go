package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	ErrNoName = errors.New("no task name given")
	ErrNoTask = errors.New("no such task")
)

type Args struct {
	Name string
	Args []string
}

type Response struct {
	Status string
	Tasks  []TaskStatus
}

type TaskStatus struct {
	Name    string
	Alive   bool
	PID     int
	Uptime  time.Duration
	Message string
	Enable  bool
	Port    string
}

func (ts TaskStatus) String() string {
	var (
		alive  = "×"
		pid    = "-"
		uptime = "-"
		port   = "-"
		name   = ts.Name
	)
	if ts.Alive {
		alive = "✓"
		pid = strconv.Itoa(ts.PID)
		uptime = fmtDuration(ts.Uptime)
		port = ts.Port
	}
	if !ts.Enable {
		name += " (disabled)"
	}
	return fmt.Sprintf("%s %s\t%s\t%s\t%s\t%s", alive, name, pid, port, uptime, ts.Message)
}

type TaskList struct {
	Tasks    []*Task
	mu       *sync.RWMutex
	taskChan chan interface{}
}

func (tl *TaskList) save(name string, ch chan<- *TaskStatus) {
	// TODO: add retry timer with backoff and eventually give up if failures
	// per time reaches a certain threshold (and maybe email somebody if that
	// happens)
	log.Printf("attempting to resuscitate %q in 5 seconds...", name)
	time.Sleep(5 * time.Second)
	for _, t := range tl.Tasks {
		if t.Name == name {
			t.Run(ch)
			return
		}
	}
}

type ReloadResult struct {
	Killed    []string
	Started   []string
	Restarted []string
}

// update all tasks:
// * enable and disable
// * add and remove
// * change parameters? (env, argv)
func (tl *TaskList) reload() (*ReloadResult, error) {
	log.Print("reload tasks")
	c, err := userConfig(user.Current())
	if err != nil {
		return nil, err
	}

	tl2, err := c.loadTasks()
	if err != nil {
		return nil, err
	}

	visited := make(map[string]bool)
	tasksToStart := make([]*Task, 0, len(tl2.Tasks))
	result := &ReloadResult{
		make([]string, 0, len(tl.Tasks)),
		make([]string, 0, len(tl.Tasks)),
		make([]string, 0, len(tl.Tasks)),
	}

	// merge existing tasks with new ones, updating existing ones where
	// possible, starting new ones, and stopping removed ones
	func() {
		tl.mu.RLock()
		defer tl.mu.RUnlock()

		for _, newtask := range tl2.Tasks {
			foundTask := false

			for _, oldtask := range tl.Tasks {
				if oldtask.Name != newtask.Name {
					continue
				}
				if !newtask.Enable && oldtask.Alive() {
					err = oldtask.Signal(os.Interrupt)
					if err != nil {
						return
					}
					result.Killed = append(result.Killed, oldtask.Name)
					continue
				} else if newtask.Enable && !oldtask.Enable {
					tasksToStart = append(tasksToStart, newtask)
					continue
				}

				foundTask = true
				start := false
				start, err = merge(newtask, oldtask)
				if err != nil {
					return
				}
				if start {
					tasksToStart = append(tasksToStart, newtask)
					result.Restarted = append(result.Restarted, newtask.Name)
				}
				break
			}

			// if no existing task was found, start it if necessary
			if !foundTask && newtask.Enable {
				tasksToStart = append(tasksToStart, newtask)
				result.Started = append(result.Started, newtask.Name)
			}
			visited[newtask.Name] = true
		}

		// find tasks that were in the old list but not in the new one
		for _, oldtask := range tl.Tasks {
			if _, ok := visited[oldtask.Name]; !ok {
				err = oldtask.Signal(os.Interrupt)
				if err != nil {
					return
				}
				result.Killed = append(result.Killed, oldtask.Name)
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	tl.mu.Lock()
	tl.Tasks = tl2.Tasks
	tl.mu.Unlock()

	tl.taskChan <- tasksToStart

	return result, nil
}

// restart process with new params if necessary
// if nothing changed, copy the old data to the new one (process, time started,
// etc.)
func merge(newtask, oldtask *Task) (start bool, err error) {
	if newtask.Invoke != oldtask.Invoke ||
		!mapequal(newtask.Env, oldtask.Env) ||
		!stringsequal(newtask.Args, oldtask.Args) {

		err = oldtask.Signal(os.Interrupt)
		if err != nil {
			return
		}

		return true, nil
	}

	newtask.cmd = oldtask.cmd
	newtask.lr = oldtask.lr
	newtask.started = oldtask.started

	return false, nil
}

func (tl *TaskList) lookup(name string) (*Task, error) {
	if name == "" {
		return nil, ErrNoName
	}

	for _, t := range tl.Tasks {
		if name == t.Name {
			return t, nil
		}
	}

	return nil, ErrNoTask
}

// RPC calls:
// status [name]
//   => table of names, on/off, PID, uptime
// start <name>
// stop <name>
// restart <name>
// signal <name> <signal>
// names
//   => list of names
// tail <name>
//   => output of tail(1)
//
// maybe:
//
// a way to keep arbitrary values on the server, allow processes to publish
// and advertise key-value pairs
// set <name> <key> <value>
// get <name> [key]

func (tl *TaskList) Status(args *Args, resp *Response) error {
	if args.Name != "" {
		var task *Task
		for _, t := range tl.Tasks {
			if t.Name == args.Name {
				task = t
				break
			}
		}
		if task == nil {
			return fmt.Errorf("no such task: %s", args.Name)
		}
		resp.Tasks = []TaskStatus{task.Status()}
	} else {
		resp.Tasks = make([]TaskStatus, len(tl.Tasks))
		for i, task := range tl.Tasks {
			resp.Tasks[i] = task.Status()
		}
	}

	return nil
}

func (tl *TaskList) Start(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	if t.Alive() {
		return fmt.Errorf("%s: task is already alive", t.Name)
	}
	t.Enable = true

	// send channel to send back status to this goroutine in addition to the
	// main one. Since the task is not started, it shouldn't generate any
	// events and we should have full control in the current goroutine for
	// mutation
	t.ch = make(chan *TaskStatus)

	// go func() {
	tl.taskChan <- t
	// }()

	select {
	case <-time.After(time.Second):
	case stat := <-t.ch:
		resp.Tasks = []TaskStatus{*stat}
	}
	t.ch = nil
	return nil
}

func (tl *TaskList) StartAll(args *Args, resp *Response) error {
	l := make([]*Task, 0, len(tl.Tasks))
	for _, t := range tl.Tasks {
		if !t.Alive() && t.Enable {
			l = append(l, t)
		}
	}
	tl.taskChan <- l
	return nil
}

// Stop a task with SIGINT and disable it so it doesn't try to resuscitate
func (tl *TaskList) Stop(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	if !t.Alive() {
		return fmt.Errorf("%s: task has not been started", t.Name)
	}
	t.Enable = false
	return t.Signal(os.Interrupt)
}

// Like Stop but with SIGKILL for badly misbehaving tasks
func (tl *TaskList) Kill(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	if !t.Alive() {
		return fmt.Errorf("%s: task has not been started", t.Name)
	}
	t.Enable = false
	return t.Kill()
}

// Perform Kill on all tasks
func (tl *TaskList) Killall(args *Args, resp *Response) error {
	for _, t := range tl.Tasks {
		if t.Alive() {
			if err := t.Kill(); err != nil {
				return err
			}
		}
	}
	return nil
}

// Restart a task
func (tl *TaskList) Restart(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	if err := tl.Stop(args, resp); err != nil {
		return err
	}
	for i := 0; ; i++ {
		if !t.Alive() {
			break
		}
		if i > 500 {
			return errors.New("could not stop task")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tl.Start(args, resp); err != nil {
		return err
	}

	return nil
}

// Send a signal to a task
func (tl *TaskList) Signal(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	if len(args.Args) < 1 {
		return errors.New("usage: signal <name> <signal>")
	}
	signame := args.Args[0]
	if n, err := strconv.Atoi(signame); err == nil {
		return t.Signal(syscall.Signal(n))
	}
	signame = strings.ToUpper(signame)
	if sig, ok := signalMap[signame]; ok {
		return t.Signal(sig)
	}
	if sig, ok := signalMap["SIG"+signame]; ok {
		return t.Signal(sig)
	}

	return fmt.Errorf("unknown signal: %s", signame)
}

// Get all task names (used for e.g. bash autocomplete)
func (tl *TaskList) Names(args *Args, resp *Response) error {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	names := make([]string, len(tl.Tasks))
	for i, task := range tl.Tasks {
		names[i] = task.Name
	}
	resp.Status = strings.Join(names, " ")
	return nil
}

// Perform tail(1) on the logs of a task
func (tl *TaskList) Tail(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	path := t.LogPath()
	cmd := exec.Command("tail", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	resp.Status = string(out)
	return nil
}

func (tl *TaskList) Logpath(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	resp.Status = t.LogPath()
	return nil
}

func (tl *TaskList) Reload(args *Args, resp *Response) error {
	res, err := tl.reload()
	if err != nil {
		return err
	}

	if len(res.Killed) > 0 {
		resp.Status += fmt.Sprintf("kill %s\n", strings.Join(res.Killed, " "))
	}
	if len(res.Started) > 0 {
		resp.Status += fmt.Sprintf("start %s\n", strings.Join(res.Started, " "))
	}
	if len(res.Restarted) > 0 {
		resp.Status += fmt.Sprintf("restart %s\n", strings.Join(res.Restarted, " "))
	}

	return nil
}

func (tl *TaskList) Get(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	_ = t

	if len(args.Args) < 1 {
		// return all keys
	} else {
		// return value of given key
	}
	return nil
}

func (tl *TaskList) Set(args *Args, resp *Response) error {
	t, err := tl.lookup(args.Name)
	if err != nil {
		return err
	}
	_ = t

	if len(args.Args) < 2 {
		resp.Status = "usage: set <name> <key> <value>"
		return nil
	}
	return nil
}

func (tl *TaskList) Help(args *Args, resp *Response) error {
	resp.Status = fmt.Sprintf(`Usage: %s <command> [args...]
Commands:
  status          query status of all running tasks
  startall        start all tasks
  killall         kill all tasks
  names           get all task names, space separated
  reload          reload task list and update currently running tasks
  start <task>    start a task
  stop <task>     stop a task with SIGINT
  kill <task>     stop a task with SIGKILL
  restart <task>  restart a task
  signal <task> <signal>
                  send a signal to a task using kill(1) names
  tail <task>     tail the logs of a task
  logpath <task>  get the path to the current log file of a task
  help            print this message`, os.Args[0])

	return nil
}

// TODO: restart daemon, transferring child pids
// TODO: try using dup(2) (syscall.Dup) to rewrite fds
