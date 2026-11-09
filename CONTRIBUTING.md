# Contributing

Thanks for helping. Bug reports with the output of `lightron doctor` and the app's log, and pull requests, are
welcome.

## Building and testing

```sh
make test                          # go vet for macOS, Windows and Linux, then the unit tests
LIGHTRON_E2E=1 go test -run E2E .  # macOS: starts a real app on your Chrome and checks the bridge, clipboard, …
go run ./examples/hello            # try it
```

The macOS menu library (`internal/macmenu/objc/menu.m`) is checked in built (`internal/macmenu/lightronmenu.dylib`)
so that apps build with `CGO_ENABLED=0` and no C compiler. After changing `menu.m`, run `make menulib` and commit
both.

## Before you send a change

- Read [docs/architecture.md](docs/architecture.md) first if you touch the engine, the bridge or the menus: most of
  what looks odd there is a workaround for something that fails silently. Keep the comment that says why.
- Test on the platform you changed. Windows changes need a real Windows machine (or VM); say in the pull request
  what you tried.
- `gofmt`, `make test`, and for macOS changes the e2e test.
- One topic per pull request. Commit messages say what changed and why.

## Scope

Lightron stays small: a window on the system's Chromium, a bridge, menus, packaging and updates. Features that an app
can build on top (tray icons, global shortcuts, a file dialog API) are better as separate packages unless they need
the engine's internals.
