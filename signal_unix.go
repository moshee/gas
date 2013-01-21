// +build darwin freebsd linux netbsd openbsd

package gas

import (
	"os"
	"syscall"
)

var signal_funcs = map[os.Signal]func(){
	syscall.SIGINT: func() { println(); DB.Close(); os.Exit(0) },
	syscall.SIGHUP: func() {
		ts := parse_templates(template_dir)
		Templates = ts
		Log(Notice, "Templates reloaded.")
	},
}
