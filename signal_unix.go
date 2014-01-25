// +build darwin freebsd linux netbsd openbsd

package gas

import (
	"os"
	"syscall"
)

var signal_funcs = map[os.Signal]func(){
	syscall.SIGINT:  stop,
	syscall.SIGQUIT: stop,
	syscall.SIGTERM: stop,
	syscall.SIGUSR1: func() {
		ts := parse_templates(template_dir)
		Templates = ts
		LogNotice("Templates reloaded.")
	},
}

func stop() {
	println()
	if DB != nil {
		DB.Close()
	}
	os.Exit(0)
}
