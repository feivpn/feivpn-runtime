//go:build !linux && !darwin

package binmgr

import "syscall"

func sysProcAttrDetached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
