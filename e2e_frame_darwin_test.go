package lightron

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestE2EFrameFiles: an app's page in an iframe of another site (a panel docked in a website) runs in its own
// process; it has window.lightron there too, and a picked file's path. LIGHTRON_E2E=1 go test -run E2EFrame -v .
func TestE2EFrameFiles(t *testing.T) {
	if os.Getenv("LIGHTRON_E2E") == "" {
		t.Skip("set LIGHTRON_E2E=1 (opens a window on this Mac's Chrome)")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a b.zip")
	_ = os.WriteFile(file, []byte("zip"), 0o600)
	folder := filepath.Join(dir, "folder")
	_ = os.Mkdir(folder, 0o700)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	frameOrigin := "http://" + ln.Addr().String()
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<!doctype html><title>frame</title><style>body{margin:0}#d{width:300px;height:200px}</style><input type=file id=f><div id=d></div><script>
				d.addEventListener("dragover", (e) => e.preventDefault());
				d.addEventListener("drop", (e) => {
					e.preventDefault();
					window.dropped = Array.from(e.dataTransfer.items).map((it) => lightron.getPathForFile(it.getAsFile()));
				});
				window.bridge = typeof window.lightron;
				f.addEventListener("change", () => { window.got = lightron.getPathForFile(f.files[0]) });
			</script>`)
		}))
	}()
	defer ln.Close()
	results := map[string]string{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		err = Run(Options{
			Name: "Lightron E2E", ID: "dev.lightron.e2e", DataDir: filepath.Join(dir, "data"),
			// The top page is another site (not the app's); the iframe is the app's
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `<!doctype html><title>top</title><iframe src="`+frameOrigin+`/"></iframe>`)
			}),
			Origins: []string{frameOrigin},
			Logf:    t.Logf,
			OnReady: func(a *App) {
				defer a.Quit()
				m := a.eng.(*mac)
				frame := ""
				for i := 0; i < 300 && frame == ""; i++ {
					m.ctxMu.Lock()
					for f := range m.frames {
						frame = f
					}
					m.ctxMu.Unlock()
					time.Sleep(100 * time.Millisecond)
				}
				if frame == "" {
					results["frame"] = "none"
					return
				}
				eval := func(js string) string {
					v, err := m.c.Eval(frame, js)
					if err != nil {
						return "error: " + err.Error()
					}
					return string(v)
				}
				for i := 0; i < 100 && eval(`document.readyState`) != `"complete"`; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["bridge"] = eval(`window.bridge`)
				doc, _ := m.c.Call(frame, "DOM.getDocument", nil)
				var d struct{ Root struct{ NodeID int64 } }
				_ = json.Unmarshal(doc, &d)
				q, _ := m.c.Call(frame, "DOM.querySelector", map[string]any{"nodeId": d.Root.NodeID, "selector": "#f"})
				var n struct{ NodeID int64 }
				_ = json.Unmarshal(q, &n)
				if _, err := m.c.Call(frame, "DOM.setFileInputFiles", map[string]any{"files": []string{file}, "nodeId": n.NodeID}); err != nil {
					results["set files"] = err.Error()
				}
				for i := 0; i < 30 && eval(`window.got ?? null`) == "null"; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["path"] = eval(`window.got ?? null`)

				// A real drop on the iframe (a zip and a folder), the way a chat view reads it: items' getAsFile
				top := a.Windows()[0].session
				box := eval(`JSON.stringify(d.getBoundingClientRect())`)
				var r struct{ X, Y float64 }
				var s string
				_ = json.Unmarshal([]byte(box), &s)
				_ = json.Unmarshal([]byte(s), &r)
				fr, _ := m.c.Eval(top, `JSON.stringify(document.querySelector("iframe").getBoundingClientRect())`)
				var f struct{ X, Y float64 }
				_ = json.Unmarshal(fr, &s)
				_ = json.Unmarshal([]byte(s), &f)
				x, y := f.X+r.X+50, f.Y+r.Y+50
				data := map[string]any{"items": []any{}, "files": []string{file, folder}, "dragOperationsMask": 1}
				for _, typ := range []string{"dragEnter", "dragOver", "drop"} {
					if _, err := m.c.Call(top, "Input.dispatchDragEvent", map[string]any{"type": typ, "x": x, "y": y, "data": data}); err != nil {
						results["drag "+typ] = err.Error()
					}
				}
				for i := 0; i < 30 && eval(`window.dropped ?? null`) == "null"; i++ {
					time.Sleep(100 * time.Millisecond)
				}
				results["dropped"] = eval(`JSON.stringify(window.dropped ?? null)`)
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("Run didn't return")
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, _ := json.Marshal(file)
	dropped, _ := json.Marshal([]string{file, folder})
	dropped, _ = json.Marshal(string(dropped))
	want := map[string]string{"bridge": `"object"`, "path": string(p), "dropped": string(dropped)}
	for k, v := range want {
		if results[k] != v {
			t.Errorf("%s = %s, want %s", k, results[k], v)
		}
	}
	t.Logf("results: %v", results)
}
