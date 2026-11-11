//go:build darwin || windows

package lightron

import (
	"net"
	"net/http"
)

// A splash (Options.Splash): with URLFunc, the first window opens at once and shows it while URLFunc runs (a backend
// starting), then goes to the app's pages. Only with Host "-": a page host is set up when the engine starts (its
// switches name the port), before URLFunc has said where the pages are.

type urlResult struct {
	url string
	err error
}

func useSplash(o Options) bool { return o.Splash != "" && o.URLFunc != nil && o.Host == "-" }

// serveSplash serves the splash page on a loopback port, until closed. It is a plain page: no bridge, no scripts of
// the app's (the app's origin isn't known yet).
func serveSplash(html string, logf func(string, ...any)) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(html))
		}))
	}()
	return ln, nil
}
