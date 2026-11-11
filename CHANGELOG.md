# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/) (before 1.0, minor versions may break the API).

## [0.4.7] - 2026-11-11

### Fixed

- macOS: Quit (the Dock menu's, a quit Apple event, the app menu's Quit) left a `KeepRunning` app in the Dock with no
  window, and clicking the Dock icon brought none back: Chrome answered Quit by closing its windows and starting its own
  shutdown, while the engine process stayed up for the debugging pipe, so the shell saw only "no window left". Quit now
  goes to the shell, which closes the browser and returns from `Run`, `KeepRunning` or not. Closing the last window
  still keeps a `KeepRunning` app running.

## [0.4.6] - 2026-11-11

### Fixed

- macOS: a link out clicked in an app's page in another site's iframe (0.4.5 gave those the bridge) did nothing: the
  bridge took the click over and the shell dropped the message. It opens in the default browser again.

## [0.4.5] - 2026-11-11

### Fixed

- macOS: an app's page in an iframe of another site (a panel the app docks in a website, with `PageScript`) runs in
  its own process, which the bridge never reached: it has `window.lightron` now, and `getPathForFile` answers there
  (a file or folder dropped on the panel). Only files' paths are answered in such an iframe; `invoke` and events stay
  the top page's.
- Windows: `getPathForFile` answers (it returned `""`): in the page, and in an app's page in an iframe, of the same
  site or another. Over the DevTools protocol (`DOM.getFileInfo`); iframes use a binding of the protocol's, WebView2
  delivering the top page's messages only. Needs WebView2 1.0.1245 or later for iframes of another site.
- A file's path that isn't answered within 5 s is `""`: a drop is never held for good.

## [0.4.4] - 2026-11-10

### Changed

- Windows: pages are at `http://<Host>/` as on macOS (`Options.Host`, by default the last part of `ID`), not at
  `127.0.0.1:<port>`: notifications show the app's name as their origin, and the page's storage no longer depends on
  the port. An existing app's pages move to a new origin once, so what they kept in localStorage starts empty;
  `Host: "-"` keeps the real address. Where WebView2 doesn't take the switches that make the host (it ignored
  `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` on GitHub's Windows runners), the pages stay at the real address.

## [0.4.3] - 2026-11-09

### Fixed

- Windows: the bridge no longer takes over links in iframes (WebView2 delivers messages from the top page only, so
  0.4.1's fix couldn't reach them): the page's own handling of its links runs there.

## [0.4.2] - 2026-11-09

### Changed

- Windows: notifications and the clipboard are allowed without asking for the app's own pages, as on macOS (WebView2
  asked on the first notification and on reading the clipboard). Other sites a window shows still ask.

## [0.4.1] - 2026-11-09

### Fixed

- Windows: links (`target=_blank`, `window.open`) in an iframe of the app's own inside another site did nothing: the
  bridge is there too (WebView2 injects into iframes), and its "open in the default browser" was dropped by the origin
  check. Opening an http(s) address is now allowed from any page.

## [0.4.0] - 2026-11-09

Windows catches up: what an app showing other sites in its window needs there too.

### Added

- Windows: `Window.Call` / `App.Call` over WebView2's DevTools protocol (`CallDevToolsProtocolMethod`; one window, so
  `App.Call` goes to its page).
- Windows: `UserAgentSuffix`.
- Windows: `KeepRunning`: closing the window hides it; an icon in the notification area brings it back or quits (its
  menu: Open, the `DockMenu`'s items, Quit). Opening the app again also brings it back.
- `lightron build`: `exe` (the executable's name), `files` (more files and directories in the package: the .app's
  Resources, next to the .exe; `{os}` / `{arch}` in sources), `nsis_include` (an NSIS script; its `preInstall` macro
  runs before the files are copied). `main` may be in another module than lightron.json.
- A Windows end-to-end test in CI (e2e_windows_test.go).

### Changed

- Windows: bridge calls are checked against the origin of the page the window shows, as on macOS.
- The Windows installer ends the running app before copying (its files are in use).

### Fixed

- Windows: `update`'s `Install` never ran the installer: PowerShell started with `DETACHED_PROCESS` exits without
  running its script.

### Notes

- WebView2 runs `Script` / `PageScript` in every frame, iframes included (macOS: the page's frames, not cross-origin
  iframes, which are separate targets). A script meant for the window's page should check `window.top === window`.

## [0.3.1] - 2026-11-09

### Fixed

- 0.3.0 didn't build on Linux (where `Run` returns `ErrUnsupported`): the page scripts used the bridge, which is only
  in the macOS and Windows builds.

## [0.3.0] - 2026-11-09

For apps whose windows also show other sites (Creght's desktop app: a web app on the left, the app's own panel on the
right).

### Added

- `Options.PageScript`: JavaScript for every page a window shows, of any site.
- `Options.Origins`: more origins whose pages are the app's (bridge, `Script`, `Bind`); `http://*.localhost:<port>`
  for any subdomain.
- `Options.BrowserArgs`: extra command-line switches for the engine / WebView2.
- `Options.UserAgentSuffix`: appended to the User-Agent (macOS for now).
- `Options.Host` `"-"`: pages stay at their real address (a backend that checks the Host header).

### Changed

- `window.lightron`, `Options.Script` and `Bind` are only on the app's own origins. Before, a window that navigated to
  another site gave it the bridge.

## [0.2.0] - 2026-11-09

What moving an existing app (Gerdr) onto Lightron needed.

### Added

- `Options.URLFunc`: the page's address from a function run once this is the only instance, e.g. after starting the
  app's backend (a second launch no longer starts one only to hand over).
- `Options.Script`: JavaScript for every page, after `window.lightron`, before the page's own scripts.
- `App.SetMenu`, `App.SetDockMenu`: change the menus while running.
- `App.Call`, `Window.Call`: DevTools protocol commands, for what Lightron has no API for (macOS).

### Fixed

- macOS: the browser crashed at start when the app had no Dock menu.
- macOS: the page's language when the system's is set only machine-wide (`AppleLanguages` missing for the user).

## [0.1.0] - 2026-11-09

The first release: Gerdr's desktop shell, made into a library.

### Added

- `lightron.Run`: an app on the user's Chrome / Edge / Brave / Chromium on macOS (a renamed copy, driven over a
  DevTools pipe: the app's own name, icon, menu bar, Dock menu and notifications) and on WebView2 on Windows.
- Pages from an `http.Handler` (private loopback server) or a URL; local http URLs shown at `http://<Host>/`.
- `window.lightron`: `invoke` bound Go functions, `send`, `on` events, `focus`, `getPathForFile` (macOS).
- Menus (`Menu`, `DockMenu`, roles, accelerators), `App.NewWindow`, `SetBadge`, `Emit`, single instance,
  `KeepRunning`.
- `update`: feeds, checked downloads with retries, install-on-quit for macOS (.dmg / .zip) and Windows (installer).
- `lightron` CLI: `init`, `build` (.app / .dmg / .zip, Windows .exe with resources, NSIS installer), `feed`,
  `doctor`.

[0.4.6]: https://github.com/zbysir/lightron/releases/tag/v0.4.6
[0.4.5]: https://github.com/zbysir/lightron/releases/tag/v0.4.5
[0.4.4]: https://github.com/zbysir/lightron/releases/tag/v0.4.4
[0.4.3]: https://github.com/zbysir/lightron/releases/tag/v0.4.3
[0.4.2]: https://github.com/zbysir/lightron/releases/tag/v0.4.2
[0.4.1]: https://github.com/zbysir/lightron/releases/tag/v0.4.1
[0.4.0]: https://github.com/zbysir/lightron/releases/tag/v0.4.0
[0.3.1]: https://github.com/zbysir/lightron/releases/tag/v0.3.1
[0.3.0]: https://github.com/zbysir/lightron/releases/tag/v0.3.0
[0.2.0]: https://github.com/zbysir/lightron/releases/tag/v0.2.0
[0.1.0]: https://github.com/zbysir/lightron/releases/tag/v0.1.0
