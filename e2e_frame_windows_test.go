package lightron

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestE2EFrameFiles: an app's page in an iframe of another site (a panel docked in a website) gets the paths of
// files dropped on it: a file and a folder. LIGHTRON_E2E=1 go test -run E2EFrame -v .
func TestE2EFrameFiles(t *testing.T) {
	if os.Getenv("LIGHTRON_E2E") == "" {
		t.Skip("set LIGHTRON_E2E=1 (opens a window)")
	}
	dir, err := os.MkdirTemp("", "lightron-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 20 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(250 * time.Millisecond)
		}
	})
	file := filepath.Join(dir, "a b.zip")
	_ = os.WriteFile(file, []byte("zip"), 0o600)
	folder := filepath.Join(dir, "folder")
	_ = os.Mkdir(folder, 0o700)

	// the website: not the app's; it embeds the app's page (?frame=<its address>)
	site, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer site.Close()
	go func() {
		_ = http.Serve(site, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<!doctype html><title>site</title><style>body{margin:0}iframe{border:0;width:400px;height:300px}</style>
				<iframe src="`+r.URL.Query().Get("frame")+`"></iframe>
				<script>addEventListener("message", (e) => { window.got = e.data })</script>`)
		}))
	}()
	siteURL := "http://" + site.Addr().String() + "/"
	panel := `<!doctype html><title>panel</title><style>body{margin:0}#d{width:400px;height:300px}</style><div id=d></div><script>
		d.addEventListener("dragover", (e) => e.preventDefault());
		d.addEventListener("drop", (e) => {
			e.preventDefault();
			parent.postMessage({ bridge: typeof window.lightron, paths: Array.from(e.dataTransfer.items).map((it) => lightron.getPathForFile(it.getAsFile())) }, "*");
		});
	</script>`

	results := map[string]string{}
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		runErr = Run(Options{
			Name: "Lightron E2E", ID: "dev.lightron.e2e", DataDir: dir,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, panel) }),
			Logf:    t.Logf,
			OnReady: func(a *App) {
				defer a.Quit()
				w := a.Windows()[0]
				eval := func(js string) string {
					for i := 0; i < 50; i++ {
						r, err := w.Call("Runtime.evaluate", map[string]any{"expression": js, "returnByValue": true})
						if err == nil {
							var v struct {
								Result struct{ Value any } `json:"result"`
							}
							_ = json.Unmarshal(r, &v)
							b, _ := json.Marshal(v.Result.Value)
							return string(b)
						}
						time.Sleep(200 * time.Millisecond)
					}
					return "error"
				}
				for i := 0; i < 150 && eval(`document.title`) != `"panel"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				var app string
				_ = json.Unmarshal([]byte(eval(`location.origin`)), &app)
				t.Logf("app origin: %s", app)
				_, _ = w.Call("Page.navigate", map[string]any{"url": siteURL + "?frame=" + app + "/"})
				for i := 0; i < 150 && eval(`document.title`) != `"site"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				time.Sleep(2 * time.Second) // the iframe loads, is attached
				data := map[string]any{"items": []any{}, "files": []string{file, folder}, "dragOperationsMask": 1}
				for _, typ := range []string{"dragEnter", "dragOver", "drop"} {
					if _, err := w.Call("Input.dispatchDragEvent", map[string]any{"type": typ, "x": 100, "y": 100, "data": data}); err != nil {
						results["drag "+typ] = err.Error()
					}
				}
				for i := 0; i < 80 && eval(`window.got ?? null`) == "null"; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["got"] = eval(`window.got ?? null`)
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
	want, _ := json.Marshal(map[string]any{"bridge": "object", "paths": []string{file, folder}})
	if results["got"] != string(want) {
		t.Errorf("got = %s, want %s", results["got"], want)
	}
	t.Logf("results: %v", results)
}
