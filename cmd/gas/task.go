package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"ktkr.us/pkg/logrotate/rotator"
)

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

type Task struct {
	Name   string
	Invoke string
	Env    map[string]string
	Args   []string
	Enable bool
	Dir    string

	cmd          *exec.Cmd
	lr           *rotator.Rotator // for logs from task itself
	started      time.Time        // time at which task was started
	ch           chan *TaskStatus // channel on which to send task status
	c            *config
	prefix       string
	outputReader *os.File
}

func (t *Task) Log(x ...interface{}) {
	log.Println(append([]interface{}{t.prefix}, x...)...)
}

func (t *Task) Logf(format string, x ...interface{}) {
	log.Printf(t.prefix+" "+format, x...)
}

func (t *Task) Run(ch chan<- *TaskStatus) {
	t.prefix = fmt.Sprintf("[%s]", t.Name)

	// copy current environment to child process
	// TODO: make optional?
	env := formatEnv(t.Env)

	t.Logf("starting %v %s %v", env, t.Invoke, t.Args)

	stat := t.Status()

	for _, val := range os.Environ() {
		env = append(env, val)
	}

	t.cmd = exec.Command(t.Invoke, t.Args...)

	// see golang/go issue #10338
	r, w, err := os.Pipe()
	if err != nil {
		stat.Message = err.Error()
		ch <- &stat
		return
	}

	t.cmd.Stderr = w
	t.cmd.Stdout = w
	t.outputReader = r

	t.cmd.Env = env
	t.cmd.Dir = t.Dir

	logpath := t.LogPath()
	t.lr, err = rotator.New(t.outputReader, logpath, 5*1024, false)
	if err != nil {
		stat.Message = err.Error()
		ch <- &stat
		return
	}

	logError := make(chan error, 1)
	taskError := make(chan error, 1)
	go func() {
		logError <- errors.Wrap(t.lr.Run(), "logrotate")
	}()

	go func() {
		if err = t.CheckRunningTask(); err != nil {
			taskError <- err
			return
		}

		t.started = time.Now()
		err = t.cmd.Start()

		stat := t.Status()
		if err != nil {
			t.cmd.Process.Release()
			taskError <- errors.Wrap(err, "start task")
			stat.Message = err.Error()
			if t.ch != nil {
				t.ch <- &stat
			}
			return
		} else {
			if err = t.MakePidFile(); err != nil {
				stat.Message = err.Error()
			}
			t.Logf("started with pid %d", t.Pid())
			if t.ch != nil {
				// report status of started task to sender
				t.ch <- &stat
			}
		}

		err = t.cmd.Wait()
		taskError <- err
	}()

	// block in this goroutine until task is done or something dies
	select {
	case err = <-logError:
		// XXX: how certain can we be that the process should be shutting down
		// when it closes stdout/stderr?
		err2 := <-taskError
		if err == nil {
			err = err2
		}
	case err = <-taskError:
	}

	if err != nil {
		if t.Alive() {
			t.Kill()
		}
		t.Logf("task died: %v", err)
		stat = t.Status()
		stat.Message = err.Error()
	} else {
		stat = t.Status()
		t.Log("task finished")
	}

	os.Remove(t.PidFile())

	// report back to main thread
	if t.ch != nil {
		t.ch <- &stat
	}
	ch <- &stat
}

// check if process was already running and process manager crashed
func (t *Task) CheckRunningTask() error {
	p := t.PidFile()
	_, err := os.Stat(p)
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "check pid file")
		} else {
			return nil
		}
	}

	f, err := os.Open(p)
	if err != nil {
		return errors.Wrap(err, "check pid file")
	}
	defer f.Close()
	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return errors.Wrap(err, "check pid file")
	}
	pidString := strings.TrimSpace(string(buf))
	pid, err := strconv.Atoi(pidString)
	if err != nil {
		return errors.Wrap(err, "check pid file")
	}
	t.Logf("task already running at pid %d", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		return errors.Wrap(err, "check pid file")
	}

	// TODO: this is where we would attempt to regain ownership of the process
	if err = proc.Kill(); err != nil {
		t.Logf("kill %d: %v", pid, err)
	} else {
		t.Logf("killed %d", pid)
	}
	f.Truncate(0)

	return nil
}

func (t *Task) LogPath() string {
	return filepath.Join(t.c.logDirPath, t.Name+".log")
}

func (t *Task) PidFile() string {
	return filepath.Join(t.c.sockDirPath, "gas", t.Name+".pid")
}

func (t *Task) MakePidFile() error {
	p := t.PidFile()
	os.MkdirAll(filepath.Dir(p), 0700)
	f, err := os.OpenFile(p, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return errors.Wrap(err, "MakePidFile")
	}

	_, err = fmt.Fprint(f, t.Pid())
	return errors.Wrap(err, "MakePidFile")
}

func (t *Task) Status() TaskStatus {
	return TaskStatus{
		t.Name,
		t.Alive(),
		t.Pid(),
		t.Uptime(),
		"",
		t.Enable,
		t.Env["GAS_PORT"],
	}
}

func (t *Task) Alive() bool {
	return t.cmd != nil && t.cmd.ProcessState == nil
}

func (t *Task) Pid() int {
	if t.Alive() && t.cmd.Process != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

func (t *Task) Kill() error {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	t.Log("processes die when they are killed")
	err := errors.Wrap(t.cmd.Process.Kill(), "Task.Kill")
	if err != nil {
		t.Log(err)
	}
	t.outputReader.Close()
	return err
}

func (t *Task) Signal(sig os.Signal) error {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	t.Logf("got signal: %v", sig)
	err := errors.Wrap(t.cmd.Process.Signal(sig), "Task.Signal")
	if err != nil {
		t.Log(err)
	}
	t.outputReader.Sync()
	return err
}

func (t *Task) Uptime() time.Duration {
	return time.Since(t.started)
}

func formatEnv(m map[string]string) []string {
	x := make([]string, 0, len(m))
	for k, v := range m {
		x = append(x, k+"="+v)
	}
	return x
}
