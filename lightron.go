// Package lightron makes desktop apps out of web pages, on a Chromium that is already on the machine instead of one
// shipped with every app (Electron's ~100MB). A Go program calls Run with the page to show; what users install is
// that program, a few MB.
//
//   - macOS: the user's own Chrome (or Edge, Brave, Chromium), run from a copy of its bundle renamed into the app (its
//     name, id and icon), so the Dock, ⌘Tab, the menu bar and notifications are the app's. Windows are --app windows:
//     a bare title bar. Lightron drives it over the DevTools protocol (a pipe, not a port).
//   - Windows: WebView2, the system's Chromium (Edge's), in a window of the app's own.
//
// The page talks to Go through window.lightron (see bridge.js): invoke a bound Go function, send a message, listen to
// events. Links to other sites open in the default browser. See README.md and docs/ for how it works and why.
package lightron

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// Options describe the app. Name, ID, and one of URL, URLFunc or Handler are required.
type Options struct {
	// Name is the app's name in the Dock / taskbar, ⌘Tab, the menu bar and notifications.
	Name string
	// ID is a reverse-DNS identifier (com.example.notes): macOS bundle ids, data directories.
	ID string
	// Version is the app's version (shown nowhere by Lightron; for your About box and updates).
	Version string
	// Icon is a square PNG, 512px or larger. On macOS it becomes the app's icon in the Dock.
	Icon []byte

	// URL is a page served by something else, e.g. a backend your app starts. An http URL on this machine is shown
	// at http://<Host>/ (see Host); https and remote URLs are shown as they are.
	URL string
	// URLFunc returns URL instead, once Lightron knows this is the only instance running (a second one only hands
	// over to the first and returns): for an app that starts its backend first and shows what that serves. An error
	// ends Run.
	URLFunc func() (string, error)
	// Handler serves the pages instead: Lightron runs it on a private loopback port that only the app's windows can
	// use (a secret cookie, set on the first load).
	Handler http.Handler
	// Host is the name pages are shown at (http://<Host>/), on macOS. Chrome writes the page's origin under every
	// web notification; "notes" reads better there than "127.0.0.1:53817". Default: the last part of ID, lowercased.
	// "-": pages stay at their real address (a backend that checks the Host header).
	Host string

	// Window is the first window. Path is relative to the page's root ("/" by default).
	Window WindowOptions

	// Menu is the macOS menu bar: its first item's Submenu is the app menu, the others are menus in order. Items
	// with Role "editMenu" / "windowMenu" stand for the browser's own Edit and Window menus (they handle copy /
	// paste, the window list). nil: an app menu (Hide, Quit…), Edit, Window.
	Menu []MenuItem
	// DockMenu is the menu of the app's Dock icon on macOS (above the window list and Quit macOS adds).
	DockMenu []MenuItem

	// Bind are Go functions the page can call: await lightron.invoke("name", ...args). Arguments arrive as JSON
	// decoded into the parameters' types; return a value, an error, or (value, error). A first parameter of type
	// *Window receives the window that called.
	Bind map[string]any
	// Script is JavaScript run in every page of the app before the page's own scripts, after window.lightron is set
	// up: a compatibility layer for an existing web app (window.yourNative = …), settings to seed.
	Script string
	// PageScript is JavaScript run in every page a window shows, of any site, before the page's own scripts: for an
	// app whose windows also show other sites (it can add to them, e.g. a panel of the app's own in an iframe).
	// window.lightron is there only on the app's own pages (the origin of URL / URLFunc / Handler, and Origins):
	// other sites never get the bridge, nor do Script or Bind reach them.
	PageScript string
	// Origins are more origins whose pages are the app's (bridge, Script, Bind), besides the one it starts at:
	// "http://127.0.0.1:7000", or "http://*.localhost:7000" for any subdomain.
	Origins []string
	// BrowserArgs are extra command-line switches for the engine (macOS) / WebView2 (Windows), e.g.
	// "--disable-features=…". They apply to the app's own profile only.
	BrowserArgs []string
	// UserAgentSuffix is appended to the browser's User-Agent, in every window (" MyApp/1.2"), so sites can tell
	// they are inside the app. macOS only for now.
	UserAgentSuffix string
	// OnMessage receives what the page sends with lightron.send(value).
	OnMessage func(w *Window, msg json.RawMessage)
	// OnReady runs once the first window has been asked for.
	OnReady func(a *App)
	// OnSecondInstance runs when the app is opened again while running (only one runs at a time). Default: bring a
	// window up, or open one.
	OnSecondInstance func(a *App)

	// KeepRunning keeps the app running when its last window closes (macOS: the Dock icon reopens one). Default:
	// quit.
	KeepRunning bool
	// DataDir holds Lightron's state (the browser profile, the engine copy on macOS). Default:
	// ~/Library/Application Support/<ID>/lightron, %LOCALAPPDATA%\<ID>\lightron.
	DataDir string
	// Logf receives Lightron's log lines. Default: log.Printf.
	Logf func(format string, args ...any)
}

// WindowOptions describe a window.
type WindowOptions struct {
	Path          string
	Width, Height int // default 1200 × 800
}

// MenuItem is a menu item, a separator, or a submenu.
type MenuItem struct {
	Label string
	// Accelerator is shown next to the item and triggers it when the page doesn't take the key: "Cmd+Shift+K",
	// "Cmd+,", "Ctrl+Enter", "F5".
	Accelerator string
	// Role makes it a standard item (Label and Click may be empty): hide, hideOthers, unhide, quit, undo, redo, cut,
	// copy, paste, selectAll, minimize, zoom, front, close, toggleFullScreen, zoomIn, zoomOut, resetZoom, devTools,
	// reload; and, at the top level of Menu, editMenu and windowMenu.
	Role string
	// Click runs when the item is chosen, with the focused window (nil if none).
	Click func(w *Window)
	// Submenu makes it a menu.
	Submenu []MenuItem
	// Separator makes it a separator line.
	Separator bool
}

// Separator is a menu separator line.
var Separator = MenuItem{Separator: true}

// ErrUnsupported is returned by Run on a platform Lightron doesn't run on yet (Linux), and by what a platform
// doesn't have (Window.Call on Windows).
var ErrUnsupported = errors.New("lightron: this platform isn't supported yet")

// Run shows the app and blocks until it quits. Call it from main.
func Run(opts Options) error {
	if opts.Name == "" || opts.ID == "" {
		return errors.New("lightron: Options.Name and Options.ID are required")
	}
	set := 0
	for _, ok := range []bool{opts.URL != "", opts.URLFunc != nil, opts.Handler != nil} {
		if ok {
			set++
		}
	}
	if set != 1 {
		return errors.New("lightron: set one of Options.URL, Options.URLFunc and Options.Handler")
	}
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	if opts.Window.Width == 0 {
		opts.Window.Width = 1200
	}
	if opts.Window.Height == 0 {
		opts.Window.Height = 800
	}
	if opts.Host == "" {
		opts.Host = defaultHost(opts.ID)
	}
	b, err := newBindings(opts.Bind)
	if err != nil {
		return err
	}
	a := &App{opts: opts, binds: b}
	return a.run()
}
