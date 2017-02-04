package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"ktkr.us/pkg/fmtutil"
)

type config struct {
	logDirPath   string
	sockDirPath  string
	sockPath     string
	taskfilePath string
	u            *user.User
}

func userConfig(u *user.User, err error) (*config, error) {
	if err != nil {
		return nil, err
	}
	sockDirPath := filepath.Join(sockDirBase, u.Uid)
	return &config{
		logDirPath:   filepath.Join(logDirBase, u.Username),
		sockDirPath:  sockDirPath,
		sockPath:     filepath.Join(sockDirPath, "gas.sock"),
		taskfilePath: filepath.Join(u.HomeDir, ".gas_tasks.json"),
		u:            u,
	}, nil
}

func (c *config) loadTasks() (tasks TaskList, err error) {
	log.Println("loading tasks")

	taskfile, err := os.Open(c.taskfilePath)
	if err != nil {
		err = errors.Wrap(err, "load tasks")
		return
	}
	defer taskfile.Close()

	err = json.NewDecoder(taskfile).Decode(&tasks.Tasks)
	if err != nil {
		err = errors.Wrap(err, "load tasks")
		return
	}
	tasks.mu = new(sync.RWMutex)
	for _, t := range tasks.Tasks {
		t.ch = make(chan *TaskStatus, 1)
		t.c = c
	}
	tasks.taskChan = make(chan interface{}, 1)
	return
}

func mapequal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func stringsequal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		if x != b[i] {
			return false
		}
	}
	return true
}

func fmtDuration(d time.Duration) string {
	ms := ""
	x := d / time.Millisecond % 1000
	if x != 0 {
		ms = strings.TrimRight("."+strconv.Itoa(int(x)), "0")
	}
	if d < (24 * fmtutil.Hr) {
		return fmtutil.HMS(d) + ms
	}
	days := d / (24 * fmtutil.Hr)
	return fmt.Sprintf("%d days, %s%s", days, fmtutil.HMS(d%(24*fmtutil.Hr)), ms)
}
