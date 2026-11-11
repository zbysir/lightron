//go:build darwin || windows

package lightron

import (
	"encoding/json"
	"net/url"
)

// pageScripts: what runs in every page before its own scripts. The bridge and Options.Script only on the app's own
// origins (appOrigin); Options.PageScript everywhere.
func pageScripts(origin string, opts Options) string {
	o, _ := json.Marshal(append([]string{origin}, opts.Origins...))
	return "if (" + string(o) + ".some((p) => p === location.origin || p.includes('://*.') && location.origin.endsWith(p.slice(p.indexOf('*.') + 1)) && location.origin.startsWith(p.slice(0, p.indexOf('*.'))))) {\n" +
		bridgeJS + "\n" + opts.Script + "\n}\n" + opts.PageScript
}

// externalOpen: a bridge message asking to open an http(s) address in the default browser
func externalOpen(payload string) (string, bool) {
	var m struct {
		T   string `json:"t"`
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(payload), &m) != nil || m.T != "open" {
		return "", false
	}
	u, err := url.Parse(m.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return u.String(), true
}
