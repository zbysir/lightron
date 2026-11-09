# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/) (before 1.0, minor versions may break the API).

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

[0.3.0]: https://github.com/zbysir/lightron/releases/tag/v0.3.0
[0.2.0]: https://github.com/zbysir/lightron/releases/tag/v0.2.0
[0.1.0]: https://github.com/zbysir/lightron/releases/tag/v0.1.0
