package lightron

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// The DevTools protocol on Windows: ICoreWebView2::CallDevToolsProtocolMethod, behind Window.Call / App.Call as on
// macOS. go-webview2 doesn't expose the ICoreWebView2 (nor this method), so it is taken from its webview by
// reflection (coreWebView) and called through its vtable (comCall). go-webview2 is pinned in go.mod; a change of its
// layout fails loudly (coreWebView returns 0, Call returns an error), not silently.

// ICoreWebView2 vtable slots (WebView2.h; the same order as go-webview2's iCoreWebView2Vtbl).
const (
	slotGetSource                  = 4
	slotCallDevToolsProtocolMethod = 36
)

// coreWebView: the ICoreWebView2 inside a go-webview2 webview (webview.browser.(*edge.Chromium).webview); 0 if its
// layout isn't what this expects.
func coreWebView(w webview2.WebView) (ptr unsafe.Pointer) {
	defer func() {
		if recover() != nil {
			ptr = nil
		}
	}()
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Pointer {
		return nil
	}
	b := v.Elem().FieldByName("browser")
	if !b.IsValid() {
		return nil
	}
	b = reflect.NewAt(b.Type(), unsafe.Pointer(b.UnsafeAddr())).Elem().Elem() // the interface's dynamic value: *edge.Chromium
	if b.Kind() != reflect.Pointer {
		return nil
	}
	f := b.Elem().FieldByName("webview")
	if !f.IsValid() || f.Kind() != reflect.Pointer {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(f.UnsafeAddr()))
}

// comCall calls method slot of a COM object.
func comCall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// source: the address of the page the webview shows (on the UI thread).
func source(wv unsafe.Pointer) string {
	var s *uint16
	if comCall(wv, slotGetSource, uintptr(unsafe.Pointer(&s))) != 0 || s == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(s))
	return windows.UTF16PtrToString(s)
}

// cdpHandler is an ICoreWebView2CallDevToolsProtocolMethodCompletedHandler: a COM object (vtable first) the
// webview calls back on the UI thread with the result.
type cdpHandler struct {
	vtbl *[4]uintptr
	id   uintptr
}

type cdpResult struct {
	json string
	hr   uintptr
}

var (
	cdpOnce    sync.Once
	cdpVtbl    [4]uintptr
	cdpMu      sync.Mutex
	cdpSeq     uintptr
	cdpPending = map[uintptr]chan cdpResult{}
	cdpKeep    = map[uintptr]*cdpHandler{} // the handler stays reachable while the webview holds it
)

func cdpInit() {
	cdpVtbl = [4]uintptr{
		windows.NewCallback(func(this *cdpHandler, _ uintptr, out *uintptr) uintptr { // QueryInterface
			if out != nil {
				*out = uintptr(unsafe.Pointer(this))
			}
			return 0
		}),
		windows.NewCallback(func(this *cdpHandler) uintptr { return 1 }), // AddRef
		windows.NewCallback(func(this *cdpHandler) uintptr { return 1 }), // Release
		windows.NewCallback(func(this *cdpHandler, hr uintptr, result *uint16) uintptr { // Invoke
			cdpMu.Lock()
			ch := cdpPending[this.id]
			delete(cdpPending, this.id)
			delete(cdpKeep, this.id)
			cdpMu.Unlock()
			if ch != nil {
				s := ""
				if result != nil {
					s = windows.UTF16PtrToString(result)
				}
				ch <- cdpResult{json: s, hr: hr & 0xffffffff}
			}
			return 0
		}),
	}
}

// cdpCall runs a DevTools protocol method on the webview's page and waits for its result. Not on the UI thread
// (the result comes through the UI thread's message loop).
func (e *win) cdpCall(method string, params any) (json.RawMessage, error) {
	wv := e.core
	if wv == nil {
		return nil, errors.New("lightron: the DevTools protocol isn't reachable in this WebView2 (go-webview2 changed?)")
	}
	if params == nil {
		params = map[string]any{}
	}
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	cdpOnce.Do(cdpInit)
	cdpMu.Lock()
	cdpSeq++
	h := &cdpHandler{vtbl: &cdpVtbl, id: cdpSeq}
	ch := make(chan cdpResult, 1)
	cdpPending[h.id], cdpKeep[h.id] = ch, h
	cdpMu.Unlock()
	m16, _ := windows.UTF16PtrFromString(method)
	p16, _ := windows.UTF16PtrFromString(string(p))
	e.wv.Dispatch(func() {
		if hr := comCall(wv, slotCallDevToolsProtocolMethod, uintptr(unsafe.Pointer(m16)), uintptr(unsafe.Pointer(p16)), uintptr(unsafe.Pointer(h))); hr != 0 {
			cdpMu.Lock()
			delete(cdpPending, h.id)
			delete(cdpKeep, h.id)
			cdpMu.Unlock()
			ch <- cdpResult{hr: hr & 0xffffffff}
		}
	})
	select {
	case r := <-ch:
		if r.hr != 0 {
			msg := r.json
			if msg == "" {
				msg = "HRESULT 0x" + itohex(r.hr)
			}
			return nil, errors.New("lightron: " + method + ": " + msg)
		}
		return json.RawMessage(r.json), nil
	case <-time.After(15 * time.Second):
		return nil, errors.New("lightron: " + method + ": no answer")
	}
}

func itohex(n uintptr) string {
	const digits = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n >>= 4 {
		b = append([]byte{digits[n&15]}, b...)
	}
	return string(b)
}
