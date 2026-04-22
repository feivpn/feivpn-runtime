//go:build !linux && !darwin

package state

import "os"

// On non-unix platforms IsAlive falls back to os.FindProcess only.
var syscallZero os.Signal = nil
