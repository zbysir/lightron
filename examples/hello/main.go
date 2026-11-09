// hello: a Lightron app whose pages are served by a Go http.Handler, calling Go from JS and back.
//
//	go run ./examples/hello
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zbysir/lightron"
)

//go:embed web
var web embed.FS

//go:embed icon.png
var icon []byte

// version is set by lightron build (-X main.version).
var version = "0.0.0-dev"

func main() {
	pages, _ := fs.Sub(web, "web")
	var app *lightron.App

	menu := lightron.DefaultMenu("Hello")
	// A menu of our own, between the app menu and Edit
	file := lightron.MenuItem{Label: "File", Submenu: []lightron.MenuItem{
		{Label: "New Window", Accelerator: "Cmd+N", Click: func(*lightron.Window) { _ = app.NewWindow("/") }},
		{Label: "Say Hi", Accelerator: "Cmd+Shift+H", Click: func(w *lightron.Window) {
			if w != nil {
				_ = w.Emit("hi", "from the menu, "+time.Now().Format("15:04:05"))
			}
		}},
	}}
	menu = append(menu[:1], append([]lightron.MenuItem{file}, menu[1:]...)...)

	err := lightron.Run(lightron.Options{
		Name:    "Hello",
		ID:      "dev.lightron.hello",
		Version: version,
		Icon:    icon,
		Handler: http.FileServer(http.FS(pages)),
		Window:  lightron.WindowOptions{Width: 900, Height: 640},
		Menu:    menu,
		DockMenu: []lightron.MenuItem{
			{Label: "New Window", Click: func(*lightron.Window) { _ = app.NewWindow("/") }},
		},
		Bind: map[string]any{
			"add": func(a, b float64) float64 { return a + b },
			"hostname": func() (string, error) {
				return os.Hostname()
			},
			"stat": func(path string) (string, error) {
				st, err := os.Stat(path)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%s: %d bytes", filepath.Base(path), st.Size()), nil
			},
		},
		OnReady: func(a *lightron.App) {
			app = a
			go func() {
				for t := range time.Tick(time.Second) {
					a.Emit("tick", t.Format("15:04:05"))
				}
			}()
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
