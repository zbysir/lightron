package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zbysir/lightron/update"
)

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "lightron.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	c, err := loadConfig(writeConfig(t, dir, `{"name": "Notes Pro", "id": "com.example.notes", "icon": "i.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "0.0.0" || c.Main != "." || c.Out != filepath.Join(dir, "dist") || c.Icon != filepath.Join(dir, "i.png") || len(c.Targets) != 3 {
		t.Fatalf("defaults: %+v", c)
	}
	if c.slug() != "Notes-Pro" || c.exe() != "notes-pro" || c.pkgName("darwin", "arm64") != "Notes-Pro-0.0.0-darwin-arm64" {
		t.Fatalf("names: %s %s %s", c.slug(), c.exe(), c.pkgName("darwin", "arm64"))
	}
	for _, bad := range []string{`{"id": "com.example.x"}`, `{"name": "X", "id": "notes"}`, `{`} {
		if _, err := loadConfig(writeConfig(t, t.TempDir(), bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

func TestFeed(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `{"name": "Notes", "id": "com.example.notes", "version": "1.0.0"}`)
	dist := filepath.Join(dir, "dist")
	_ = os.MkdirAll(dist, 0o755)
	for _, f := range []string{"Notes-1.2.0-darwin-arm64.dmg", "Notes-1.2.0-darwin-arm64.zip", "Notes-1.2.0-darwin-amd64.zip", "Notes-1.2.0-windows-amd64.zip", "Notes-1.1.0-windows-amd64-setup.exe"} {
		_ = os.WriteFile(filepath.Join(dist, f), []byte(f), 0o644)
	}
	if err := cmdFeed([]string{"-config", cfg, "-version", "v1.2.0", "-notes", "hi"}); err != nil {
		t.Fatal(err)
	}
	var f update.Feed
	b, _ := os.ReadFile(filepath.Join(dist, "update.json"))
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	// darwin prefers the .dmg; amd64 has only the .zip; windows has no installer of this version.
	if f.Version != "1.2.0" || f.Notes != "hi" || len(f.Packages) != 2 ||
		f.Packages["darwin-arm64"].File != "Notes-1.2.0-darwin-arm64.dmg" || f.Packages["darwin-amd64"].File != "Notes-1.2.0-darwin-amd64.zip" {
		t.Fatalf("feed: %s", b)
	}
	if p := f.Packages["darwin-arm64"]; p.Size != int64(len(p.File)) || len(p.Sha256) != 64 {
		t.Fatalf("package: %+v", p)
	}
}
