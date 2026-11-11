package lightron

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/zbysir/lightron/internal/pack"
)

//go:embed bridge.js
var bridgeJS string

// ErrNoBrowser: no WebView2 runtime (Run has told the user, with Microsoft's download page). Rare: Windows 11 has it,
// Windows 10 gets it with updates.
var ErrNoBrowser = errors.New("lightron: the Microsoft Edge WebView2 Runtime isn't installed")

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procMessageBox  = user32.NewProc("MessageBoxW")
	procSendMessage = user32.NewProc("SendMessageW")
	procShowWindow  = user32.NewProc("ShowWindow")
	procSetFore     = user32.NewProc("SetForegroundWindow")
	procIsIconic    = user32.NewProc("IsIconic")
	procGetMetrics  = user32.NewProc("GetSystemMetrics")
	procIconFromRes = user32.NewProc("CreateIconFromResourceEx")
	procDpiContext  = user32.NewProc("SetProcessDpiAwarenessContext")
)

// win is the Windows engine: WebView2 in a window of the app's own. One window (NewWindow isn't supported yet).
type win struct {
	app      *App
	wv       webview2.WebView
	hwnd     uintptr
	main     *Window
	core     unsafe.Pointer // the ICoreWebView2 (cdp_windows.go); nil if unreachable
	core11   unsafe.Pointer // its ICoreWebView2_11 (sessions: frames_windows.go); nil on an older WebView2
	origin   string         // the app's own origin (bridge calls from other sites are dropped)
	tray     *tray          // KeepRunning (tray_windows.go)
	quitting bool           // closing for real: WM_CLOSE isn't a hide
}

func (a *App) run() error {
	a.quit = make(chan struct{})
	// Sharp on high-DPI screens (a manifest's <dpiAwareness>PerMonitorV2</dpiAwareness>, for apps built without one)
	_, _, _ = procDpiContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	dir := a.opts.DataDir
	if dir == "" {
		dir = filepath.Join(os.Getenv("LOCALAPPDATA"), a.opts.ID, "lightron")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	e := &win{app: a}
	a.eng = e
	ln, other := claimInstance(dir, func() {
		if a.opts.OnSecondInstance != nil {
			a.opts.OnSecondInstance(a)
		} else {
			a.FocusOrOpen()
		}
	})
	if other {
		return nil
	}
	if ln != nil {
		defer ln.Close()
	}
	entry := a.opts.URL
	if a.opts.URLFunc != nil {
		var err error
		if entry, err = a.opts.URLFunc(); err != nil {
			return err
		}
	}
	if a.opts.Handler != nil {
		srv, err := serve(a.opts.Handler, a.opts.Logf)
		if err != nil {
			return err
		}
		defer srv.close()
		entry = srv.entry
	}

	real := entry
	entry, extra := mapHost(entry, a.opts.Host)
	mapped := entry != real
	extra = append(extra, a.opts.BrowserArgs...)
	if len(extra) > 0 {
		// One string, which WebView2 splits as a command line: a switch with spaces in it (the host rule) is quoted.
		args := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
		for _, x := range extra {
			args += " " + quoteArg(x)
		}
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", strings.TrimSpace(args))
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		// Debug turns on WebView2's right-click menu and F12 (developer tools); bridge.js drops the menu where it
		// isn't useful. Off, both are gone, cut / copy / paste in fields included.
		Debug:     true,
		AutoFocus: true,
		DataPath:  filepath.Join(dir, "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title: a.opts.Name, Width: uint(a.opts.Window.Width), Height: uint(a.opts.Window.Height), Center: true,
		},
	})
	if w == nil {
		messageBox(a.opts.Name, a.opts.Name+" needs the Microsoft Edge WebView2 Runtime. Click OK to download it, then open "+a.opts.Name+" again.")
		openExternal("https://go.microsoft.com/fwlink/p/?LinkId=2124703")
		return ErrNoBrowser
	}
	defer w.Destroy()
	e.wv, e.hwnd = w, uintptr(w.Window())
	e.core = coreWebView(w)
	e.origin = originOf(entry)
	e.main = &Window{app: a, id: "main"}
	a.addWindow(e.main)
	var small uintptr
	if len(a.opts.Icon) > 0 {
		small = e.setIcon(a.opts.Icon)
	}
	if a.opts.KeepRunning {
		e.keepRunning(small)
	}
	if err := w.Bind("__lightronPost", func(payload string) { e.message(payload) }); err != nil {
		return err
	}
	scriptOpts := a.opts
	if mapped { // the bridge also at the real address, where the pages go if the host doesn't take (below)
		scriptOpts.Origins = append([]string{originOf(real)}, a.opts.Origins...)
	}
	w.Init(pageScripts(originOf(entry), scriptOpts))
	first := pageURL(entry, a.opts.Window.Path)
	// Before the first page, over the DevTools protocol (it needs the message loop: after Run starts): the
	// permissions, the User-Agent.
	go func() {
		if !e.grantPermissions() && mapped {
			// WebView2 didn't take the host switches: some don't read WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS (seen on
			// GitHub's Windows runners, WebView2 156), and the origin they would make secure can't be granted
			// anything. The pages stay at their real address, as before Host on Windows.
			a.logf("the page host %q didn't take; pages at %s", a.opts.Host, originOf(real))
			e.origin = originOf(real)
			first = pageURL(real, a.opts.Window.Path)
			e.grantPermissions()
		}
		if a.opts.UserAgentSuffix != "" {
			if r, err := e.cdpCall("Browser.getVersion", nil); err != nil {
				a.logf("user agent: %v", err)
			} else {
				var v struct {
					UserAgent string `json:"userAgent"`
				}
				_ = json.Unmarshal(r, &v)
				if _, err := e.cdpCall("Network.setUserAgentOverride", map[string]any{"userAgent": v.UserAgent + a.opts.UserAgentSuffix}); err != nil {
					a.logf("user agent: %v", err)
				}
			}
		}
		e.watchFrames()
		w.Dispatch(func() { w.Navigate(first) })
	}()
	go func() {
		<-a.quit
		w.Dispatch(func() {
			e.quitting = true
			if e.tray != nil {
				e.tray.remove()
			}
			w.Terminate()
		})
	}()
	if a.opts.OnReady != nil {
		go a.opts.OnReady(a)
	}
	w.Run()
	if e.tray != nil {
		e.tray.remove()
	}
	a.Quit()
	return nil
}

// grantPermissions: notifications and the clipboard without asking, for the app's own pages, as on macOS: WebView2
// would otherwise ask on the first notification and on reading the clipboard (paste buttons). One by one, as on macOS
// (Browser.grantPermissions denies every permission it doesn't list). Other sites a window shows keep WebView2's
// default (it asks). False if one was refused.
func (e *win) grantPermissions() bool {
	ok := true
	for _, p := range []string{"notifications", "clipboard-read", "clipboard-write"} {
		if _, err := e.cdpCall("Browser.setPermission", map[string]any{
			"origin": e.origin, "permission": map[string]any{"name": p, "allowWithoutSanitization": p == "clipboard-write"}, "setting": "granted",
		}); err != nil {
			e.app.logf("permission %s: %v", p, err)
			ok = false
		}
	}
	return ok
}

// message: the binding runs on the UI thread; nothing slow here. Only the app's own pages may use it
// (Options.PageScript): the window can show other sites, and the binding is there on every page.
func (e *win) message(payload string) {
	// Opening an http(s) link in the default browser is fine from any page: WebView2 puts the bridge in iframes too
	// (a page of the app's own inside another site), whose calls the origin check below can't tell from the page's
	if u, ok := externalOpen(payload); ok {
		go openExternal(u)
		return
	}
	if e.core != nil {
		if o := originOf(source(e.core)); !appOrigin(o, e.origin, e.app.opts.Origins) {
			e.app.logf("bridge: a call from %q ignored (not the app's page)", o)
			return
		}
	}
	var p struct {
		T     string `json:"t"`
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	switch p.T {
	case "path":
		go e.answerPath("", 0, p.ID)
	case "title":
		t := p.Title
		if t == "" {
			t = e.app.opts.Name
		}
		e.wv.Dispatch(func() { e.wv.SetTitle(t) })
	default:
		e.app.message(e.main, payload)
	}
}

func (e *win) newWindow(string) error {
	if e.main != nil {
		return errors.New("lightron: more than one window isn't supported on Windows yet")
	}
	return nil
}

func (e *win) eval(_ *Window, js string) error {
	e.wv.Dispatch(func() { e.wv.Eval(js) })
	return nil
}

func (e *win) focus(*Window) {
	e.wv.Dispatch(func() {
		if r, _, _ := procIsIconic.Call(e.hwnd); r != 0 {
			_, _, _ = procShowWindow.Call(e.hwnd, 9) // SW_RESTORE
		}
		_, _, _ = procShowWindow.Call(e.hwnd, 5) // SW_SHOW
		_, _, _ = procSetFore.Call(e.hwnd)
	})
}

// close: with KeepRunning, the window hides (the icon in the notification area brings it back); else the app quits.
func (e *win) close(*Window) {
	if e.tray != nil {
		e.wv.Dispatch(func() { _, _, _ = procShowWindow.Call(e.hwnd, 0) })
		return
	}
	e.app.Quit()
}

func (e *win) setBadge(string) {}

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

// call: the DevTools protocol on the window's page (one window: App.Call goes there too).
func (e *win) call(_ *Window, method string, params any) (json.RawMessage, error) {
	return e.cdpCall(method, params)
}

func (e *win) setMenu() {}

// setIcon: the window's big (Alt+Tab, taskbar) and small (title bar) icons from the PNG, at runtime: no resource
// compiler needed. (lightron build also puts it in the exe, for Explorer.)
func (e *win) setIcon(png []byte) (small uintptr) {
	img, err := pack.DecodePNG(png)
	if err != nil {
		return 0
	}
	data := pack.PNG(pack.Scale(img, 256))
	for _, size := range []struct{ which, mx, my uintptr }{{1, 11, 12}, {0, 49, 50}} { // ICON_BIG: SM_CXICON; ICON_SMALL: SM_CXSMICON
		cx, _, _ := procGetMetrics.Call(size.mx)
		cy, _, _ := procGetMetrics.Call(size.my)
		icon, _, _ := procIconFromRes.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, cx, cy, 0)
		if icon != 0 {
			_, _, _ = procSendMessage.Call(e.hwnd, 0x0080 /* WM_SETICON */, size.which, icon)
			if size.which == 0 {
				small = icon
			}
		}
	}
	return small
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	_, _, _ = procMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10 /* MB_ICONERROR */)
}

// quoteArg: a switch as one word of a Windows command line ("--k=v w" → --k="v w"; Chromium drops the quotes).
func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t") {
		return s
	}
	if k, v, ok := strings.Cut(s, "="); ok && strings.HasPrefix(k, "--") && !strings.ContainsAny(k, " \t") {
		return k + "=\"" + v + "\""
	}
	return "\"" + s + "\""
}
