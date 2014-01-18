package gas

import (
	"log"
	"os"
)

type LogLevel int

const (
	None LogLevel = iota
	Fatal
	Warning
	Notice
	Debug
)

var (
	Verbosity   LogLevel = Fatal
	logger      *log.Logger
	logFile     *os.File
	logFilePath string
	//logChan                  = make(chan logMessage, 10)
	//logRotateThreshold int64 = 5 * 1024 * 1024 // 5MB
	//logNeedRotate            = make(chan time.Time)
)

func (l LogLevel) String() string {
	switch l {
	case Fatal:
		return "FATAL: "
	case Warning:
		return "Warning: "
	}
	return ""
}

func Log(level LogLevel, format string, args ...interface{}) {
	if Verbosity >= level {
		//logChan <- logMessage{level, format, args}
		logger.Printf(level.String()+format, args...)
	}
	if level == Fatal {
		os.Exit(-1)
	}
}

func LogDebug(format string, args ...interface{}) {
	Log(Debug, format, args...)
}

func LogNotice(format string, args ...interface{}) {
	Log(Notice, format, args...)
}

func LogWarning(format string, args ...interface{}) {
	Log(Warning, format, args...)
}

func LogFatal(format string, args ...interface{}) {
	Log(Fatal, format, args...)
}
