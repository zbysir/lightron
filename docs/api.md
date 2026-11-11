# API reference

Everything a Lightron app can use, in one place: the Go package (`lightron.Run` and what it hands you), the page's
`window.lightron`, how links and windows behave, `lightron.json`, the `lightron` command and the `update` package.
What differs between macOS and Windows is said where it applies, and summed up in [Platforms](#platforms).

- [Run and Options](#run-and-options)
- [App](#app)
- [Window](#window)
- [Menus](#menus)
- [Errors](#errors)
- [The page: window.lightron](#the-page-windowlightron)
- [Links, windows and other sites](#links-windows-and-other-sites)
- [Links of the app's schemes](#links-of-the-apps-schemes-1)
- [lightron.json](#lightronjson)
- [The lightron command](#the-lightron-command)
- [The update package](#the-update-package)
- [Platforms](#platforms)

## Run and Options

```go
func Run(opts lightron.Options) error
```

Shows the app and blocks until it quits. Call it from `main`, once. It returns `nil` when the app quits normally
(last window closed, `App.Quit`, the menu's Quit), and also right away in a second instance, after handing over to
the first one (see `OnSecondInstance`).

`Name`, `ID` and exactly one of `URL`, `URLFunc`, `Handler` are required.

### Identity

| Field | Type | |
|---|---|---|
| `Name` | `string` | The app's name: Dock / taskbar, ⌘Tab, menu bar, notifications. Required. |
| `ID` | `string` | Reverse-DNS id (`com.example.notes`). Names the data directory, and on macOS the engine's bundle id (`<ID>.engine`). Use the same as `lightron.json`'s `id`. Required. |
| `Version` | `string` | Your version. Lightron shows it nowhere; it is for your About box and the `update` package. `lightron build` passes it as `-X main.version`. |
| `Icon` | `[]byte` | Square PNG, 512 px or larger. The Dock icon on macOS, the window / taskbar icon on Windows. |

### What the window shows

| Field | Type | |
|---|---|---|
| `URL` | `string` | A page served by something else (a backend your app starts, a dev server). An `http` URL on this machine is shown at `http://<Host>/` (see `Host`); `https` and remote URLs are shown as they are. |
| `URLFunc` | `func() (string, error)` | Returns `URL` instead, called once Lightron knows this is the only instance: start your backend here, return its address. An error ends `Run` with it. |
| `Handler` | `http.Handler` | Lightron serves the pages itself, on a private loopback port only the app's windows can use (a secret cookie set on the first load). |
| `Host` | `string` | The host name pages are shown at when the page is `http` on this machine. The browser keeps storage (localStorage, IndexedDB) by origin and writes the origin under web notifications, so a fixed name keeps both when the port changes. Default: the last part of `ID`, lowercased. `"-"`: pages stay at their real address (a backend that checks the `Host` header). |
| `Window` | `WindowOptions` | The first window: `Path` (relative to the page's root, default `/`), `Width`, `Height` (default 1200 × 800). |

### The bridge and scripts

| Field | Type | |
|---|---|---|
| `Bind` | `map[string]any` | Go functions the page calls with `await lightron.invoke("name", ...args)`. Arguments are JSON-decoded into the parameters' types. Return nothing, a value, an `error`, or `(value, error)`. A first parameter of type `*lightron.Window` receives the calling window (it is not counted among the page's arguments). |
| `OnMessage` | `func(w *Window, msg json.RawMessage)` | Receives `lightron.send(value)`. |
| `Script` | `string` | JavaScript run in every page **of the app's own origins**, before the page's scripts and after `window.lightron` is set up: a compatibility layer (`window.myNative = …`), settings to seed. |
| `PageScript` | `string` | JavaScript run in every page a window shows, **of any site**, before its scripts. No bridge there (except on the app's own origins). On Windows it also runs in every iframe: check `window.top === window` for the page itself. For an app whose windows show other sites and add to them (a panel of the app's own in an iframe). |
| `Origins` | `[]string` | More origins that are the app's, besides the one it starts at: they get the bridge, `Script` and `Bind`, and links to them stay in the app (see [Links](#links-windows-and-other-sites)). `"http://127.0.0.1:7000"`, or `"http://*.localhost:7000"` for any subdomain. |

### The engine

| Field | Type | |
|---|---|---|
| `BrowserArgs` | `[]string` | Extra command-line switches for the browser (macOS) / WebView2 (Windows), e.g. `--disable-features=…`. They apply to the app's own profile only. |
| `UserAgentSuffix` | `string` | Appended to the User-Agent in every window (`" MyApp/1.2"`), so sites can tell they are in the app. |
| `DataDir` | `string` | Lightron's state: the browser profile, and on macOS the engine copy. Default `~/Library/Application Support/<ID>/lightron`, `%LOCALAPPDATA%\<ID>\lightron`. One instance runs per data directory. |
| `Logf` | `func(format string, args ...any)` | Lightron's log lines (prefixed `lightron: `). Default `log.Printf`. |

### Links of the app's schemes

| Field | Type | |
|---|---|---|
| `Protocols` | `[]string` | URL schemes the app opens (`"notes"` for `notes://…`). A link of one, clicked in a browser or another app, starts the app if it isn't running and goes to `OnURL`. See [Links of the app's schemes](#links-of-the-apps-schemes-1) for how. |
| `OnURL` | `func(a *App, url string)` | Each link the app is opened with: at start (possibly before the first window has loaded: wait for `a.Windows()` before navigating one), or while running (instead of `OnSecondInstance`). Any web page can send such a link: check it before acting on it. |

### Life cycle and menus

| Field | Type | |
|---|---|---|
| `OnReady` | `func(a *App)` | Runs once, on its own goroutine, when the first window has been asked for. Where you get the `*App`; it may block (a loop that watches something). |
| `OnSecondInstance` | `func(a *App)` | The app was opened again while running (only one instance runs). Default: `a.FocusOrOpen()`. |
| `KeepRunning` | `bool` | Keep running with no window. macOS: the Dock icon reopens one; Windows: an icon in the notification area (click: the window; its menu: Open, `DockMenu`'s items, Quit). Default: quit when the last window closes. |
| `Menu` | `[]MenuItem` | The macOS menu bar (see [Menus](#menus)). nil: an app menu (Hide, Quit…), Edit, Window. |
| `DockMenu` | `[]MenuItem` | The Dock icon's menu on macOS, above the window list and Quit. On Windows with `KeepRunning`, its plain items (a `Click`, no submenu) go in the notification-area icon's menu. |

## App

`OnReady` hands you the running `*App`. Its methods are safe from any goroutine.

| Method | |
|---|---|
| `NewWindow(path string) error` | Opens a window on `path` (relative to the page's root; `""` = root), or on a full `http(s)` URL (e.g. one of `Origins`). macOS only: on Windows it returns an error while the window is open. |
| `Windows() []*Window` | The open windows. |
| `FocusOrOpen()` | Brings a window up, or opens one at `Window.Path` if there is none. |
| `Emit(event string, data any)` | `Window.Emit` to every window. |
| `SetBadge(label string)` | The Dock icon's badge (`""` clears it). macOS only. |
| `SetMenu(menu []MenuItem)` | Replaces `Options.Menu` (another language, an update's label, changed keys). nil: the default menu. |
| `SetDockMenu(menu []MenuItem)` | Replaces `Options.DockMenu`. |
| `Call(method string, params any) (json.RawMessage, error)` | Sends a DevTools protocol command to the browser (not a page) and returns its result: for what Lightron has no API for (cookies: `Storage.getCookies`). On Windows (one window, no browser-level session) it goes to the window's page. Don't detach from targets Lightron attached to or disable domains it enabled (Runtime, Page, Inspector). |
| `Quit()` | Closes the windows and makes `Run` return. |

## Window

| Method | |
|---|---|
| `ID() string` | Stable while the window is open. |
| `Eval(js string) error` | Runs JavaScript in the window's page (no result: use `Call("Runtime.evaluate", …)` for one). |
| `Emit(event string, data any) error` | Calls the page's `lightron.on(event, …)` listeners with `data` (any JSON value). |
| `Focus()` | Brings the window and the app to the front (restores it if minimized). |
| `Call(method string, params any) (json.RawMessage, error)` | A DevTools protocol command to this window's page (`Runtime.evaluate`, `Page.navigate`, `Emulation.setEmulatedMedia`, `Network.getCookies`…). On Windows through WebView2's `CallDevToolsProtocolMethod`. |
| `Close()` | Closes the window. On Windows with `KeepRunning` it hides; without, the app quits (its only window). |

## Menus

```go
type MenuItem struct {
	Label       string
	Accelerator string           // "Cmd+Shift+K", "Cmd+,", "Ctrl+Enter", "F5"
	Role        string           // a standard item; Label and Click may be empty
	Click       func(w *Window)  // the focused window, or nil
	Submenu     []MenuItem
	Separator   bool             // or use lightron.Separator
}
```

`Menu`'s first item's `Submenu` is the app menu; the others are menus in order. An accelerator triggers its item when
the page doesn't take the key itself.

Roles: `hide`, `hideOthers`, `unhide`, `quit`, `undo`, `redo`, `cut`, `copy`, `paste`, `selectAll`, `minimize`,
`zoom`, `front`, `close`, `toggleFullScreen`, `zoomIn`, `zoomOut`, `resetZoom`, `devTools`, `reload`; and, at the top
level only, `editMenu` and `windowMenu` (the browser's own Edit and Window menus: copy / paste, the window list).

Menus are macOS only. On Windows the window has no menu bar; the page's own keyboard shortcuts work as usual.

## Errors

| | |
|---|---|
| `lightron.ErrNoBrowser` | macOS: no Chrome, Edge, Brave or Chromium (Run has told the person and offered Chrome's download page). Windows: no WebView2 runtime (Run opened its download page). |
| `lightron.ErrUnsupported` | A platform Lightron doesn't run on (Linux), or what a platform doesn't have. |

## The page: window.lightron

On the app's own origins (where it starts, plus `Origins`), before the page's scripts:

```js
await lightron.invoke("name", ...args) // a function in Options.Bind; rejects with its error's message
lightron.send(value)                   // to Options.OnMessage
const off = lightron.on("event", (data) => {}) // from Window.Emit / App.Emit; off() stops listening
lightron.focus()                       // bring this window and the app forward (e.g. a notification's onclick)
lightron.getPathForFile(file)          // a picked / dropped / pasted File's path on disk, synchronously ("" if none)
lightron.platform                      // "darwin" | "windows"
```

- `getPathForFile`: paths are looked up while the drop / paste / change event is held, so the page reads them
  synchronously in its own handler. It also works in an app's page inside another site's iframe (the only bridge call
  answered there; `invoke`, `send` and events are the top page's).
- Notifications (`new Notification(…)`) and the clipboard are allowed without asking.
- Right-click: no browser menu where the page doesn't show its own, except on editable fields and selected text.
- Windows: the window's title follows `document.title`.

Other sites never get `window.lightron`, `Script` or `Bind`; calls from other origins are dropped by the shell.

## Links, windows and other sites

What happens to a link (`<a href>` click) or `window.open` in an app page:

| Target | Plain click / `window.open(u, "_self")` | New window (`target="_blank"`, ⌘/Ctrl/Shift-click, middle click, `window.open(u)`) |
|---|---|---|
| Same origin | navigates (the browser's own) | a new window of the app's (the browser's own popup) |
| Another of the app's origins (`Origins`) | navigates the window there | a new window of the app's (macOS); on Windows the window navigates there |
| Another site (`http(s)`, `mailto:`) | the default browser | the default browser |

A window can still show another site (navigate to it from Go with `Window.Call("Page.navigate", …)`, or a redirect):
an app that shows a web app, with `PageScript` adding to it. That site has no bridge.

Inside another site's iframe, links out open in the default browser on macOS; on Windows the page's own handling runs
(WebView2 delivers messages from the top page only).

## Links of the app's schemes

With `Options.Protocols` set, a link like `notes://open?id=3` ends up in `OnURL`, whether the app was running or not.

**macOS.** The app's `.app` doesn't claim the schemes; the engine copy does (its `Info.plist`), because the engine is
what runs, so it is what macOS sends links to. The copy is made the schemes' default handler each time it starts, so
an older install of the app (an Electron one, say) doesn't keep them.

- Running: the menu library in the engine takes the link (an Apple event) and passes it to the shell.
- Not running: macOS starts the engine for the link. Before the browser starts, the library takes the link, starts the
  app's program with the link as its argument, and exits; the app then starts as usual and gets the link.
- The engine copy is made when the app first runs, so a link clicked before that finds no handler. Copies in a
  temporary directory (`/tmp`) aren't used for links by macOS: keep `DataDir` out of one.
- The first time macOS starts the engine itself, it checks it (10–30 s, once per browser version).

**Windows.** The schemes are registered for the current user (`HKCU\Software\Classes\<scheme>`) at each start, and by
the installer when `lightron.json` lists them (`protocols`), so links work before the first start; the uninstaller
removes them. A link starts the app with the link as its argument; if it is running, the new instance hands the link
over and leaves.

## lightron.json

`lightron build` and `lightron feed` read it (`-config` to point elsewhere). Relative paths are relative to the file.

| Field | |
|---|---|
| `name` | The app's name: the `.app`, the installer, shortcuts, file names. Required. |
| `id` | Reverse-DNS id, the macOS bundle id and the Windows install / uninstall key. Required. |
| `version` | `x.y.z` (or `x.y.z-N`). `--version` overrides it. |
| `icon` | Square PNG, 512 px or larger (1024 px for sharp Retina icons). Default: Lightron's. |
| `main` | The Go package to build. Default `.`. |
| `out` | Output directory. Default `dist`. |
| `publisher` | Shown in Windows' Installed apps and the exe's properties. |
| `targets` | What `build` builds without `--target`. Default `darwin/arm64`, `darwin/amd64`, `windows/amd64`. |
| `exe` | The executable's name, without `.exe`. Default: the name, lowercased, spaces as dashes. |
| `files` | More files and directories to ship, destination → source: in the `.app`'s `Contents/Resources` on macOS, next to the `.exe` on Windows. `{os}` / `{arch}` in a source are the target's: `{"bin": "build/{os}-{arch}/bin"}`. Find them at run time from `os.Executable()`. |
| `nsis_include` | An NSIS script `!include`d by the Windows installer; its macro `preInstall`, if defined, runs before the files are copied (to remove what an earlier installer left). |
| `protocols` | The app's URL schemes (as in `Options.Protocols`): the Windows installer registers them and the uninstaller removes them. Nothing on macOS (see [Links of the app's schemes](#links-of-the-apps-schemes-1)). |

See [packaging.md](packaging.md) for what each package contains and signing.

## The lightron command

```sh
go install github.com/zbysir/lightron/cmd/lightron@latest   # or: go run github.com/zbysir/lightron/cmd/lightron@<version>
```

| Command | |
|---|---|
| `lightron init [dir]` | A new app: `main.go`, `web/index.html`, `icon.png`, `lightron.json`. |
| `lightron build [-config f] [-target os/arch,…] [-version v] [-ldflags …]` | Builds into `out`: `<Name>-<version>-darwin-<arch>.dmg` (on a Mac) and `.zip` of `<Name>.app`; `<Name>-<version>-windows-<arch>-setup.exe` (needs `makensis`) and `.zip`. Also leaves the unpacked `darwin-<arch>/<Name>.app`. The version reaches the program as `-X main.version`. Builds with `CGO_ENABLED=0`, so every target builds from any OS. |
| `lightron feed [-config f] [-version v] [-notes text\|@file]` | Writes `<out>/update.json` for the `update` package from the built packages. |
| `lightron doctor` | What this machine has (browsers, WebView2, makensis). |
| `lightron version` | Lightron's version. |

Use the same Lightron version for the command as in your `go.mod`, so the package matches the library.

## The update package

`github.com/zbysir/lightron/update`: check a feed, download the newer package, install it once the app has quit.

```go
u := update.New(update.Options{FeedURL: "https://…/update.json", Current: version, Dir: dataDir})
rel, err := u.Check(ctx)          // nil, nil when up to date; downloads and verifies (size, sha256) otherwise
if rel != nil && rel.Install() == nil {
	app.Quit()                    // macOS swaps the .app and opens the new one; Windows runs the installer silently
}
```

| | |
|---|---|
| `update.Options` | `FeedURL`, `Current` (x.y.z or a git describe), `Dir` (downloads), `Platform` (default `GOOS-GOARCH`), `Client`, `Logf`. |
| `(*Updater).Check(ctx)` | Reads the feed; downloads this platform's package if newer (not again if already downloaded). |
| `(*Release).Install()` | Hands the package to a detached helper that waits for this process to exit. If the `.app` can't be swapped where it is (disk image, App Translocation, read-only), it opens the package for the person to drag over and returns an error. Works with any downloaded `.dmg` / `-setup.exe`, also one your own feed format pointed to: `(&update.Release{Version: v, File: path}).Install()`. |
| `update.Newer(a, b)`, `update.ParseVersion(v)` | Version comparison. A non-comparable `Current` (`0.1.0-dev`) makes `Check` return `update.ErrNotUpdatable`. |

Feed format and hosting: [updates.md](updates.md).

## Platforms

| | macOS | Windows |
|---|---|---|
| Engine | a copy of the user's Chrome / Edge / Brave / Chromium, as your app | WebView2 |
| Windows | many (`NewWindow`, popups, links to `Origins`) | one |
| Menu bar, badge | yes | no |
| `DockMenu` | the Dock icon's menu | the notification-area icon's menu (with `KeepRunning`) |
| `KeepRunning` | stays in the Dock | an icon in the notification area |
| `App.Call` | browser-level session | the window's page |
| Bridge in another site's iframe | files' paths, links out | files' paths |
| URL schemes (`Protocols`) | claimed by the engine copy; cold start through it | registry (per user), argument |
| Version | macOS 11+ | Windows 10 / 11 |
