package main

import (
	"os/exec"
	"strings"

	"github.com/zbysir/lightron/internal/macengine"
)

func runtimeChecks() {
	exe, err := macengine.FindBrowser()
	if err != nil {
		check(false, "browser", err.Error()+": apps show a prompt to install Chrome")
		return
	}
	app := macengine.App(exe)
	ver := ""
	if out, err := exec.Command("defaults", "read", app+"/Contents/Info.plist", "CFBundleShortVersionString").Output(); err == nil {
		ver = " " + strings.TrimSpace(string(out))
	}
	check(true, "browser", app+ver)
}
