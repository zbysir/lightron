//go:build darwin || windows

package lightron

import "encoding/json"

// pageScripts: what runs in every page before its own scripts. The bridge and Options.Script only on the app's own
// origins (appOrigin); Options.PageScript everywhere.
func pageScripts(origin string, opts Options) string {
	o, _ := json.Marshal(append([]string{origin}, opts.Origins...))
	return "if (" + string(o) + ".some((p) => p === location.origin || p.includes('://*.') && location.origin.endsWith(p.slice(p.indexOf('*.') + 1)) && location.origin.startsWith(p.slice(0, p.indexOf('*.'))))) {\n" +
		bridgeJS + "\n" + opts.Script + "\n}\n" + opts.PageScript
}
