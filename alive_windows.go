package lightron

import "golang.org/x/sys/windows"

// processAlive: the process is still there (a quitting first instance, single.go)
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	r, _ := windows.WaitForSingleObject(h, 0)
	return r == uint32(windows.WAIT_TIMEOUT)
}
