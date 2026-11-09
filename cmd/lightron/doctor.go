package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// cmdDoctor: what this machine can build, and what a Lightron app finds here when it runs.
func cmdDoctor() error {
	fmt.Printf("lightron %s on %s/%s\n\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Println("Building")
	if out, err := exec.Command("go", "version").Output(); err == nil {
		check(true, "go", strings.TrimSpace(strings.TrimPrefix(string(out), "go version ")))
	} else {
		check(false, "go", "not found: https://go.dev/dl/")
	}
	if runtime.GOOS == "darwin" {
		check(have("codesign"), "codesign", "signs the .app (ad hoc); Xcode command line tools")
		check(have("hdiutil"), "hdiutil", "makes the .dmg")
	} else {
		fmt.Println("  -  .dmg         macOS only (the .zip is built anywhere)")
	}
	check(have("makensis"), "makensis", "the Windows installer; brew install makensis / apt install nsis")

	fmt.Println("\nRunning")
	runtimeChecks()
	return nil
}

func check(ok bool, what, note string) {
	mark := "✓"
	if !ok {
		mark = "✗"
	}
	fmt.Printf("  %s  %-11s %s\n", mark, what, note)
}
