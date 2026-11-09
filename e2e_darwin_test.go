package lightron

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2E runs a real app on the browser of this Mac and checks the bridge from inside its page. It opens a window
// for a few seconds. LIGHTRON_E2E=1 go test -run E2E -v .
func TestE2E(t *testing.T) {
	if os.Getenv("LIGHTRON_E2E") == "" {
		t.Skip("set LIGHTRON_E2E=1 (opens a window on this Mac's Chrome)")
	}
	dir := t.TempDir()
	page := `<!doctype html><title>e2e</title><script>
		window.events = [];
		lightron.on("ping", (d) => window.events.push(d));
		window.bridgeAtLoad = typeof window.lightron === "object";
		window.scriptAtLoad = window.fromScript;
	</script>`
	results := map[string]string{}
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runErr = Run(Options{
			Name: "Lightron E2E", ID: "dev.lightron.e2e", DataDir: dir,
			Script:  `window.fromScript = typeof window.lightron.invoke === "function"`,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, page) }),
			Bind: map[string]any{
				"add":  func(a, b int) int { return a + b },
				"fail": func() error { return errors.New("nope") },
				"me":   func(w *Window) string { return w.ID() },
			},
			Logf: t.Logf,
			OnReady: func(a *App) {
				defer a.Quit()
				m := a.eng.(*mac)
				var w *Window
				for i := 0; i < 300 && w == nil; i++ {
					if ws := a.Windows(); len(ws) > 0 {
						w = ws[0]
					}
					time.Sleep(100 * time.Millisecond)
				}
				if w == nil {
					results["window"] = "none"
					return
				}
				eval := func(js string) string {
					for i := 0; i < 50; i++ {
						v, err := m.c.Eval(w.session, js)
						if err == nil {
							return string(v)
						}
						time.Sleep(100 * time.Millisecond)
					}
					return "error"
				}
				// wait for the page
				for i := 0; i < 100 && eval(`document.title`) != `"e2e"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["bridge at load"] = eval(`window.bridgeAtLoad`)
				results["script at load"] = eval(`window.scriptAtLoad`)
				if r, err := w.Call("Runtime.evaluate", map[string]any{"expression": "6 * 7", "returnByValue": true}); err == nil {
					results["window call"] = string(r)
				}
				if r, err := a.Call("Browser.getVersion", nil); err == nil && strings.Contains(string(r), "product") {
					results["app call"] = "ok"
				}
				a.SetMenu(append(DefaultMenu("Lightron E2E"), MenuItem{Label: "Extra", Submenu: []MenuItem{{Label: "Hi", Click: func(*Window) {}}}}))
				time.Sleep(500 * time.Millisecond)
				bar, _ := exec.Command("osascript", "-e", `tell application "System Events" to get name of menu bar items of menu bar 1 of (first process whose bundle identifier is "dev.lightron.e2e.engine")`).Output()
				results["menu after SetMenu"] = fmt.Sprint(strings.Contains(string(bar), "Extra"))
				results["origin"] = eval(`location.origin`)
				results["secure"] = eval(`isSecureContext`)
				results["add"] = eval(`lightron.invoke("add", 2, 3)`)
				results["fail"] = eval(`lightron.invoke("fail").then(() => "resolved", (e) => e.message)`)
				results["me"] = eval(`lightron.invoke("me")`)
				_ = w.Emit("ping", map[string]int{"n": 1})
				time.Sleep(300 * time.Millisecond)
				results["events"] = eval(`JSON.stringify(window.events)`)
				results["permissions"] = eval(`Promise.all(["clipboard-write", "notifications"].map((n) => navigator.permissions.query({ name: n }).then((p) => p.state))).then((a) => a.join(","))`)
				_, _ = m.c.Call(w.session, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true})
				saved, _ := exec.Command("pbpaste").Output()
				results["copy"] = eval(`navigator.clipboard.writeText("lightron e2e").then(() => "ok", (e) => String(e))`)
				got, _ := exec.Command("pbpaste").Output()
				results["clipboard"] = string(got)
				restore := exec.Command("pbcopy")
				restore.Stdin = strings.NewReader(string(saved))
				_ = restore.Run()
				// The Handler's secret reaches the browser over the pipe only: no process's command line carries it.
				ps, _ := exec.Command("ps", "-axww", "-o", "args=").Output()
				results["secret on a command line"] = fmt.Sprint(strings.Contains(string(ps), loginPath))
				results["window id"] = w.ID()
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("Run didn't return")
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	want := map[string]string{
		"bridge at load":           "true",
		"script at load":           "true",
		"window call":              `{"result":{"type":"number","value":42,"description":"42"}}`,
		"app call":                 "ok",
		"menu after SetMenu":       "true",
		"origin":                   `"http://e2e"`,
		"secure":                   "true",
		"add":                      "5",
		"fail":                     `"nope"`,
		"events":                   `"[{\"n\":1}]"`,
		"permissions":              `"granted,granted"`,
		"copy":                     `"ok"`,
		"clipboard":                "lightron e2e",
		"secret on a command line": "false",
	}
	for k, v := range want {
		if results[k] != v {
			t.Errorf("%s = %s, want %s", k, results[k], v)
		}
	}
	if id, _ := json.Marshal(results["window id"]); results["me"] != string(id) {
		t.Errorf("me = %s, want the window's id %s", results["me"], id)
	}
	t.Logf("results: %v", results)
}
