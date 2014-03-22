package gas

import "os"

var signalFuncs = make(map[os.Signal][]func())
