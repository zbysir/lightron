package lightron

import (
	"net/url"
	"os/exec"
)

// openExternal opens an http(s) / mailto URL in the default browser / mail app.
func openExternal(u string) {
	if pu, err := url.Parse(u); err == nil && (pu.Scheme == "http" || pu.Scheme == "https" || pu.Scheme == "mailto") {
		_ = exec.Command("open", u).Start()
	}
}
