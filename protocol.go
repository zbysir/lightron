package lightron

import (
	"strings"
)

// Links of the app's own schemes (Options.Protocols): what started the app (its arguments), what a second instance
// was started with (single.go), and on macOS what the engine received while running (menu_darwin.go) all end up in
// OnURL.

// urlsIn: the arguments that are links of one of the app's schemes.
func (a *App) urlsIn(args []string) []string {
	var out []string
	for _, arg := range args {
		if a.ownURL(arg) {
			out = append(out, arg)
		}
	}
	return out
}

func (a *App) ownURL(s string) bool {
	if strings.ContainsAny(s, "\r\n") {
		return false
	}
	for _, p := range a.opts.Protocols {
		if len(s) > len(p)+1 && strings.EqualFold(s[:len(p)+1], p+":") {
			return true
		}
	}
	return false
}

// openURLs hands links to OnURL, one by one (dropping what isn't one of the app's schemes).
func (a *App) openURLs(urls []string) {
	if a.opts.OnURL == nil {
		return
	}
	for _, u := range urls {
		if a.ownURL(u) {
			a.opts.OnURL(a, u)
		}
	}
}

// second: the app was opened again. With links and OnURL, they go there; otherwise OnSecondInstance (default: bring a
// window up).
func (a *App) second(urls []string) {
	if urls = a.urlsIn(urls); len(urls) > 0 && a.opts.OnURL != nil {
		a.openURLs(urls)
		return
	}
	if a.opts.OnSecondInstance != nil {
		a.opts.OnSecondInstance(a)
	} else if eng := a.engine(); eng != nil { // nil: still starting, its window is on its way
		eng.reopen()
	}
}
