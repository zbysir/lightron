package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is lightron.json.
type Config struct {
	// Name is the app's name (the Dock, the taskbar, the installer, file names). Required.
	Name string `json:"name"`
	// ID is a reverse-DNS identifier (com.example.notes). Required.
	ID string `json:"id"`
	// Version is the app's version, x.y.z. lightron build --version overrides it (a release tag).
	Version string `json:"version"`
	// Icon is a square PNG, 512px or larger. Default: Lightron's.
	Icon string `json:"icon,omitempty"`
	// Main is the Go package to build. Default: ".".
	Main string `json:"main,omitempty"`
	// Out is where packages go. Default: "dist".
	Out string `json:"out,omitempty"`
	// Publisher is shown in Windows' Installed apps.
	Publisher string `json:"publisher,omitempty"`
	// Targets are what `lightron build` builds by default: darwin/arm64, darwin/amd64, windows/amd64.
	Targets []string `json:"targets,omitempty"`
	// Exe is the executable's name, without .exe. Default: the name, lowercased, spaces as dashes (notes-pro).
	Exe string `json:"exe,omitempty"`
	// Files are more files and directories to ship: destination (relative) → source. In the .app's
	// Contents/Resources on macOS, next to the .exe on Windows. "{os}" and "{arch}" in a source are the target's
	// (darwin / windows, arm64 / amd64): {"bin": "build/{os}-{arch}/bin"}.
	Files map[string]string `json:"files,omitempty"`
	// NSISInclude is an NSIS script for the Windows installer: !include'd, and its macro preInstall (if it defines
	// one) runs before the files are copied, e.g. to remove what an earlier installer left.
	NSISInclude string `json:"nsis_include,omitempty"`

	// dir is lightron.json's directory (absolute): Main is built from there.
	dir string
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found (lightron init writes one)", path)
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("%s: name is required", path)
	}
	if !idRe.MatchString(c.ID) {
		return nil, fmt.Errorf("%s: id must be reverse-DNS, like com.example.notes (got %q)", path, c.ID)
	}
	if c.Version == "" {
		c.Version = "0.0.0"
	}
	if c.Main == "" {
		c.Main = "."
	}
	if c.Out == "" {
		c.Out = "dist"
	}
	if len(c.Targets) == 0 {
		c.Targets = []string{"darwin/arm64", "darwin/amd64", "windows/amd64"}
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	c.dir = dir
	if c.Icon != "" && !filepath.IsAbs(c.Icon) {
		c.Icon = filepath.Join(dir, c.Icon)
	}
	if !filepath.IsAbs(c.Out) {
		c.Out = filepath.Join(dir, c.Out)
	}
	if c.NSISInclude != "" && !filepath.IsAbs(c.NSISInclude) {
		c.NSISInclude = filepath.Join(dir, c.NSISInclude)
	}
	for dst, src := range c.Files {
		if filepath.IsAbs(dst) || strings.HasPrefix(filepath.Clean(dst), "..") {
			return nil, fmt.Errorf("%s: files: %q must be a relative path inside the app", path, dst)
		}
		if !filepath.IsAbs(src) {
			c.Files[dst] = filepath.Join(dir, src)
		}
	}
	return &c, nil
}

// slug: the name in file names (Notes Pro → Notes-Pro).
func (c *Config) slug() string { return strings.ReplaceAll(strings.TrimSpace(c.Name), " ", "-") }

// exe: the executable's name (Exe, or notes-pro).
func (c *Config) exe() string {
	if c.Exe != "" {
		return c.Exe
	}
	return strings.ToLower(c.slug())
}

// files: Files for a target, sources with {os} and {arch} filled in.
func (c *Config) files(goos, arch string) map[string]string {
	out := map[string]string{}
	for dst, src := range c.Files {
		out[filepath.Clean(dst)] = strings.NewReplacer("{os}", goos, "{arch}", arch).Replace(src)
	}
	return out
}

// pkgName: <slug>-<version>-<os>-<arch>, the stem of every package build writes (what feed looks for).
func (c *Config) pkgName(goos, arch string) string {
	return fmt.Sprintf("%s-%s-%s-%s", c.slug(), c.Version, goos, arch)
}
