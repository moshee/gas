package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

const (
	logDirBase  = "/var/log/gas"
	sockDirBase = "/var/run/user"
)

func main() {
	var (
		flagServer = flag.Bool("s", false, "Run as unprivileged server")
		flagUser   = flag.String("u", "", "Set up environment for `USER`")
		flagFile   = flag.String("f", "", "Use named task file")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  server: %s -s [-f <taskfilepath>] [sockpath]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  client: %s -u <username>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	log.SetPrefix("gas: ")
	log.SetFlags(0)

	if *flagServer {
		runServer(*flagFile, flag.Arg(0))
		return
	}

	// run subcommand
	if flag.NArg() > 0 {
		handleCommand(flag.Arg(0), flag.Args()[1:])
		return
	}

	if *flagUser == "" {
		log.Fatal("need to set user with -u flag")
	}

	// The following code assumes root privilege
	c, err := userConfig(user.Lookup(*flagUser))
	if err != nil {
		log.Fatal(err)
	}

	uid, err := strconv.Atoi(c.u.Uid)
	if err != nil {
		log.Fatal(err)
	}
	gid, err := strconv.Atoi(c.u.Gid)
	if err != nil {
		log.Fatal(err)
	}

	dirs := []struct {
		path  string
		mode  os.FileMode
		chown bool
	}{
		{logDirBase, 0755, false},
		{c.logDirPath, 0700, true},
		{sockDirBase, 0755, false},
		{c.sockDirPath, 0700, true},
	}

	for _, dir := range dirs {
		err = os.MkdirAll(dir.path, dir.mode)
		if err != nil {
			log.Fatal(err)
		}
		if dir.chown {
			err = os.Chown(dir.path, uid, gid)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	log.Println("setup done - please launch with -s flag")
}

func runServer(flagFile string, sockpath string) {
	c, err := userConfig(user.Current())
	if err != nil {
		log.Fatal(err)
	}
	if flagFile != "" {
		c.taskfilePath = flagFile
	}
	if sockpath != "" {
		c.sockPath = sockpath
	}

	if c.u.Uid == "0" {
		log.Fatal("cowardly refusing to run as root")
	}

	log.SetPrefix("")
	log.SetFlags(log.LstdFlags)

	tasks, err := c.loadTasks()
	if err != nil {
		log.Fatal(err)
	}
	n := 0
	for _, t := range tasks.Tasks {
		if t.Enable {
			n++
		}
	}
	if n > 0 {
		log.Printf("starting %d enabled tasks", n)
	} else {
		log.Print("no tasks are enabled")
	}
	statusChan := make(chan *TaskStatus)

	err = os.Remove(c.sockPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatal(err)
		}
	}
	l, err := net.Listen("unix", c.sockPath)
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(c.sockPath)

	for _, task := range tasks.Tasks {
		if task.Enable {
			go task.Run(statusChan)
		}
	}

	rpc.Register(&tasks)
	go rpc.Accept(l)

	sigchan := make(chan os.Signal, 2)
	signal.Notify(sigchan, os.Interrupt, syscall.SIGHUP)

	for {
		select {
		case ts := <-statusChan:
			if !ts.Alive {
				if ts.Enable {
					log.Printf("%s died: %s", ts.Name, ts.Message)
					go tasks.save(ts.Name, statusChan)
				} else {
					log.Printf("%s killed: %s", ts.Name, ts.Message)
				}
			}

		case sig := <-sigchan:
			switch sig {
			case os.Interrupt:
				log.Print("killing tasks...")
				for _, task := range tasks.Tasks {
					if task.Alive() {
						if err = task.Kill(); err != nil {
							log.Print(err)
						}
					}
				}
				log.Print("bye")
				return

			case syscall.SIGHUP:
				res, err := tasks.reload()
				if err != nil {
					log.Print(err)
					break
				}
				if len(res.Killed) > 0 {
					log.Print("reload tasks: kill ", strings.Join(res.Killed, " "))
				}
				if len(res.Started) > 0 {
					log.Print("reload tasks: start ", strings.Join(res.Started, " "))
				}
				if len(res.Restarted) > 0 {
					log.Print("reload tasks: restart ", strings.Join(res.Restarted, " "))
				}
			}

		case tasksToStart := <-tasks.taskChan:
			switch v := tasksToStart.(type) {
			case []*Task:
				for _, task := range v {
					if task.Enable {
						go task.Run(statusChan)
					} else {
						task.Signal(signalMap["TERM"])
					}
				}

			case *Task:
				if v.Enable {
					go v.Run(statusChan)
				} else {
					v.Signal(signalMap["TERM"])
				}
			}
		}
	}
}
