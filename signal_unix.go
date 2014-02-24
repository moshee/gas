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
		parseTemplates(templateDir)
		LogNotice("Templates reloaded.")
	},
}

func stop() {
	println()
	exit(0)
}
