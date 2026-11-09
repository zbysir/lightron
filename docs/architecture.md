# How Lightron works

Lightron is a Go library: your program calls `lightron.Run`, which finds the machine's Chromium, opens your pages
in it as an app, and connects them to your Go code. This document is for contributors and for anyone who wants to
know what happens on their users' machines. Most of it is about macOS, where the work is.

```
your program (Go)                                    the engine (the system's Chromium)
┌──────────────────────────┐   DevTools protocol     ┌────────────────────────────────┐
│ lightron.Run             │ ◀── pipe (fd 3 / 4) ──▶ │ browser process                │
│  ├ Handler on 127.0.0.1  │                         │  ├ menu library (macOS)  ◀─────┼── unix socket
│  ├ bindings (Bind)       │                         │  └ --app windows               │
│  ├ menus                 │ ◀──────── HTTP ──────── │      └ your page + bridge.js   │
│  └ single instance       │                         └────────────────────────────────┘
└──────────────────────────┘
```

## macOS

### The engine: a copy of the user's browser, renamed

What the Dock, ⌘Tab, the menu bar and notifications show for a process comes from its bundle's `Info.plist`, and
macOS lets a process change only its own presentation. Run as installed, Chrome is "Google Chrome": a second Chrome
icon in the Dock, Chrome's name in the menu bar. So Lightron runs a copy of the browser (`internal/macengine`):

- Found in /Applications or ~/Applications: Google Chrome, Microsoft Edge, Brave Browser, Chromium, in that order.
- Copied with an APFS clone (`cp -c`): about a second, and no disk space while it shares blocks with the original.
  Only the running framework version is kept (~700 MB once the original updates past it, so older copies are
  deleted when a new one is made).
- Its `Info.plist` gets the app's name, bundle id (`<ID>.engine`) and icon, nothing else. `CFBundleIconName` goes:
  macOS prefers the icon in `Assets.car` to the `.icns`.
- Re-signed ad hoc (editing the plist breaks the vendor's signature, and macOS kills it at launch), after
  `xattr -cr`. Ad hoc means no hardened runtime, so the menu library can load; and a different code identity, so the
  engine runs with `--use-mock-keychain` (the user's Chrome keychain items would prompt).
- At `<DataDir>/engine/<browser version>/<Name>.app`: the Dock shows the bundle's folder name.
- The first launch of a new copy is slow (macOS checks it: a "Verifying…" dialog, 10–30 s), once per browser
  version.

Things that don't work, tried: `LSUIElement` (Chromium then thinks it is a helper and can't find its framework:
"icudtl.dat not found"), symlinks to the browser's binary or framework (same), `lsappinfo setinfo` on another
process (ignored).

### Driving it

The engine starts with `--remote-debugging-pipe`: DevTools protocol messages as NUL-separated JSON on fds 3 and 4
(`internal/cdp`). A pipe, not a port: no other program can connect. Other flags:

- `--user-data-dir=<DataDir>/profile`: a profile of the app's own.
- `--no-startup-window`; windows are opened by running the engine again with `--app=<url>`, which the running
  browser takes over (its process singleton). `--app` windows have a bare title bar.
- `--test-type`, on both command lines: Chrome otherwise shows a warning bar about the flags below.
- `--host-resolver-rules=MAP <Host> 127.0.0.1:<port>` and `--unsafely-treat-insecure-origin-as-secure=http://<Host>`:
  pages are at `http://<Host>/` (Chrome writes the origin under every notification; "notes" reads better than
  "127.0.0.1:53817"), which is a secure context (clipboard, notifications) without a certificate.
- `--accept-lang` from the system's languages (a fresh profile would say en-US); translation off in Preferences.
- `--window-size`: the size of a window with no saved placement; after that the browser remembers.

`Target.setAutoAttach` with `waitForDebuggerOnStart` holds every new page until Lightron has set it up:
`Runtime.enable` (without it, a binding added while the page waits is gone after its first navigation),
`Runtime.addBinding("__lightronPost")`, `Page.addScriptToEvaluateOnNewDocument(bridge.js)`. If a page still committed
without the bridge, it is reloaded once.

Pages of other origins opened from the app (`window.open`, `target=_blank`) are closed and opened in the default
browser. A new-tab page (the browser's own ⌘N/⌘T) is replaced by a window of the app.

Notifications and the clipboard are granted with `Browser.setPermission`, one at a time. Not
`Browser.grantPermissions`: that denies every permission it doesn't list.

### Menus and the Dock: a library inside the browser

Chrome has no API for its menu bar, and it is another process. `internal/macmenu/objc/menu.m` is loaded into it with
`DYLD_INSERT_LIBRARIES` (possible because the copy is ad-hoc signed) and talks to the Go side over a unix socket:
the menu as JSON one way, `click N` lines the other.

- It edits Chrome's menu bar instead of replacing it: replacing `NSApp.mainMenu` makes Chrome crash (a `CHECK` on
  its own menu). Chrome's menus are hidden and the app's inserted; Edit and Window are Chrome's own (copy/paste and
  the window list keep working), standard items are copies of Chrome's.
- It answers `applicationDockMenu:` with the app's Dock menu, and takes Dock clicks
  (`applicationShouldHandleReopen:`): Chrome's own answer is a new tabbed window, which flashes on screen.
- Bringing the app forward is done from inside (`activate`), never with `open -a` on the engine: that is a reopen,
  which opens a window.
- When the socket closes (the Go program exited, even by SIGKILL), it quits the browser: no orphaned app in the
  Dock.

It is built universal by `make menulib` and checked in, so apps build with `CGO_ENABLED=0`.

### File paths

A dropped, pasted or picked `File`'s path is known only to the browser (`DOM.getFileInfo`), which answers
asynchronously, while code like an upload handler wants it right away. So `bridge.js` holds such events in the
capture phase, asks for each file's path, and dispatches the event again with the same `File` objects;
`lightron.getPathForFile` is then synchronous.

### Lifetime

The app quits when its last window closes (after a 5 s grace period at launch, while the first window opens), unless
`KeepRunning`. On quit, `Browser.close`; if the browser doesn't exit within 5 s, it is killed.

## Windows

WebView2 through [go-webview2](https://github.com/jchv/go-webview2) (pure Go, no cgo): one window of the app's own,
the bridge added with `AddScriptToExecuteOnDocumentCreated`, the binding as a WebView2 host binding. The profile is
`%LOCALAPPDATA%\<ID>\lightron\WebView2`. Details that matter:

- `Debug: true` in go-webview2: without it, its context menus are off (no copy/paste menu). DevTools stay
  reachable from the context menu, as in Electron apps.
- Per-monitor DPI awareness (also in the exe's manifest, which `lightron build` writes).
- The window's icon is set at runtime from `Options.Icon`; the exe's icon (Explorer, the taskbar when pinned) comes
  from its resources.
- The page's `<title>` becomes the window's title.
- No WebView2 runtime: a message box, and its download page.

## Both

- **The page server**: an `Options.Handler` is served on `127.0.0.1:<random port>`. Any local program can reach
  that port, so requests need a secret cookie. On macOS it is put into the browser with `Storage.setCookies` over the
  pipe; on Windows the first navigation is a login URL (in-process, never on a command line) that sets it.
- **Single instance**: a unix socket in the data directory (Windows 10+ has them too). A second start tells the first
  (`OnSecondInstance`, by default: bring a window up) and exits.
- **Bindings** (`bind.go`): reflection. Arguments are JSON-decoded into the function's parameter types; a first
  parameter of type `*Window` gets the calling window; variadic functions work; a panic becomes a rejected promise.

## Updates and packaging

`update/` and `cmd/lightron`: see [updates.md](updates.md) and [packaging.md](packaging.md).
