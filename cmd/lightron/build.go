package main

import (
	_ "embed"
	"flag"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zbysir/lightron/internal/pack"
)

//go:embed assets/icon.png
var defaultIcon []byte

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	cfgPath := fs.String("config", "lightron.json", "the app's description")
	targets := fs.String("target", "", "comma-separated os/arch to build (default: lightron.json's targets, or darwin/arm64,darwin/amd64,windows/amd64)")
	ver := fs.String("version", "", "the version to build (default: lightron.json's); a leading v is dropped (v1.2.0 tags)")
	ldflags := fs.String("ldflags", "", "more -ldflags for go build")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: lightron build [flags]

Builds the app for each target into dist/:
  macOS    <Name>-<version>-darwin-<arch>.dmg (on a Mac) and .zip, of <Name>.app
  Windows  <Name>-<version>-windows-<arch>-setup.exe (with makensis) and .zip

The version is also given to the program as main.version (-X), for an About box or the update package.

`)
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if *ver != "" {
		c.Version = strings.TrimPrefix(*ver, "v")
	}
	list := c.Targets
	if *targets != "" {
		list = strings.Split(*targets, ",")
	}
	iconPNG := defaultIcon
	if c.Icon != "" {
		if iconPNG, err = os.ReadFile(c.Icon); err != nil {
			return err
		}
	}
	icon, err := pack.DecodePNG(iconPNG)
	if err != nil {
		return fmt.Errorf("icon: %w", err)
	}
	if b := icon.Bounds(); b.Dx() != b.Dy() || b.Dx() < 256 {
		return fmt.Errorf("icon: want a square PNG, 512px or larger (got %dx%d)", b.Dx(), b.Dy())
	}
	if err := os.MkdirAll(c.Out, 0o755); err != nil {
		return err
	}
	for _, t := range list {
		goos, arch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			return fmt.Errorf("target %q: want os/arch", t)
		}
		start := time.Now()
		var outs []string
		switch goos {
		case "darwin":
			outs, err = buildMac(c, arch, icon, *ldflags)
		case "windows":
			outs, err = buildWindows(c, arch, icon, *ldflags)
		default:
			err = fmt.Errorf("%s isn't a target lightron builds (darwin, windows)", goos)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		fmt.Printf("%s (%v)\n", t, time.Since(start).Round(100*time.Millisecond))
		for _, o := range outs {
			st, _ := os.Stat(o)
			rel := o
			if wd, err := os.Getwd(); err == nil {
				if r, err := filepath.Rel(wd, o); err == nil {
					rel = r
				}
			}
			fmt.Printf("  %-56s %s\n", rel, humanSize(st.Size()))
		}
	}
	return nil
}

func goBuild(c *Config, goos, arch, out, ldflags string) error {
	flags := "-s -w -X main.version=" + c.Version
	if goos == "windows" {
		flags += " -H windowsgui"
	}
	if ldflags != "" {
		flags += " " + ldflags
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", flags, "-o", out, c.Main)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// buildMac: <Name>.app = the program (Contents/MacOS), its Info.plist, its icon. LSUIElement keeps the program out
// of the Dock: what shows there is the engine (the browser copy carrying the app's name and icon), so the program
// would otherwise be a second, dead icon.
func buildMac(c *Config, arch string, icon image.Image, ldflags string) ([]string, error) {
	work := filepath.Join(c.Out, "darwin-"+arch)
	app := filepath.Join(work, c.Name+".app")
	_ = os.RemoveAll(app)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(app, "Contents", "Resources"), 0o755); err != nil {
		return nil, err
	}
	if err := goBuild(c, "darwin", arch, filepath.Join(app, "Contents", "MacOS", c.exe()), ldflags); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Resources", "icon.icns"), pack.ICNS(icon), 0o644); err != nil {
		return nil, err
	}
	plist := pack.Plist(map[string]any{
		"CFBundleIdentifier": c.ID, "CFBundleName": c.Name, "CFBundleDisplayName": c.Name,
		"CFBundleExecutable": c.exe(), "CFBundleIconFile": "icon", "CFBundlePackageType": "APPL",
		"CFBundleShortVersionString": c.Version, "CFBundleVersion": c.Version,
		"LSMinimumSystemVersion": "11.0", "LSUIElement": true, "NSHighResolutionCapable": true,
	})
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), plist, 0o644); err != nil {
		return nil, err
	}
	// Apple silicon runs only signed code; the Go linker signs the binary, signing the bundle seals the Info.plist
	// and resources with it. Ad hoc: a Developer ID signature and notarization are the app maker's step (docs).
	if have("codesign") {
		if out, err := exec.Command("codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("codesign: %v %s", err, out)
		}
	}
	name := c.pkgName("darwin", arch)
	zip := filepath.Join(c.Out, name+".zip")
	if err := pack.Zip(zip, app); err != nil {
		return nil, err
	}
	outs := []string{zip}
	if have("hdiutil") {
		dmg := filepath.Join(c.Out, name+".dmg")
		vol, err := os.MkdirTemp("", "lightron-dmg-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(vol)
		if out, err := exec.Command("ditto", app, filepath.Join(vol, c.Name+".app")).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ditto: %v %s", err, out)
		}
		_ = os.Symlink("/Applications", filepath.Join(vol, "Applications"))
		_ = os.Remove(dmg)
		if out, err := exec.Command("hdiutil", "create", "-quiet", "-volname", c.Name, "-srcfolder", vol, "-ov", "-format", "ULFO", dmg).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("hdiutil: %v %s", err, out)
		}
		outs = append([]string{dmg}, outs...)
	} else {
		fmt.Println("  (no .dmg: hdiutil is macOS's; the .zip installs the same way)")
	}
	return outs, nil
}

// buildWindows: <exe>.exe with its icon, manifest and version (a .syso next to the main package while it builds),
// a .zip of it, and the installer when makensis is around.
func buildWindows(c *Config, arch string, icon image.Image, ldflags string) ([]string, error) {
	work := filepath.Join(c.Out, "windows-"+arch)
	stage := filepath.Join(work, c.Name)
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, err
	}
	mainDir := filepath.Join(c.dir, c.Main)
	syso := filepath.Join(mainDir, "zz_lightron_windows_"+arch+".syso")
	f, err := os.Create(syso)
	if err != nil {
		return nil, err
	}
	err = pack.WindowsResources(f, arch, icon, c.Name, c.Version, c.Publisher)
	f.Close()
	defer os.Remove(syso)
	if err != nil {
		return nil, err
	}
	exe := c.exe() + ".exe"
	if err := goBuild(c, "windows", arch, filepath.Join(stage, exe), ldflags); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "icon.ico"), pack.ICO(icon, []int{16, 24, 32, 48, 64, 256}, false), 0o644); err != nil {
		return nil, err
	}
	name := c.pkgName("windows", arch)
	zip := filepath.Join(c.Out, name+".zip")
	if err := pack.Zip(zip, stage); err != nil {
		return nil, err
	}
	outs := []string{zip}
	if have("makensis") {
		setup := mustAbs(filepath.Join(c.Out, name+"-setup.exe"))
		setupIcon := mustAbs(filepath.Join(work, "setup.ico"))
		if err := os.WriteFile(setupIcon, pack.ICO(icon, []int{16, 32, 48}, true), 0o644); err != nil {
			return nil, err
		}
		script, err := pack.NSISScript(pack.NSIS{
			Name: c.Name, Version: c.Version, Publisher: c.Publisher, Exe: exe,
			Files: []string{mustAbs(filepath.Join(stage, exe)), mustAbs(filepath.Join(stage, "icon.ico"))},
			Out:   setup, Icon: setupIcon, ShortcutIcon: "icon.ico", RegKey: c.slug(),
		})
		if err != nil {
			return nil, err
		}
		nsi := filepath.Join(work, "installer.nsi")
		if err := os.WriteFile(nsi, []byte(script), 0o644); err != nil {
			return nil, err
		}
		cmd := exec.Command("makensis", "-V2", nsi)
		// With no locale set, Homebrew's makensis aborts on any script (std::bad_alloc). Linux has C.UTF-8.
		loc := "C.UTF-8"
		if runtime.GOOS == "darwin" {
			loc = "en_US.UTF-8"
		}
		cmd.Env = append(os.Environ(), "LC_ALL="+loc, "LANG="+loc)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("makensis: %v %s", err, out)
		}
		outs = append([]string{setup}, outs...)
	} else {
		fmt.Println("  (no installer: makensis not found; brew install makensis / apt install nsis)")
	}
	return outs, nil
}

func have(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
