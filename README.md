# Optic

**See more in every shadow.** A Windows app by OB Industries that brightens shadows and boosts colour while you play (MX Bikes and any other game), then puts the screen back to normal when you leave the game.

Optic never touches the game. It changes the display's gamma ramp (`SetDeviceGammaRamp`) and, on NVIDIA/AMD, digital vibrance (NVAPI / ADL), so it can't crash the game and works with ReShade off. End-user instructions are in [`docs/READ ME.md`](docs/READ%20ME.md); this file is for development.

## Quick start

```bash
go test ./...                    # unit tests (run on any OS)
GOOS=windows go vet -unsafeptr=false .   # Windows code
scripts/build.sh                 # dist/build/Optic.exe + dist/Optic-Setup-<version>.exe
```

`scripts/build.sh` needs Go (see `go.mod`), `x86_64-w64-mingw32-windres` (Debian/Ubuntu: `binutils-mingw-w64-x86-64`) and NSIS (`nsis`). It works on Linux, which is what GitHub Actions uses. It stamps the version from `version.go` into the exe's resources (`res/app.rc.in` → `res/app.rc` → `rsrc_windows_amd64.syso`), cross-compiles with `-H windowsgui`, then builds the installer.

To try the UI in a normal browser, open `ui/index.html`: without the Go backend it uses the built-in mock at the bottom of the file.

## Installer

`installer/optic.nsi` (NSIS, 64-bit, per-user, `RequestExecutionLevel user`):

- Installs to `%LOCALAPPDATA%\Programs\Optic` with desktop and Start menu shortcuts, opens Optic at the end, and registers an uninstaller in Windows Settings → Apps (HKCU, publisher OB Industries).
- Before copying, it runs the new `Optic.exe --quit`, which signals the named event `Local\OpticQuitEvent`. The running Optic restores the screen and exits. If it hasn't let go of the exe after about 3 seconds, the installer falls back to `taskkill` (the crash-recovery file restores the colours on next start). It also closes the old ClearRide.
- `/S /UPDATE` (used by in-app updates) installs silently and reopens Optic.
- The uninstaller removes files, shortcuts, the uninstall key and the `Run` startup value, and keeps `%APPDATA%\Optic`.

## Updates

See [`docs/UPDATES.md`](docs/UPDATES.md) for setup and releasing. In short:

- `update.go` (portable, tested): Ed25519 manifest verification, installer SHA-256/size check, version compare. The public key is `updatePublicKey`.
- `update_windows.go`: GitHub Releases check (`/repos/<repo>/releases/latest`), download with progress to `%APPDATA%\Optic\updates`, re-verify, run the installer with `/S /UPDATE`, skip version. State is pushed to the page through `window.onUpdate`.
- The update repo is baked in at build time with `-X main.updateRepo=<owner/name>` (from `UPDATE_REPO` or `GITHUB_REPOSITORY`). Builds without it never check for updates.
- Release assets: `Optic-Setup-<v>.exe`, `update.json`, `update.json.sig`. `scripts/build.sh --sign` writes the last two with `tools/makeupdate` (key from `OPTIC_UPDATE_KEY` or `OPTIC_UPDATE_KEY_FILE`). `.github/workflows/release.yml` publishes them when `main` has an unreleased `appVersion`, with that version's `CHANGELOG.md` section as the notes.
- `go run ./tools/keygen <file>.pem` makes a new key pair (refuses to overwrite) and prints the public key to paste into `update.go`.

## Layout

| Path | Responsibility |
| --- | --- |
| `version.go` | `appVersion` (single source of the version), names |
| `CHANGELOG.md`, `changelog.go` | Release history, embedded; feeds the *What's new* pop-up, update notes and GitHub release notes |
| `app_windows.go` | Window (WebView2), JS bindings, tray menu, game watching loop, options |
| `engine_windows.go` | Applies/restores gamma ramps, crash-recovery file, conflict back-off (stops fighting f.lux/Night light etc.) |
| `curve.go` | Pure colour maths: levels, shadows/highlights, gamma, contrast, temperature, RGB → ramp (tested) |
| `gpu_windows.go` | Saturation through NVIDIA NVAPI digital vibrance or AMD ADL |
| `steam_windows.go` | Steam library detection, ReShade on/off (DLL rename), Start with Windows, full-range registry unlock |
| `native_windows.go`, `win32_windows.go` | Tray icon, hotkeys (`RegisterHotKey`), dialogs, single instance, quit event |
| `config.go` | `%APPDATA%\Optic\config.json`, looks, migration from ClearRide |
| `update.go`, `update_windows.go` | Signed updates |
| `ui/index.html` | The whole UI (embedded), plus a mock backend for browser testing |
| `installer/optic.nsi`, `assets/` | Installer, icon, logo, sidebar image |
| `tools/keygen`, `tools/makeupdate` | Signing key and update manifest tools |

## Things that must not change

- Optic never writes to game files except renaming the ReShade DLL (`<dll>` ↔ `<dll>.off`) when the game isn't running.
- Original ramps are captured before the first change and saved to `restore.json`, so a crash or power cut is undone on the next start. Exit, the panic key (Ctrl+Alt+F12) and the installer's `--quit` all restore the screen.
- Updates are only installed when the manifest signature and the installer checksum both verify.

Created by OB Industries.
