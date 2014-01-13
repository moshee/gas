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

/*
// Consume messages and log them, while also rotating logs as needed
func logLines(messages <-chan logMessage, needRotate <-chan time.Time) {
	for {
		select {
		case msg := <-messages:
			if logger == nil {
				continue
			}

			logger.Printf(msg.level.String()+msg.format, msg.args...)

			if msg.level == Fatal {
				debug.PrintStack()
				os.Exit(1)
			}

		case now := <-needRotate:
			rotateLog(now)
		}
	}
}

// Poll the filesize and send on the channel if the log gets too big (needs to
// be rotated)
func pollLogfile(pollInterval time.Duration, needRotate chan<- time.Time) {
	t := time.NewTicker(pollInterval)
	for {
		now := <-t.C

		if logFile == os.Stdout || logFile == nil {
			continue
		}

		fi, err := logFile.Stat()
		if err != nil {
			log.Printf("Error: pollLogfile: %v", err)
			continue
		}

		if fi.Size() >= logRotateThreshold {
			needRotate <- now
		}
	}
}

// gzip compress the current logfile ("logfile.log") into
// "logfile-year-month-day.log.gz" and open/truncate the log file with a new
// one
func rotateLog(now time.Time) {
	ext := filepath.Ext(logFilePath)
	fileName := logFilePath[:len(logFilePath)-len(ext)]
	arcName := fmt.Sprintf("%s-%d-%d-%d%s.gz", fileName, now.Year(), now.Month(), now.Day(), ext)

	arc, err := os.Create(arcName)
	if err != nil {
		log.Printf("Error rotating log: creating archive file: %v", err)
		return
	}
	defer arc.Close()

	gz := gzip.NewWriter(arc)
	io.Copy(gz, logFile)
	gz.Close()

	logFile.Close()
	logFile, err = os.OpenFile(logFilePath, os.O_TRUNC|os.O_CREATE|os.O_RDWR, os.FileMode(0644))
	if err != nil {
		log.Printf("Error rotating log: creating new log file: %v", err)
		return
	}
}

type logMessage struct {
	level  LogLevel
	format string
	args   []interface{}
}
*/

func Log(level LogLevel, format string, args ...interface{}) {
	/*
		if Verbosity >= level {
			logChan <- logMessage{level, format, args}
		}
	*/
	logger.Printf(level.String()+format, args...)
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
