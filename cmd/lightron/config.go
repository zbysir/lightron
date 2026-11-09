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
	return &c, nil
}

// slug: the name in file names (Notes Pro → Notes-Pro).
func (c *Config) slug() string { return strings.ReplaceAll(strings.TrimSpace(c.Name), " ", "-") }

// exe: the executable's name (notes-pro).
func (c *Config) exe() string { return strings.ToLower(c.slug()) }

// pkgName: <slug>-<version>-<os>-<arch>, the stem of every package build writes (what feed looks for).
func (c *Config) pkgName(goos, arch string) string {
	return fmt.Sprintf("%s-%s-%s-%s", c.slug(), c.Version, goos, arch)
}
