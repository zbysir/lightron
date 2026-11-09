package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zbysir/lightron/update"
)

// The package each platform updates with, by preference: what update.Install knows how to put in place.
var feedKinds = map[string][]string{"darwin": {".dmg", ".zip"}, "windows": {"-setup.exe"}}

func cmdFeed(args []string) error {
	fs := flag.NewFlagSet("feed", flag.ExitOnError)
	cfgPath := fs.String("config", "lightron.json", "the app's description")
	ver := fs.String("version", "", "the version the packages were built as (default: lightron.json's)")
	notes := fs.String("notes", "", "release notes shown with the update (or @file)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: lightron feed [flags]

Writes dist/update.json for the update package: the version and, per platform, the package lightron build wrote
(macOS: the .dmg, else the .zip; Windows: the -setup.exe), with its size and sha256. Upload it with the packages,
side by side, e.g. as assets of a GitHub release; the app's FeedURL is then
https://github.com/<owner>/<repo>/releases/latest/download/update.json.

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
	if update.ParseVersion(c.Version) == nil {
		return fmt.Errorf("version %q: want x.y.z", c.Version)
	}
	if strings.HasPrefix(*notes, "@") {
		b, err := os.ReadFile((*notes)[1:])
		if err != nil {
			return err
		}
		*notes = strings.TrimSpace(string(b))
	}
	f := update.Feed{Version: c.Version, Notes: *notes, Packages: map[string]update.Package{}}
	for _, t := range c.Targets {
		goos, arch, _ := strings.Cut(t, "/")
		for _, kind := range feedKinds[goos] {
			file := c.pkgName(goos, arch) + kind
			p, err := describe(filepath.Join(c.Out, file))
			if err != nil {
				continue
			}
			f.Packages[goos+"-"+arch] = p
			break
		}
	}
	if len(f.Packages) == 0 {
		return fmt.Errorf("no packages of %s %s in %s (lightron build first)", c.Name, c.Version, c.Out)
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	out := filepath.Join(c.Out, "update.json")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	keys := make([]string, 0, len(f.Packages))
	for k := range f.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println(out)
	for _, k := range keys {
		fmt.Printf("  %-14s %s\n", k, f.Packages[k].File)
	}
	return nil
}

func describe(path string) (update.Package, error) {
	fh, err := os.Open(path)
	if err != nil {
		return update.Package{}, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return update.Package{}, err
	}
	return update.Package{File: filepath.Base(path), Size: n, Sha256: hex.EncodeToString(h.Sum(nil))}, nil
}
