package lightron

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	app  *App
	wv   webview2.WebView
	hwnd uintptr
	main *Window
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
	e.main = &Window{app: a, id: "main"}
	a.addWindow(e.main)
	if len(a.opts.Icon) > 0 {
		e.setIcon(a.opts.Icon)
	}
	if err := w.Bind("__lightronPost", func(payload string) { e.message(payload) }); err != nil {
		return err
	}
	w.Init(bridgeJS + "\n" + a.opts.Script)
	w.Navigate(pageURL(entry, a.opts.Window.Path))
	go func() {
		<-a.quit
		w.Dispatch(w.Terminate)
	}()
	if a.opts.OnReady != nil {
		go a.opts.OnReady(a)
	}
	w.Run()
	a.Quit()
	return nil
}

// message: the binding runs on the UI thread; nothing slow here.
func (e *win) message(payload string) {
	var p struct {
		T     string `json:"t"`
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	switch p.T {
	case "path": // a file's path isn't reachable through this WebView2 binding
		go func() { _ = e.eval(e.main, "window.lightron.__settle("+itoa(p.ID)+",\"\",null)") }()
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

func (e *win) close(*Window) { e.app.Quit() }

func (e *win) setBadge(string) {}

func (e *win) call(*Window, string, any) (json.RawMessage, error) { return nil, ErrUnsupported }

func (e *win) setMenu() {}

// setIcon: the window's big (Alt+Tab, taskbar) and small (title bar) icons from the PNG, at runtime: no resource
// compiler needed. (lightron build also puts it in the exe, for Explorer.)
func (e *win) setIcon(png []byte) {
	img, err := pack.DecodePNG(png)
	if err != nil {
		return
	}
	data := pack.PNG(pack.Scale(img, 256))
	for _, size := range []struct{ which, mx, my uintptr }{{1, 11, 12}, {0, 49, 50}} { // ICON_BIG: SM_CXICON; ICON_SMALL: SM_CXSMICON
		cx, _, _ := procGetMetrics.Call(size.mx)
		cy, _, _ := procGetMetrics.Call(size.my)
		icon, _, _ := procIconFromRes.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, cx, cy, 0)
		if icon != 0 {
			_, _, _ = procSendMessage.Call(e.hwnd, 0x0080 /* WM_SETICON */, size.which, icon)
		}
	}
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	_, _, _ = procMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10 /* MB_ICONERROR */)
}
