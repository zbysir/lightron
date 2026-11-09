package update

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Install runs the downloaded installer (lightron build's NSIS one, or any that takes /S for silent) once this process
// has exited: the caller should quit right after a nil return. The installer installs over this one and starts the
// new one. PowerShell (Windows 10 and later) does the waiting, hidden.
func (r *Release) Install() error {
	if !strings.HasSuffix(strings.ToLower(r.File), ".exe") {
		return fmt.Errorf("update: %s isn't an installer (.exe)", r.File)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	script := fmt.Sprintf("Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Process -FilePath %s -ArgumentList '/S'", os.Getpid(), quote(r.File))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000008} // CREATE_NO_WINDOW | DETACHED_PROCESS
	return cmd.Start()
}
