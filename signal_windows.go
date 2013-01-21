package gas

import "os"

var signal_funcs = make(map[os.Signal]func())
