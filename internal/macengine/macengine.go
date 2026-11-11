//go:build darwin

// Package macengine finds the Chromium-family browser on a Mac and makes the copy of it an app runs on: the app's
// name, id and icon in its Info.plist, so to macOS the running browser is the app (its Dock icon, ⌘Tab entry, menu
// bar, notifications), and its --app windows have a bare title bar.
//
// Why a copy: what the Dock, ⌘Tab and the menu bar show for a process comes from its bundle's Info.plist, and macOS
// lets a process change only its own type (lsappinfo setinfo on another one is ignored). Run as installed, the
// browser is "Google Chrome": a second Chrome icon in the Dock, Chrome's name in the menu bar.
//
//   - APFS clone (cp -c): about a second, and no disk space while it shares blocks with the original. One framework
//     version (~700MB) becomes the copy's own once the browser updates past it, so only the running version is kept,
//     and the copy is made again when the browser's version (or the app's brand) changes, older copies deleted.
//   - Name, id and icon, nothing else. LSUIElement breaks Chromium: it reads it to decide it is a helper process and
//     then can't find its framework ("icudtl.dat not found in bundle", from the second launch on, once LaunchServices
//     registered the bundle). Symlinking the browser's binary or Frameworks into a bundle of ours fails the same way.
//   - The icon lives in Assets.car too (CFBundleIconName), which macOS prefers to the .icns: that key goes.
//   - Editing the plist breaks the vendor's signature (SIGKILL at launch): the copy is re-signed ad hoc, after
//     stripping extended attributes (codesign refuses "detritus"). The nested framework keeps the vendor's signature.
//     Ad hoc also means no hardened runtime and no library validation: the app's menu library can load.
//   - A re-signed copy is another code identity: the user's keychain items for the browser would prompt, so the
//     engine runs with --use-mock-keychain.
//   - The Dock shows the bundle's folder name, not CFBundleName: the copy is <dir>/<browser version>/<Name>.app.
//   - The first launch of a fresh copy takes ~10s (macOS scanning the newly signed bundle), once per browser version.
package macengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Brand is what the copy is to macOS.
type Brand struct {
	Name string // Dock, ⌘Tab, menu bar, notifications
	ID   string // CFBundleIdentifier: its own, so its notification permission and Dock place aren't the browser's
	// Schemes replace the browser's URL types (http, https, file…): the copy is the handler of the app's own schemes
	// only. LaunchServices then sends their links to the copy: running, the menu library takes them; not running, it
	// is started by LaunchServices itself, with Env.
	Schemes []string
	// Env is the copy's LSEnvironment (with the browser's own): what a launch by LaunchServices (a link, not the
	// shell) gets, e.g. the menu library to load.
	Env  map[string]string
	ICNS []byte // the icon; nil = keep the browser's
}

// Candidates are the browsers looked for, in order. Each is "<name>.app" in /Applications or ~/Applications.
var Candidates = []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"}

// ErrNoBrowser: none of the Candidates is installed.
var ErrNoBrowser = errors.New("no Chrome, Edge, Brave or Chromium on this Mac")

// FindBrowser returns the executable of the first installed Candidate.
func FindBrowser() (string, error) {
	home, _ := os.UserHomeDir()
	for _, name := range Candidates {
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
			exe := filepath.Join(root, name+".app", "Contents", "MacOS", name)
			if st, err := os.Stat(exe); err == nil && !st.IsDir() {
				return exe, nil
			}
		}
	}
	return "", ErrNoBrowser
}

// layout: bump when what a copy holds changes, so existing copies are made again.
const layout = "1"

// Prepare returns the executable of a copy of the browser at exe carrying b, making it in dir if needed.
func Prepare(exe, dir string, b Brand, logf func(string, ...any)) (string, error) {
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return "", fmt.Errorf("not inside an app bundle: %s", exe)
	}
	src := exe[:i+4]
	bin := filepath.Base(exe)
	version, err := plistString(filepath.Join(src, "Contents/Info.plist"), "CFBundleShortVersionString")
	if err != nil {
		return "", err
	}
	browser := strings.TrimSuffix(filepath.Base(src), ".app")
	slot := filepath.Join(dir, browser+" "+version)
	dst := filepath.Join(slot, b.Name+".app")
	out := filepath.Join(dst, "Contents/MacOS", bin)
	sum := sha256.Sum256(b.ICNS)
	extra, _ := json.Marshal([]any{b.Schemes, b.Env})
	stamp := layout + "\n" + b.Name + "\n" + b.ID + "\n" + hex.EncodeToString(sum[:8])
	if len(b.Schemes) > 0 || len(b.Env) > 0 {
		stamp += "\n" + string(extra)
	}
	if got, err := os.ReadFile(filepath.Join(dst, ".lightron")); err == nil && string(got) == stamp {
		return out, nil
	}

	start := time.Now()
	if err := os.MkdirAll(slot, 0o755); err != nil {
		return "", err
	}
	tmp := filepath.Join(slot, ".building.app")
	_ = os.RemoveAll(tmp)
	if o, err := exec.Command("cp", "-cR", src, tmp).CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("clone %s (needs APFS, on the same volume as %s): %v %s", src, dir, err, o)
	}
	// Only the running version of the framework: an update of the original then frees the rest.
	vers, _ := filepath.Glob(filepath.Join(tmp, "Contents/Frameworks/*.framework/Versions/*"))
	for _, v := range vers {
		if n := filepath.Base(v); n != version && n != "Current" {
			_ = os.RemoveAll(v)
		}
	}
	plist := filepath.Join(tmp, "Contents/Info.plist")
	steps := [][]string{
		{"plutil", "-replace", "CFBundleName", "-string", b.Name, plist},
		{"plutil", "-replace", "CFBundleDisplayName", "-string", b.Name, plist},
		{"plutil", "-replace", "CFBundleIdentifier", "-string", b.ID, plist},
	}
	if len(b.Schemes) > 0 {
		types, _ := json.Marshal([]map[string]any{{"CFBundleURLName": b.ID, "CFBundleURLSchemes": b.Schemes}})
		steps = append(steps, []string{"plutil", "-replace", "CFBundleURLTypes", "-json", string(types), plist})
	}
	if len(b.Env) > 0 {
		env := map[string]string{}
		if o, err := exec.Command("plutil", "-extract", "LSEnvironment", "json", "-o", "-", plist).Output(); err == nil {
			_ = json.Unmarshal(o, &env)
		}
		for k, v := range b.Env {
			env[k] = v
		}
		e, _ := json.Marshal(env)
		steps = append(steps, []string{"plutil", "-replace", "LSEnvironment", "-json", string(e), plist})
	}
	if len(b.ICNS) > 0 {
		icon, err := plistString(plist, "CFBundleIconFile")
		if err == nil && icon != "" {
			if !strings.HasSuffix(icon, ".icns") {
				icon += ".icns"
			}
			if err := os.WriteFile(filepath.Join(tmp, "Contents/Resources", icon), b.ICNS, 0o644); err != nil {
				_ = os.RemoveAll(tmp)
				return "", err
			}
			steps = append(steps, []string{"plutil", "-remove", "CFBundleIconName", plist})
		}
	}
	steps = append(steps, []string{"xattr", "-cr", tmp}, []string{"codesign", "--force", "--sign", "-", tmp})
	for _, s := range steps {
		o, err := exec.Command(s[0], s[1:]...).CombinedOutput()
		// xattr: some files refuse (com.apple.provenance), the signature only cares about the rest. plutil -remove:
		// fine when the key isn't there.
		if err != nil && s[0] != "xattr" && !(s[0] == "plutil" && s[1] == "-remove") {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("%s: %v %s", s[0], err, o)
		}
	}
	_ = os.WriteFile(filepath.Join(tmp, ".lightron"), []byte(stamp), 0o644)
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	// Made again at the same path, LaunchServices may still hand out what it cached (the browser's icon).
	_ = exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", dst).Run()
	// Older copies (the browser moved on, another name) now hold blocks of their own: delete them.
	old, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, o := range old {
		if o != slot {
			_ = os.RemoveAll(o)
		}
	}
	others, _ := filepath.Glob(filepath.Join(slot, "*.app"))
	for _, o := range others {
		if o != dst {
			_ = os.RemoveAll(o)
		}
	}
	logf("lightron: engine: copied %s %s as %q in %v", browser, version, b.Name, time.Since(start).Round(time.Millisecond))
	return out, nil
}

// App is the .app bundle an engine executable is in.
func App(exe string) string {
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+4]
	}
	return ""
}

func plistString(path, key string) (string, error) {
	b, err := exec.Command("plutil", "-extract", key, "raw", "-o", "-", path).Output()
	if err != nil {
		return "", fmt.Errorf("read %s from %s: %v", key, path, err)
	}
	return strings.TrimSpace(string(b)), nil
}
