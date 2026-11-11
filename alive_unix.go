//go:build !windows

package lightron

import "syscall"

// processAlive: the process is still there (a quitting first instance, single.go)
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
