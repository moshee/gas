// +build darwin freebsd linux netbsd openbsd

package gas

import (
	"os"
	"syscall"
)

var signalFuncs = map[os.Signal][]func(){
	syscall.SIGINT:  {stop},
	syscall.SIGQUIT: {stop},
	syscall.SIGTERM: {stop},
}

func stop() {
	println()
	exit(0)
}
