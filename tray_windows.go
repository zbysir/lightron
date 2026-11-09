package lightron

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// KeepRunning on Windows: closing the window hides it, and an icon in the notification area brings it back (click)
// or quits (its menu: Open, the DockMenu's items, Quit). Opening the app again also brings it back (one instance).
// The window's procedure is go-webview2's; it is subclassed (SetWindowLongPtr) for WM_CLOSE and the icon's messages.

var (
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	procShellNotify    = shell32.NewProc("Shell_NotifyIconW")
	procSetWindowLong  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc = user32.NewProc("CallWindowProcW")
	procCreatePopup    = user32.NewProc("CreatePopupMenu")
	procAppendMenu     = user32.NewProc("AppendMenuW")
	procTrackPopup     = user32.NewProc("TrackPopupMenu")
	procDestroyMenu    = user32.NewProc("DestroyMenu")
	procGetCursorPos   = user32.NewProc("GetCursorPos")
	procPostMessage    = user32.NewProc("PostMessageW")
	procUILanguage     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
)

const (
	wmClose     = 0x0010
	wmNull      = 0x0000
	wmTray      = 0x8000 + 0x4c // WM_APP + n: the icon's callback message
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	nimAdd      = 0
	nimDelete   = 2
	nifMessage  = 1
	nifIcon     = 2
	nifTip      = 4
	gwlpWndProc = ^uintptr(3) // -4
)

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type tray struct {
	e       *win
	oldProc uintptr
	nid     notifyIconData
	items   []MenuItem // index+1 is the menu command id; 0 is "nothing chosen"
	added   bool
}

// keepRunning hides the window on close and puts the icon up (on the UI thread, before Run).
func (e *win) keepRunning(icon uintptr) {
	t := &tray{e: e}
	e.tray = t
	t.oldProc, _, _ = procSetWindowLong.Call(e.hwnd, gwlpWndProc, windows.NewCallback(t.wndProc))
	t.nid = notifyIconData{HWnd: e.hwnd, UID: 1, UFlags: nifMessage | nifIcon | nifTip, UCallbackMessage: wmTray, HIcon: icon}
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	tip, _ := windows.UTF16FromString(e.app.opts.Name)
	copy(t.nid.SzTip[:len(t.nid.SzTip)-1], tip)
	r, _, _ := procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	t.added = r != 0
}

func (t *tray) remove() {
	if t.added {
		_, _, _ = procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		t.added = false
	}
}

func (t *tray) wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmClose:
		if !t.e.quitting {
			_, _, _ = procShowWindow.Call(hwnd, 0) // SW_HIDE
			return 0
		}
	case wmTray:
		switch lp & 0xffff {
		case wmLButtonUp:
			t.e.focus(nil)
			return 0
		case wmRButtonUp:
			t.menu()
			return 0
		}
	}
	r, _, _ := procCallWindowProc.Call(t.oldProc, hwnd, msg, wp, lp)
	return r
}

// menu: Open, the DockMenu's items, Quit. Chosen with TPM_RETURNCMD, run here.
func (t *tray) menu() {
	open, quit := "Open "+t.e.app.opts.Name, "Quit "+t.e.app.opts.Name
	if lang, _, _ := procUILanguage.Call(); lang&0x3ff == 0x04 { // LANG_CHINESE
		open, quit = "打开 "+t.e.app.opts.Name, "退出 "+t.e.app.opts.Name
	}
	_, dock := t.e.app.menus()
	t.items = []MenuItem{{Label: open, Click: func(*Window) { t.e.focus(nil) }}}
	t.items = append(t.items, Separator)
	for _, it := range dock {
		if it.Separator || it.Click != nil && it.Role == "" && len(it.Submenu) == 0 {
			t.items = append(t.items, it)
		}
	}
	t.items = append(t.items, Separator, MenuItem{Label: quit, Click: func(*Window) { t.e.app.Quit() }})
	m, _, _ := procCreatePopup.Call()
	defer procDestroyMenu.Call(m)
	for i, it := range t.items {
		if it.Separator {
			_, _, _ = procAppendMenu.Call(m, 0x800 /* MF_SEPARATOR */, 0, 0)
			continue
		}
		s, _ := windows.UTF16PtrFromString(it.Label)
		_, _, _ = procAppendMenu.Call(m, 0 /* MF_STRING */, uintptr(i+1), uintptr(unsafe.Pointer(s)))
	}
	var pt struct{ X, Y int32 }
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetFore.Call(t.e.hwnd) // or the menu doesn't close when clicking elsewhere
	cmd, _, _ := procTrackPopup.Call(m, 0x0100|0x0020 /* TPM_RETURNCMD | TPM_BOTTOMALIGN */, uintptr(pt.X), uintptr(pt.Y), 0, t.e.hwnd, 0)
	_, _, _ = procPostMessage.Call(t.e.hwnd, wmNull, 0, 0)
	if cmd > 0 && int(cmd) <= len(t.items) {
		if f := t.items[cmd-1].Click; f != nil {
			go f(t.e.main)
		}
	}
}
