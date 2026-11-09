package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Install puts the downloaded .app in place of the running one once this process has exited, and opens it: the
// caller should quit right after a nil return. The new bundle is copied next to the running one first (same volume,
// so the swap is two renames: copying into /Applications in place can be stopped halfway by App Management); a
// detached script waits for this process, swaps, opens the new one, and puts the old one back if a rename fails.
// When the app can't be swapped where it is (run from a disk image, App Translocation, a directory it can't write),
// the package is opened for the person to drag it over, and an error says so.
func (r *Release) Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	bundle, _ := filepath.Abs(filepath.Join(filepath.Dir(exe), "..", ".."))
	if !strings.HasSuffix(bundle, ".app") || strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") || !writable(filepath.Dir(bundle)) {
		_ = exec.Command("open", r.File).Start()
		return fmt.Errorf("update: %s can't be replaced in place; opened %s", bundle, filepath.Base(r.File))
	}
	staged := filepath.Join(filepath.Dir(bundle), "."+strings.TrimSuffix(filepath.Base(bundle), ".app")+"-update.app")
	_ = os.RemoveAll(staged)
	if err := stage(r.File, staged); err != nil {
		_ = os.RemoveAll(staged)
		_ = exec.Command("open", r.File).Start()
		return fmt.Errorf("update: %v; opened %s", err, filepath.Base(r.File))
	}
	script := `
while kill -0 "$1" 2>/dev/null; do sleep 0.2; done
old="$2.old"; rm -rf "$old"
if mv "$2" "$old" && mv "$3" "$2"; then rm -rf "$old" "$4"; open "$2"
else [ -d "$2" ] || mv "$old" "$2"; rm -rf "$3"; open "$2"; open "$4"; fi`
	cmd := exec.Command("/bin/sh", "-c", script, "sh", strconv.Itoa(os.Getpid()), bundle, staged, r.File)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives us
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	return nil
}

// stage copies the .app out of a .dmg (mounted read-only for a moment) or a .zip into dst.
func stage(pkg, dst string) error {
	switch {
	case strings.HasSuffix(pkg, ".dmg"):
		mnt, err := os.MkdirTemp("", "lightron-dmg-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(mnt)
		if out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, pkg).CombinedOutput(); err != nil {
			return fmt.Errorf("hdiutil attach: %v %s", err, out)
		}
		defer exec.Command("hdiutil", "detach", "-quiet", mnt).Run()
		app, err := findApp(mnt)
		if err != nil {
			return err
		}
		if out, err := exec.Command("ditto", app, dst).CombinedOutput(); err != nil {
			return fmt.Errorf("ditto: %v %s", err, out)
		}
		return nil
	case strings.HasSuffix(pkg, ".zip"):
		tmp, err := os.MkdirTemp(filepath.Dir(dst), ".lightron-zip-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		// ditto keeps what a .app needs (modes, symlinks, extended attributes)
		if out, err := exec.Command("ditto", "-x", "-k", pkg, tmp).CombinedOutput(); err != nil {
			return fmt.Errorf("ditto -x: %v %s", err, out)
		}
		app, err := findApp(tmp)
		if err != nil {
			return err
		}
		return os.Rename(app, dst)
	}
	return fmt.Errorf("%s isn't a .dmg or .zip", filepath.Base(pkg))
}

func findApp(dir string) (string, error) {
	names, _ := os.ReadDir(dir)
	for _, n := range names {
		if strings.HasSuffix(n.Name(), ".app") && n.IsDir() {
			return filepath.Join(dir, n.Name()), nil
		}
	}
	return "", fmt.Errorf("no .app in the package")
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".lightron-write-test-")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
