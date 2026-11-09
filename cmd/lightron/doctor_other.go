//go:build !darwin && !windows

package main

import "fmt"

func runtimeChecks() {
	fmt.Println("  -  apps don't run on this platform yet (macOS, Windows); building for them works")
}
