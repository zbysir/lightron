// Command lightron starts, packages and publishes Lightron apps.
//
//	lightron init [dir]        a new app: main.go, a page, an icon, lightron.json
//	lightron build             the packages: macOS .app / .dmg / .zip, Windows installer / .zip
//	lightron feed              update.json for the update package, from what build wrote
//	lightron doctor            what this machine has for building and running
//
// lightron.json describes the app (name, id, version, icon); see docs/packaging.md.
package main

import (
	"fmt"
	"os"
)

const usage = `lightron: desktop apps on the system's Chromium (https://github.com/zbysir/lightron)

Usage:
  lightron init [dir]       create a new app in dir (default: the current directory)
  lightron build [flags]    build the packages into dist/ (see lightron build -h)
  lightron feed [flags]     write dist/update.json from the packages (see lightron feed -h)
  lightron doctor           check this machine
  lightron version          print lightron's version
`

// version: set at release (-X main.version).
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "feed":
		err = cmdFeed(os.Args[2:])
	case "doctor":
		err = cmdDoctor()
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "lightron: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lightron:", err)
		os.Exit(1)
	}
}
