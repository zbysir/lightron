// wrap: an app around a page served by something else, e.g. the backend your app already has, or a dev server.
//
//	go run ./examples/wrap http://localhost:5173
//
// An http URL on this machine is shown at http://<Host>/ (here http://wrap/), so cookies, storage and notifications
// belong to the app, not to "localhost:5173". Remote and https URLs are shown as they are.
package main

import (
	"log"
	"os"

	"github.com/zbysir/lightron"
)

func main() {
	url := "https://go.dev/"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	err := lightron.Run(lightron.Options{
		Name:        "Wrap",
		ID:          "dev.lightron.wrap",
		URL:         url,
		KeepRunning: true, // closing the window leaves it in the Dock; clicking there opens another
	})
	if err != nil {
		log.Fatal(err)
	}
}
