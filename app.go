package lightron

import (
	"encoding/json"
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

// Close closes the window.
func (w *Window) Close() { w.app.eng.close(w) }

// NewWindow opens a window on path ("" = the page's root). Not on Windows yet.
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
	case "focus":
		go w.Focus()
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
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
