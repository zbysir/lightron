//go:build !darwin && !windows

package lightron

import (
	"net/url"
	"os/exec"
)

func openExternal(u string) {
	if pu, err := url.Parse(u); err == nil && (pu.Scheme == "http" || pu.Scheme == "https" || pu.Scheme == "mailto") {
		_ = exec.Command("xdg-open", u).Start()
	}
}
