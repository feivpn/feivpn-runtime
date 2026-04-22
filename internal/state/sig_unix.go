//go:build linux || darwin

package state

import "syscall"

// syscallZero is the signal used to test if a PID is alive without
// actually delivering a signal (kill(2) with sig=0).
var syscallZero = syscall.Signal(0)
