# Packaging

`lightron build` turns your Go program into what people install. It reads `lightron.json` next to your `main`
package (`lightron init` writes one):

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2.0",
  "icon": "icon.png",
  "publisher": "Example Inc.",
  "targets": ["darwin/arm64", "darwin/amd64", "windows/amd64"]
}
```

| Field | |
|---|---|
| `name` | The app's name: the `.app`, the installer, shortcuts, file names. Required. |
| `id` | Reverse-DNS id, the macOS bundle id. Use the same as `Options.ID`. Required. |
| `version` | `x.y.z`. `lightron build --version v1.2.0` overrides it (a release tag; the `v` is dropped). |
| `icon` | Square PNG, 512 px or larger (1024 px for sharp Retina icons). Default: Lightron's. |
| `main` | The Go package to build, relative to `lightron.json`. Default `.`. |
| `out` | Output directory. Default `dist`. |
| `publisher` | Shown in Windows' Installed apps and the exe's properties. |
| `targets` | What `lightron build` builds without `--target`. |
| `exe` | The executable's name, without `.exe`. Default: the name, lowercased, spaces as dashes. |
| `files` | More files to ship, destination → source (`{os}` / `{arch}` in a source): `.app/Contents/Resources` on macOS, next to the `.exe` on Windows. |
| `nsis_include` | An NSIS script for the Windows installer; its macro `preInstall` runs before the files are copied. |
| `protocols` | URL schemes the Windows installer registers (and the uninstaller removes); see `Options.Protocols`. |

The version is also passed to your program as `-X main.version=<version>`: declare `var version = "0.0.0-dev"` in
`package main` and pass it to `Options.Version` and the `update` package.

## What it makes

Each Go build is `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w …"`, so every target builds from any OS.

**macOS**: `<Name>.app`, as `<Name>-<version>-darwin-<arch>.dmg` (built on a Mac only: `hdiutil`) and `.zip`.
The bundle holds your program, its `Info.plist` and the icon. It is `LSUIElement`: the program itself stays out of
the Dock, because what shows there is the engine (the browser copy carrying your name and icon). One package per
architecture: a universal one would double the size for nothing.

**Windows**: `<Name>.exe` with its icon, a per-monitor-DPI manifest and version info (a `.syso` resource file
next to your main package while it builds), as `<Name>-<version>-windows-<arch>.zip` and, with `makensis` installed
(`brew install makensis`, `apt install nsis`), `…-setup.exe`: a per-user installer (no admin prompt) into
`%LOCALAPPDATA%\Programs\<Name>`, with Start menu and desktop shortcuts and an uninstaller, which starts the app when
done. Run with `/S` it is silent: that is how the `update` package installs.

Typical sizes for a small app: 3 MB per package.

## Signing

`lightron build` signs the macOS bundle ad hoc, which is enough on your own Macs. For other people:

- **macOS**: sign with a Developer ID and notarize, or Gatekeeper refuses a downloaded app:

  ```sh
  codesign --force --options runtime --timestamp --sign "Developer ID Application: …" dist/darwin-arm64/Notes.app
  # then make the .dmg again (or sign the .zip's contents before zipping) and:
  xcrun notarytool submit Notes-1.2.0-darwin-arm64.dmg --keychain-profile … --wait
  xcrun stapler staple Notes-1.2.0-darwin-arm64.dmg
  ```

  The hardened runtime applies to your program only. The browser copy is made and signed on the user's Mac, so it
  never needs your signature.

- **Windows**: an unsigned installer works, with a SmartScreen warning until it has a reputation. Sign the exe and
  the installer with `signtool` or [osslsigncode](https://github.com/mtrojnar/osslsigncode) (from macOS / Linux).

## In CI

```yaml
- uses: actions/setup-go@v5
  with: { go-version: stable }
- run: brew install makensis   # macos runners; ubuntu: sudo apt-get install -y nsis
- run: go install github.com/zbysir/lightron/cmd/lightron@latest
- run: lightron build --version "${GITHUB_REF_NAME}"
- run: lightron feed --version "${GITHUB_REF_NAME}"
- run: gh release upload "${GITHUB_REF_NAME}" dist/*.dmg dist/*.zip dist/*-setup.exe dist/update.json
```

On a macOS runner this builds everything; on Linux, everything but the `.dmg`.
