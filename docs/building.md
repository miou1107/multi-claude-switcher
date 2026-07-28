# Building from source

Only needed if you want to change the code. Users should
[download a release](https://github.com/miou1107/multi-claude-switcher/releases/latest)
instead — the shipped apps update themselves.

Requires Go 1.22 or newer.

## Which binary is which

The repository builds four binaries, and picking the wrong one is the usual
first mistake:

| Binary | Platform | What it is |
|---|---|---|
| `cmd/mcs-menubar` | macOS | **The shipped macOS app.** Native NSPopover panel; needs CGO. |
| `cmd/mcs-tray` | Windows | **The shipped Windows app.** System-tray icon plus a WebView2 panel; pure Go, no CGO. |
| `cmd/mcs` | both | Command-line interface. Not shipped in releases — see [cli.md](cli.md). |
| `cmd/mcs-picker` | macOS | Helper window used by the legacy tray menu. Not part of the shipped panel flow. |

`cmd/mcs-tray` also builds on macOS, but it is the older static-menu app and is
not what releases contain. For a macOS build you almost certainly want
`cmd/mcs-menubar`.

## macOS

```bash
go build -o bin/mcs-menubar ./cmd/mcs-menubar
```

To produce a double-clickable `Multi-Claude Switcher.app` in `dist/` (universal
arm64 + Intel, ad-hoc signed, zipped), pass the version you are packaging:

```bash
./scripts/package-app.sh 0.10.1
```

## Windows

Pure Go — no CGO and no C toolchain:

```powershell
$env:CGO_ENABLED = '0'
go build -ldflags '-H=windowsgui' -o bin/mcs-tray.exe ./cmd/mcs-tray
```

`-H=windowsgui` makes it a GUI-subsystem executable, so it runs from the tray
with no console window.

To build the installer you also need [Inno Setup](https://jrsoftware.org/isinfo.php):

```powershell
& "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" /DMyAppVersion=0.10.1 packaging\windows-setup.iss
```

## CLI

```bash
go build -o bin/mcs ./cmd/mcs
```

## Tests

```bash
go test ./...
```

On Windows the repository is checked out with CRLF line endings, so `gofmt -l`
flags every file. Check formatting on LF-normalised copies rather than in place.

## Releasing

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which builds both
platforms, publishes the GitHub Release, and bumps the Homebrew cask in
`miou1107/homebrew-tap`. Nothing is released without a tag.

## Regenerating the README screenshot

`docs/assets/panel.png` is rendered from the shipped panel code with placeholder
accounts, not photographed, so it cannot drift from the real UI and never
contains anyone's real account names. After a UI change:

```bash
go run ./scripts/gen-screenshot panel.html
msedge --headless --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --screenshot=docs/assets/panel.png \
  --window-size=400,347 "file://$PWD/panel.html"
```

Any Chromium works in place of `msedge`.

## Other tools

`scripts/probe/probe_runner.py` inspects profiles and validates local session
synchronization. It is a maintainer's debugging tool, not part of either app.
