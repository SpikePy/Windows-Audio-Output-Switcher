# Details

Everything beyond [the README](README.md): the full configuration
reference, troubleshooting, building from source, and how the two tricky
Windows parts actually work.

## Configuration

*Configure* in the tray menu opens
`%APPDATA%\AudioOutputSwitcher\devices.yaml` in whatever app Windows has
associated with `.yaml` files, for you to hand-edit:

- `hotkey` — the global shortcut, e.g. `win+s` or `ctrl+alt+f9`; defaults
  to `win+s` if left blank. Switching to a bad or unregistrable value keeps
  whatever hotkey was already working active instead of leaving you with
  none.
- `poll_seconds` — how often to check this file for hand-edits (and
  re-check devices as a fallback); defaults to `60` if left blank or set to
  `0` or less. Device changes — plugging in, unplugging, or changing the
  default output in Windows' own sound settings — are picked up instantly
  via Windows' device notifications, regardless of this interval.
- `outputs` — kept in sync with every device that's ever been seen, matched
  by its Windows device ID (so renaming a device, or having two with the
  same name, keeps their settings apart): a newly connected device is added
  automatically, and a disconnected one keeps its row (never deleted
  automatically, only by editing it out yourself) — with, per device:
  - `id` — the Windows device ID the row belongs to; don't edit it.
  - `alias` — the name shown for it in the right-click menu and the OSD;
    filled in with the device's Windows name if left blank.
  - `last_seen` — the date it was last detected as active, updated
    automatically; useful for spotting stale entries worth deleting by
    hand.
  - `skip` — set to `true` to leave it out of hotkey/left-click cycling.
    It's still shown in the right-click menu (as "*(excluded)*") and stays
    fully clickable there, so you can switch to it directly at any time.

Edits are picked up automatically, no restart needed. If a save leaves the
file invalid (e.g. a YAML typo), the app keeps its previous settings, tells
you so, and won't write to the file until it's fixed.

## Troubleshooting

The switcher has no console window, so if a switch doesn't seem to work,
check `%TEMP%\AudioOutputSwitcher.log` for details (hotkey registration
failures, COM errors, etc. are all logged there).

## Building from source

Requires Go 1.24+ on Windows (the code is Windows-only):

```sh
go build -ldflags "-H=windowsgui" -o AudioOutputSwitcher.exe ./cmd/switcher
go build -o Setup_AudioOutputSwitcher.exe ./cmd/setup
go test ./...
```

(Cross-compiling from Linux/WSL also works by prefixing the build commands
with `GOOS=windows GOARCH=amd64`.)

Both pipelines run on Windows runners:
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) checks formatting,
builds, vets and tests every push to `main` and every pull request, and
[`.github/workflows/release.yml`](.github/workflows/release.yml) — triggered
by pushing a tag matching `v*.*.*` — builds both binaries, embeds
`assets/icons/icon.ico` as each `.exe`'s icon resource, and publishes them on
a new GitHub release.

## How the switch actually happens

Windows has no public API to change the default audio device — only to read
it. This tool uses the same undocumented `IPolicyConfig` COM interface that
Windows' own sound settings UI and tools like EarTrumpet or NirCmd rely on;
it has been stable since Windows 7. See
[`internal/audio`](internal/audio) for the implementation.

## Binding Win+ shortcuts

Windows reserves most bare `Win+<letter>` combinations for the shell itself
(`Win+S` for Search, `Win+E` for Explorer, `Win+A` for Quick Settings, ...).
The standard way to claim a global hotkey, `RegisterHotKey`, doesn't help
here: the shell's own shortcut handling sees the keystroke first, so the
app's `WM_HOTKEY` never fires and the reserved action still happens.

Instead, [`internal/llhotkey`](internal/llhotkey) installs a low-level
keyboard hook (`WH_KEYBOARD_LL`), which runs earlier in the input pipeline
than the shell's shortcut handling. When the configured combo is detected,
the hook consumes the keystroke — rather than passing it on — which is what
stops Search from also opening. This is the same technique tools like
AutoHotkey use to remap `Win+<key>` shortcuts.

**Trade-off:** a global low-level keyboard hook sees every keystroke typed
anywhere on the system (necessary to detect the combo at all — this app
only acts on the one combo it's watching for and otherwise passes every
other keystroke straight through unmodified). Antivirus/EDR software can
flag this pattern heuristically since it overlaps with how keyloggers work;
if that happens, it's a false positive rather than any actual keystroke
logging, and the source in `internal/llhotkey` is the whole of what runs.

## Project layout

| Path                     | Purpose                                                          |
| ------------------------ | ----------------------------------------------------------------- |
| `cmd/switcher`           | The tray application                                             |
| `cmd/setup`              | Interactive install/update/uninstall menu                       |
| `internal/audio`         | Core Audio API + `IPolicyConfig` bindings, device notifications  |
| `internal/llhotkey`      | Global hotkey via a low-level keyboard hook                      |
| `internal/hotkeycfg`     | Parses hotkey combo strings like `"ctrl+alt+f9"`                 |
| `internal/osd`           | The volume-OSD-style on-screen switch notification               |
| `internal/outputconfig`  | Persists hotkey/poll interval/per-device settings to `devices.yaml` |
| `internal/install`       | Install/update/uninstall logic shared by `cmd/setup`             |
| `internal/updater`       | GitHub release lookup/download over WinHTTP, for `internal/install` |
| `assets/icons`           | Embedded tray/exe icon                                           |
