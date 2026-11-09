//go:build darwin

// Package macmenu holds the macOS menu library (objc/menu.m, built into lightronmenu.dylib by `make menulib`) and writes it
// out for the engine to load.
package macmenu

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
)

//go:embed lightronmenu.dylib
var dylib []byte

// Path writes the library into dir (once per build of it) and returns where it is.
func Path(dir string) (string, error) {
	sum := sha256.Sum256(dylib)
	p := filepath.Join(dir, "lightronmenu-"+hex.EncodeToString(sum[:6])+".dylib")
	if st, err := os.Stat(p); err == nil && st.Size() == int64(len(dylib)) {
		return p, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, dylib, 0o755); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
