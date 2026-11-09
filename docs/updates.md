# Updates

The `update` package keeps an app up to date from a **feed**: a JSON file with the latest version and a package per
platform. `lightron feed` writes it from what `lightron build` made:

```json
{
  "version": "1.2.0",
  "notes": "What's new",
  "packages": {
    "darwin-arm64":  { "file": "Notes-1.2.0-darwin-arm64.dmg",        "size": 3149840, "sha256": "…" },
    "darwin-amd64":  { "file": "Notes-1.2.0-darwin-amd64.dmg",        "size": 3345218, "sha256": "…" },
    "windows-amd64": { "file": "Notes-1.2.0-windows-amd64-setup.exe", "size": 2458155, "sha256": "…" }
  }
}
```

The packages are looked up next to the feed, so any static host works. A GitHub release is one: upload
`update.json` with the packages, and the feed URL is
`https://github.com/<owner>/<repo>/releases/latest/download/update.json`.

## In the app

```go
import "github.com/zbysir/lightron/update"

u := update.New(update.Options{
	FeedURL: "https://github.com/me/notes/releases/latest/download/update.json",
	Current: version, // -X main.version, set by lightron build
})
rel, err := u.Check(ctx) // nil, nil: up to date
if err == nil && rel != nil && askTheUser(rel.Version, rel.Notes) {
	if err := rel.Install(); err == nil {
		app.Quit()
	}
}
```

- `Check` downloads the newer package and checks its size and SHA-256. Server errors (5xx, 429) are tried again a
  few times over half a minute; a 4xx is final. A package downloaded earlier and not installed yet isn't downloaded
  again.
- `Install` hands over to a helper that waits for the app to exit, then:
  - **macOS**: copies the new `.app` next to the running one, swaps the two (two renames), and opens the new one. If
    a rename fails, the old one is put back. If the app can't be replaced where it is (run from a disk image, App
    Translocation, a folder it can't write to), the package is opened for the person to drag over, and `Install`
    returns an error saying so.
  - **Windows**: runs the installer silently (`/S`), which starts the app when done.
- Builds whose version isn't `x.y.z` (or a `git describe` of one) return `update.ErrNotUpdatable`: development
  builds don't update themselves.

How often to check, whether to ask, and when to install are up to the app: Lightron has no UI for it. A common
pattern: check at launch and every few hours, show "Restart to update" in a menu, install when chosen or at quit.

## Security

The sha256 in the feed catches a truncated or corrupted download, not a forged one: whoever can change the packages
can change the feed. Serve both over HTTPS from a host you control (GitHub releases are fine). On macOS a forged
package also fails Gatekeeper if your releases are Developer ID signed and notarized; on Windows, sign the installer.
