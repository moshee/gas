package main

import (
	"os"

	"golang.org/x/sys/unix"
)

var signalMap = map[string]os.Signal{
	"ABRT":   unix.SIGABRT,
	"ALRM":   unix.SIGALRM,
	"BUS":    unix.SIGBUS,
	"CHLD":   unix.SIGCHLD,
	"CONT":   unix.SIGCONT,
	"EMT":    unix.SIGEMT,
	"FPE":    unix.SIGFPE,
	"HUP":    unix.SIGHUP,
	"ILL":    unix.SIGILL,
	"INFO":   unix.SIGINFO,
	"INT":    unix.SIGINT,
	"IO":     unix.SIGIO,
	"IOT":    unix.SIGIOT,
	"KILL":   unix.SIGKILL,
	"PIPE":   unix.SIGPIPE,
	"PROF":   unix.SIGPROF,
	"QUIT":   unix.SIGQUIT,
	"SEGV":   unix.SIGSEGV,
	"STOP":   unix.SIGSTOP,
	"SYS":    unix.SIGSYS,
	"TERM":   unix.SIGTERM,
	"TRAP":   unix.SIGTRAP,
	"TSTP":   unix.SIGTSTP,
	"TTIN":   unix.SIGTTIN,
	"TTOU":   unix.SIGTTOU,
	"URG":    unix.SIGURG,
	"USR1":   unix.SIGUSR1,
	"USR2":   unix.SIGUSR2,
	"VTALRM": unix.SIGVTALRM,
	"WINCH":  unix.SIGWINCH,
	"XCPU":   unix.SIGXCPU,
	"XFSZ":   unix.SIGXFSZ,
}
