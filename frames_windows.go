package lightron

import (
	"encoding/json"
	"errors"
)

// Files' paths on Windows. A dropped / pasted / picked File's path is the browser's to answer (DOM.getFileInfo), over
// the DevTools protocol as on macOS. The page asks through the binding; an app's page in an iframe of another site
// (a panel docked in a website) can't: WebView2 delivers the top page's messages only, and the iframe runs in its own
// process, which the page's session doesn't reach. So each such iframe is attached (Target.setAutoAttach) and given
// a binding of the protocol's, __lightronFramePost, which bridge.js uses for paths there (only paths: invoke's
// answers would go to the top page).

const frameBinding = "__lightronFramePost"

// watchFrames: not on the UI thread. Without ICoreWebView2_11 (WebView2 before 1.0.1245) iframes get no paths.
func (e *win) watchFrames() {
	done := make(chan error, 1)
	e.wv.Dispatch(func() {
		if e.core == nil {
			done <- errors.New("no ICoreWebView2")
			return
		}
		if e.core11 = queryInterface(e.core, &iidCoreWebView2_11); e.core11 == nil {
			done <- errors.New("no ICoreWebView2_11")
			return
		}
		if err := e.cdpOn("Target.attachedToTarget", e.frameAttached); err != nil {
			done <- err
			return
		}
		done <- e.cdpOn("Runtime.bindingCalled", e.frameCalled)
	})
	if err := <-done; err != nil {
		e.app.logf("iframes: %v", err)
		return
	}
	// The page's session too: an iframe of the same site runs in the page's process, not attached on its own
	e.addFrameBinding("")
	if err := e.autoAttach(""); err != nil {
		e.app.logf("iframes: %v", err)
	}
}

func (e *win) addFrameBinding(session string) bool {
	for _, step := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": frameBinding}},
	} {
		if _, err := e.cdpCallIn(session, step.method, step.params); err != nil {
			e.app.logf("iframes %s: %v", step.method, err)
			return false
		}
	}
	return true
}

func (e *win) autoAttach(session string) error {
	_, err := e.cdpCallIn(session, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true})
	return err
}

func (e *win) frameAttached(parent string, params json.RawMessage) {
	var p struct {
		SessionID  string `json:"sessionId"`
		TargetInfo struct {
			Type string `json:"type"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(params, &p) != nil || p.SessionID == "" {
		return
	}
	if p.TargetInfo.Type != "iframe" {
		_, _ = e.cdpCallIn(parent, "Target.detachFromTarget", map[string]any{"sessionId": p.SessionID})
		return
	}
	if e.addFrameBinding(p.SessionID) {
		_ = e.autoAttach(p.SessionID) // its own iframes of yet another site
	}
}

func (e *win) frameCalled(session string, params json.RawMessage) {
	var p struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
		Context int64  `json:"executionContextId"`
	}
	if json.Unmarshal(params, &p) != nil || p.Name != frameBinding {
		return
	}
	var m struct {
		T  string `json:"t"`
		ID int64  `json:"id"`
	}
	if json.Unmarshal([]byte(p.Payload), &m) != nil || m.T != "path" {
		return
	}
	// Only the app's own pages (Options.PageScript): the binding is in every iframe of another site
	r, err := e.cdpCallIn(session, "Runtime.evaluate", map[string]any{"expression": "location.origin", "contextId": p.Context, "returnByValue": true})
	if err != nil {
		return
	}
	var o struct {
		Result struct{ Value string } `json:"result"`
	}
	_ = json.Unmarshal(r, &o)
	if !appOrigin(o.Result.Value, e.origin, e.app.opts.Origins) {
		e.app.logf("bridge: a call from %q ignored (not the app's page)", o.Result.Value)
		return
	}
	e.answerPath(session, p.Context, m.ID)
}

// answerPath: the File the page parked in window.__lightronFiles under id → its path → lightron.__settle. session ""
// and context 0: the page.
func (e *win) answerPath(session string, context, id int64) {
	eval := func(js string) (json.RawMessage, error) {
		p := map[string]any{"expression": js}
		if context != 0 {
			p["contextId"] = context
		}
		return e.cdpCallIn(session, "Runtime.evaluate", p)
	}
	path := ""
	if r, err := eval("window.__lightronFiles.get(" + itoa(id) + ")"); err == nil {
		var v struct {
			Result struct {
				ObjectID string `json:"objectId"`
			} `json:"result"`
		}
		_ = json.Unmarshal(r, &v)
		if v.Result.ObjectID != "" {
			if r, err := e.cdpCallIn(session, "DOM.getFileInfo", map[string]any{"objectId": v.Result.ObjectID}); err == nil {
				var fi struct{ Path string }
				_ = json.Unmarshal(r, &fi)
				path = fi.Path
			} else {
				e.app.logf("file path: %v", err)
			}
		}
	}
	js, _ := json.Marshal(path)
	_, _ = eval("window.lightron.__settle(" + itoa(id) + "," + string(js) + ",null)")
}
