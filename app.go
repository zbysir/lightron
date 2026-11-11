package lightron

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
)

// App is a running app.
type App struct {
	opts  Options
	binds *bindings

	mu      sync.Mutex
	windows map[string]*Window
	quit    chan struct{}
	once    sync.Once

	eng engine // the platform's (app_darwin.go, app_windows.go)
}

// engine is what a platform provides.
type engine interface {
	newWindow(path string) error
	eval(w *Window, js string) error
	focus(w *Window)
	close(w *Window)
	setBadge(label string)
	call(w *Window, method string, params any) (json.RawMessage, error)
	setMenu()
	ownOrigin() string // where the app's pages start (Options.Origins are on top of it)
}

// Window is one window of the app.
type Window struct {
	app *App
	id  string
	// platform state
	session string
}

// ID is the window's identifier, stable while it is open.
func (w *Window) ID() string { return w.id }

// Eval runs JS in the window's page.
func (w *Window) Eval(js string) error { return w.app.eng.eval(w, js) }

// Emit calls the page's lightron.on(event, …) listeners with data (any JSON value).
func (w *Window) Emit(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e, _ := json.Marshal(event)
	return w.Eval("window.lightron && window.lightron.__emit(" + string(e) + "," + string(b) + ")")
}

// Focus brings the window and the app to the front.
func (w *Window) Focus() { w.app.eng.focus(w) }

// Call sends a DevTools protocol command to this window's page (Runtime.evaluate, Page.navigate,
// SystemInfo-style queries…) and returns its result. On Windows through WebView2's CallDevToolsProtocolMethod. See App.Call.
func (w *Window) Call(method string, params any) (json.RawMessage, error) {
	return w.app.eng.call(w, method, params)
}

// Close closes the window.
func (w *Window) Close() { w.app.eng.close(w) }

// NewWindow opens a window on path ("" = the page's root), or on a full http(s) URL (any page, e.g. one of
// Options.Origins). Not on Windows yet: it returns an error there while the window is open.
func (a *App) NewWindow(path string) error { return a.eng.newWindow(path) }

// Windows are the open windows.
func (a *App) Windows() []*Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws := make([]*Window, 0, len(a.windows))
	for _, w := range a.windows {
		ws = append(ws, w)
	}
	return ws
}

// Emit sends an event to every window (see Window.Emit).
func (a *App) Emit(event string, data any) {
	for _, w := range a.Windows() {
		_ = w.Emit(event, data)
	}
}

// SetBadge shows label on the app's Dock icon ("" clears it). macOS only.
func (a *App) SetBadge(label string) { a.eng.setBadge(label) }

// SetMenu replaces the macOS menu bar (Options.Menu): another language, the label of an update that is ready, keys
// the person changed. nil: the default menu.
func (a *App) SetMenu(menu []MenuItem) {
	a.mu.Lock()
	a.opts.Menu = menu
	a.mu.Unlock()
	a.eng.setMenu()
}

// SetDockMenu replaces the Dock icon's menu (Options.DockMenu).
func (a *App) SetDockMenu(menu []MenuItem) {
	a.mu.Lock()
	a.opts.DockMenu = menu
	a.mu.Unlock()
	a.eng.setMenu()
}

// menus: the current Menu and DockMenu.
func (a *App) menus() (menu, dock []MenuItem) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts.Menu, a.opts.DockMenu
}

// Call sends a DevTools protocol command to the browser (not to a page; see Window.Call) and returns its result:
// what Lightron has no API for. On Windows (one window, no browser-level session) it goes to the window's page. Lightron relies on the targets it attached
// to; don't detach from them or turn off domains it enabled (Runtime, Page, Inspector).
func (a *App) Call(method string, params any) (json.RawMessage, error) {
	return a.eng.call(nil, method, params)
}

// Quit closes the windows and makes Run return.
func (a *App) Quit() { a.once.Do(func() { close(a.quit) }) }

// FocusOrOpen brings a window up, or opens one if there is none.
func (a *App) FocusOrOpen() {
	if ws := a.Windows(); len(ws) > 0 {
		ws[0].Focus()
		return
	}
	_ = a.NewWindow(a.opts.Window.Path)
}

func (a *App) logf(format string, args ...any) { a.opts.Logf("lightron: "+format, args...) }

func (a *App) addWindow(w *Window) {
	a.mu.Lock()
	if a.windows == nil {
		a.windows = map[string]*Window{}
	}
	a.windows[w.id] = w
	a.mu.Unlock()
}

func (a *App) removeWindow(id string) (left int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.windows, id)
	return len(a.windows)
}

func (a *App) window(id string) *Window {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.windows[id]
}

// message handles what a page posted through the bridge: {t: "invoke" | "send" | …}.
func (a *App) message(w *Window, payload string) {
	var m struct {
		T    string            `json:"t"`
		ID   int64             `json:"id"`
		Name string            `json:"name"`
		Args []json.RawMessage `json:"args"`
		Msg  json.RawMessage   `json:"msg"`
		URL  string            `json:"url"`
	}
	if json.Unmarshal([]byte(payload), &m) != nil {
		return
	}
	switch m.T {
	case "invoke":
		go func() {
			res, err := a.binds.call(m.Name, w, m.Args)
			out, _ := json.Marshal(res)
			if err != nil {
				e, _ := json.Marshal(err.Error())
				_ = w.Eval("window.lightron.__settle(" + itoa(m.ID) + ",null," + string(e) + ")")
				return
			}
			_ = w.Eval("window.lightron.__settle(" + itoa(m.ID) + "," + string(out) + ",null)")
		}()
	case "send":
		if a.opts.OnMessage != nil {
			go a.opts.OnMessage(w, m.Msg)
		}
	case "open":
		go openExternal(m.URL)
	case "window":
		// window.open / a target=_blank link to another of the app's origins (bridge.js): a window of the app's, not the
		// default browser. Where there is only one window (Windows), the window goes there instead.
		u, err := url.Parse(m.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !appOrigin(u.Scheme+"://"+u.Host, a.eng.ownOrigin(), a.opts.Origins) {
			return
		}
		go func() {
			if err := a.NewWindow(u.String()); err != nil {
				b, _ := json.Marshal(u.String())
				_ = w.Eval("location.href = " + string(b))
			}
		}()
	case "focus":
		go w.Focus()
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func originOf(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return u
	}
	return pu.Scheme + "://" + pu.Host
}

// appOrigin: whether pages of this origin are the app's: the one it starts at, or one of Options.Origins ("*." for
// any subdomain). Same rule as pageScripts' check in the page.
func appOrigin(origin, start string, more []string) bool {
	if origin == "" || origin == "null" {
		return false
	}
	for _, p := range append([]string{start}, more...) {
		if p == origin {
			return true
		}
		if i := strings.Index(p, "://*."); i >= 0 && strings.HasPrefix(origin, p[:i+3]) && strings.HasSuffix(origin, p[i+4:]) && len(origin) > len(p)-1 {
			return true
		}
	}
	return false
}

// defaultHost: the last part of a reverse-DNS id, as a host name (com.example.Notes → notes).
func defaultHost(id string) string {
	h := strings.ToLower(id[strings.LastIndex(id, ".")+1:])
	var b strings.Builder
	for _, r := range h {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

// mapHost: the pages at http://<host>/ instead of 127.0.0.1:<port>. Chrome writes a web notification's origin under
// its title, and nothing removes it; "notes" reads better there. The browser maps the name to the real address
// (--host-resolver-rules) and counts it as a secure origin (notifications, clipboard need one), only in this
// profile. Only for http on this machine: https can't be redirected (the certificate), remote hosts stay as they are.
func mapHost(entry, host string) (string, []string) {
	u, err := url.Parse(entry)
	if err != nil || u.Scheme != "http" || !isLoopback(u.Host) || host == "" || host == "-" {
		return entry, nil
	}
	real := u.Host
	u.Host = host
	return u.String(), []string{
		"--host-resolver-rules=MAP " + host + " " + real,
		"--unsafely-treat-insecure-origin-as-secure=http://" + host,
	}
}
