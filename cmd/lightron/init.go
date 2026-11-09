package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed template
var scaffold embed.FS

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	name := fs.String("name", "", "the app's name (default: the directory's, capitalized)")
	id := fs.String("id", "", "reverse-DNS id (default: com.example.<name>)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: lightron init [flags] [dir]\n\nWrites main.go, web/index.html, icon.png and lightron.json into dir (made if needed), and go.mod if\nthere is none. Existing files are left alone.\n\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if *name == "" {
		base := filepath.Base(abs)
		*name = strings.ToUpper(base[:1]) + base[1:]
	}
	if *id == "" {
		*id = "com.example." + regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(*name), "")
	}
	if !idRe.MatchString(*id) {
		return fmt.Errorf("id must be reverse-DNS, like com.example.notes (got %q)", *id)
	}
	if err := os.MkdirAll(filepath.Join(abs, "web"), 0o755); err != nil {
		return err
	}
	data := struct{ Name, ID string }{*name, *id}
	cfg, _ := json.MarshalIndent(Config{Name: *name, ID: *id, Version: "0.1.0", Icon: "icon.png"}, "", "  ")
	files := map[string][]byte{"icon.png": defaultIcon, "lightron.json": append(cfg, '\n')}
	for src, dst := range map[string]string{"template/main.go.tmpl": "main.go", "template/web/index.html": "web/index.html"} {
		raw, _ := scaffold.ReadFile(src)
		var b strings.Builder
		if err := template.Must(template.New(src).Parse(string(raw))).Execute(&b, data); err != nil {
			return err
		}
		files[dst] = []byte(b.String())
	}
	for _, f := range []string{"main.go", "web/index.html", "icon.png", "lightron.json"} {
		p := filepath.Join(abs, f)
		if _, err := os.Stat(p); err == nil {
			fmt.Println("  kept   ", f)
			continue
		}
		if err := os.WriteFile(p, files[f], 0o644); err != nil {
			return err
		}
		fmt.Println("  wrote  ", f)
	}
	if _, err := os.Stat(filepath.Join(abs, "go.mod")); errors.Is(err, os.ErrNotExist) {
		if err := run(abs, "go", "mod", "init", strings.ToLower(regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(*name, "-"))); err != nil {
			return err
		}
	}
	dep := "github.com/zbysir/lightron@latest"
	if version != "dev" {
		dep = "github.com/zbysir/lightron@v" + strings.TrimPrefix(version, "v")
	}
	if err := run(abs, "go", "get", dep); err != nil {
		fmt.Printf("\ngo get failed; run it yourself in %s:\n  go get %s && go mod tidy\n", dir, dep)
		return nil
	}
	_ = run(abs, "go", "mod", "tidy")
	fmt.Printf("\nDone. In %s:\n  go run .           run it\n  lightron build     package it into dist/\n", dir)
	return nil
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
