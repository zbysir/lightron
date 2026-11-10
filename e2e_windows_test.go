package lightron

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestE2E runs a real app on this machine's WebView2 and checks it from the Go side over the DevTools protocol
// (Window.Call): the bridge, the User-Agent, PageScript on another site (without the bridge, its calls dropped), and
// KeepRunning (closing hides). It opens a window for a few seconds. LIGHTRON_E2E=1 go test -run E2E -v .
func TestE2E(t *testing.T) {
	if os.Getenv("LIGHTRON_E2E") == "" {
		t.Skip("set LIGHTRON_E2E=1 (opens a window)")
	}
	// Not t.TempDir: WebView2's processes outlive Run by a moment and hold its lockfile, which fails the test's cleanup
	dir, err := os.MkdirTemp("", "lightron-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 20 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(250 * time.Millisecond)
		}
	})
	page := `<!doctype html><title>e2e</title><script>
		window.bridgeAtLoad = typeof window.lightron === "object";
		window.scriptAtLoad = window.fromScript;
	</script>`
	// another site: a second server is another origin (another port)
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	go func() {
		_ = http.Serve(other, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<!doctype html><title>other</title>`)
		}))
	}()
	otherURL := "http://" + other.Addr().String() + "/"

	results := map[string]string{}
	marked := false
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		runErr = Run(Options{
			Name: "Lightron E2E", ID: "dev.lightron.e2e", DataDir: dir,
			Script:          `window.fromScript = typeof window.lightron.invoke === "function"`,
			PageScript:      `window.fromPageScript = location.origin`,
			UserAgentSuffix: " LightronE2E/1",
			KeepRunning:     true,
			Handler:         http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, page) }),
			Bind: map[string]any{
				"add":  func(a, b int) int { return a + b },
				"mark": func() { marked = true },
			},
			Logf: t.Logf,
			OnReady: func(a *App) {
				defer a.Quit()
				w := a.Windows()[0]
				e := a.eng.(*win)
				eval := func(js string) string {
					for i := 0; i < 50; i++ {
						r, err := w.Call("Runtime.evaluate", map[string]any{"expression": js, "awaitPromise": true, "returnByValue": true})
						if err == nil {
							var v struct {
								Result struct{ Value any } `json:"result"`
							}
							_ = json.Unmarshal(r, &v)
							b, _ := json.Marshal(v.Result.Value)
							return string(b)
						}
						results["eval error"] = err.Error()
						time.Sleep(200 * time.Millisecond)
					}
					return "error"
				}
				for i := 0; i < 150 && eval(`document.title`) != `"e2e"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["bridge at load"] = eval(`window.bridgeAtLoad`)
				results["script at load"] = eval(`window.scriptAtLoad`)
				results["host"] = eval(`location.host`)
				if results["host"] != `"e2e"` {
					t.Logf("page: %s %s", eval(`location.href`), eval(`document.body && document.body.innerText`))
					t.Logf("args env: %q", os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"))
					if r, err := a.Call("Browser.getVersion", nil); err == nil {
						t.Logf("version: %s", r)
					}
					cl, _ := exec.Command("powershell", "-NoProfile", "-Command", `Get-CimInstance Win32_Process -Filter "Name='msedgewebview2.exe'" | Where-Object { $_.CommandLine -notmatch '--type=' } | ForEach-Object { $_.CommandLine }`).CombinedOutput()
					t.Logf("browser process: %s", cl)
				}
				results["page script"] = fmt.Sprint(eval(`window.fromPageScript`) != `null`)
				results["add"] = eval(`lightron.invoke("add", 2, 3)`)
				results["user agent"] = fmt.Sprint(strings.HasSuffix(eval(`navigator.userAgent`), ` LightronE2E/1"`))
				if r, err := w.Call("Network.getCookies", map[string]any{"urls": []string{"http://e2e/"}}); err == nil && strings.Contains(string(r), "cookies") {
					results["cookies"] = "ok"
				}

				// another site in the window: PageScript runs, no bridge, and a call past the bridge is dropped
				results["permissions"] = eval(`Promise.all(["notifications", "clipboard-read", "clipboard-write"].map((n) => navigator.permissions.query({ name: n }).then((p) => p.state))).then((a) => a.join(","))`)
				results["notification permission"] = eval(`Notification.permission`)
				_, _ = w.Call("Page.navigate", map[string]any{"url": otherURL})
				for i := 0; i < 150 && eval(`document.title`) != `"other"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["other: page script"] = eval(`window.fromPageScript`)
				results["other: bridge"] = eval(`typeof window.lightron`)
				results["other: notification permission"] = eval(`Notification.permission`)
				eval(`(window.__lightronPost && window.__lightronPost(JSON.stringify({t: "invoke", id: 1, name: "mark", args: []})), 1)`)
				time.Sleep(500 * time.Millisecond)
				results["other: marked"] = fmt.Sprint(marked)

				// KeepRunning: closing hides the window, the app keeps running; FocusOrOpen brings it back
				_, _, _ = procPostMessage.Call(e.hwnd, wmClose, 0, 0)
				time.Sleep(500 * time.Millisecond)
				results["closed: visible"] = fmt.Sprint(isVisible(e.hwnd))
				a.FocusOrOpen()
				time.Sleep(500 * time.Millisecond)
				results["reopened: visible"] = fmt.Sprint(isVisible(e.hwnd))
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		t.Fatalf("Run didn't return; so far: %v", results)
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	want := map[string]string{
		"bridge at load":                 "true",
		"script at load":                 "true",
		"page script":                    "true",
		"add":                            "5",
		"user agent":                     "true",
		"cookies":                        "ok",
		"host":                           `"e2e"`,
		"permissions":                    `"granted,granted,granted"`,
		"notification permission":        `"granted"`,
		"other: notification permission": `"default"`,
		"other: page script":             fmt.Sprintf("%q", strings.TrimSuffix(otherURL, "/")),
		"other: bridge":                  `"undefined"`,
		"other: marked":                  "false",
		"closed: visible":                "false",
		"reopened: visible":              "true",
	}
	for k, v := range want {
		if results[k] != v {
			t.Errorf("%s = %s, want %s", k, results[k], v)
		}
	}
	t.Logf("results: %v", results)
}

func isVisible(hwnd uintptr) bool {
	r, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd)
	return r != 0
}
