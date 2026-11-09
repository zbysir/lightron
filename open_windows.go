package lightron

import (
	"net/url"

	"golang.org/x/sys/windows"
)

// openExternal opens an http(s) / mailto URL in the default browser / mail app.
func openExternal(u string) {
	pu, err := url.Parse(u)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https" && pu.Scheme != "mailto") {
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	t, _ := windows.UTF16PtrFromString(u)
	_ = windows.ShellExecute(0, verb, t, nil, nil, windows.SW_SHOWNORMAL)
}
