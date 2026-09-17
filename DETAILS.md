# Details

Everything beyond [the README](README.md): the full configuration and
command-line reference, scripted setup, troubleshooting, building from source, and how the two tricky
Windows parts actually work.

## Configuration

*Configure* in the tray menu opens
`%LOCALAPPDATA%\AudioOutputSwitcher\config.yaml` in whatever app Windows has
associated with `.yaml` files, for you to hand-edit:

- `hotkey` — the global shortcut, e.g. `win+a` or `ctrl+alt+f9`. A config
  file created from scratch starts out with `win+a`. Leave the value
  empty, set it to `disabled`, or delete the line to switch the hotkey off
  entirely — the tray icon and its menu keep working, and the app writes
  your "off" value back unchanged. Switching to a bad or unregistrable
  value keeps whatever hotkey was already working active instead of
  leaving you with none.
- `autostart` — `true` (the default, also when left blank) keeps a
  shortcut to the installed exe in your Startup folder so the app starts
  at login; `false` removes it. The app applies this every time it starts
  and whenever you save the file, and setup does the same on install. The
  exe itself always lives in `%LOCALAPPDATA%\AudioOutputSwitcher`, never in the
  Startup folder.
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

Edits are picked up automatically, no restart needed. A value that isn't
valid (say `poll_seconds: soon`, or a hotkey that doesn't parse) falls back
to that setting's default while everything else in the file still applies;
keys the app doesn't know are ignored. A file that isn't YAML at all (e.g.
a missing bracket) is ignored as a whole, keeping the previous settings.
Either way the app tells you on screen, and won't write to the file until
it's fixed.

Setup's window closes itself 5 seconds after a successful install, update
or uninstall, and stays open after an error.

Up to v1.0.6 the config lived in `%APPDATA%\AudioOutputSwitcher`; setup
and the app move it over on their own.

## Command line

`AudioOutputSwitcher.exe` takes one flag per setting, which wins over the
config file for as long as that copy runs (the file itself keeps its own
values):

| Flag                   | Setting                                                     |
| ---------------------- | ----------------------------------------------------------- |
| `-hotkey <combo>`      | `hotkey`, e.g. `-hotkey ctrl+alt+f9` or `-hotkey disabled`  |
| `-autostart=false`     | `autostart` — also adds/removes the Startup folder shortcut |
| `-poll-seconds <n>`    | `poll_seconds`                                              |
| `-enable-logging`      | write `AudioOutputSwitcher.log` next to the exe             |

Only one copy runs at a time: starting a second one while the first is
still running does nothing.

`Setup_AudioOutputSwitcher.exe` shows its window unless started with
`-mode install` or `-mode uninstall`, which does that straight away and
prints its progress to the console it was started from — for scripting.
It exits with `0` on success and `1` on failure; it doesn't delete itself
in this mode. Since setup is a GUI program, shells don't wait for it on
their own: use `start /wait Setup_AudioOutputSwitcher.exe -mode install` in
`cmd`, or `Start-Process -Wait -NoNewWindow` in PowerShell.

## Troubleshooting

The switcher has no console window, so if a switch doesn't seem to work,
exit it from the tray and start it again with `-enable-logging`. It then
writes `AudioOutputSwitcher.log` next to the exe, in
`%LOCALAPPDATA%\AudioOutputSwitcher` (hotkey registration failures, COM
errors, etc. are all logged there). Logging is off otherwise.

## Building from source

Requires Go 1.24+ on Windows (the code is Windows-only):

```sh
go build -ldflags "-H=windowsgui" -o AudioOutputSwitcher.exe ./cmd/switcher
go build -ldflags "-H=windowsgui" -o Setup_AudioOutputSwitcher.exe ./cmd/setup
go test ./...
```

(Cross-compiling from Linux/WSL also works by prefixing the build commands
with `GOOS=windows GOARCH=amd64`.)

The icon is drawn in code by [`tools/genicon`](tools/genicon), which writes
`assets/icons/icon.ico` (the tray icon); both exes carry it as their file
icon through the committed `rsrc_windows_amd64.syso` next to their
`main.go`, setup's together with `assets/setup.manifest` (common controls
v6 for its task dialog, sharp text at any display scaling, no admin
prompt). After changing the glyph, regenerate all three:

```sh
go run ./tools/genicon assets/icons/icon.ico
go run github.com/akavel/rsrc@latest -arch amd64 -ico assets/icons/icon.ico -o cmd/switcher/rsrc_windows_amd64.syso
go run github.com/akavel/rsrc@latest -arch amd64 -ico assets/icons/icon.ico -manifest assets/setup.manifest -o cmd/setup/rsrc_windows_amd64.syso
```

A test fails if the committed files don't match what `genicon` renders.

Both pipelines run on Windows runners:
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) checks formatting,
builds, vets and tests every push to `main` and every pull request, and
[`.github/workflows/build.yml`](.github/workflows/build.yml) — triggered
by pushing a tag matching `v*.*.*` — runs the same checks, builds both
binaries and publishes them on a new GitHub release.

Setup always installs the newest release: it reads the version from where
`releases/latest` redirects to and downloads
`releases/latest/download/AudioOutputSwitcher.exe`, through WinINet
(Windows' own HTTP stack, with the system proxy and certificates). It never
calls the GitHub API, whose rate limit breaks installs on shared networks.

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
stops Quick Settings from also opening. This is the same technique tools like
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
| `cmd/setup`              | Setup's task dialog and `-mode` flag                             |
| `internal/audio`         | Core Audio API + `IPolicyConfig` bindings, device notifications  |
| `internal/llhotkey`      | Global hotkey via a low-level keyboard hook                      |
| `internal/hotkeycfg`     | Parses hotkey combo strings like `"ctrl+alt+f9"`                 |
| `internal/osd`           | The volume-OSD-style on-screen switch notification               |
| `internal/outputconfig`  | Persists hotkey/poll interval/per-device settings to `config.yaml` |
| `internal/autostart`     | Keeps the Startup folder shortcut in line with `autostart`       |
| `internal/install`       | Install/update/uninstall logic and setup's countdowns            |
| `internal/updater`       | Latest-release lookup and download over WinINet                  |
| `assets/icons`           | Embedded tray icon                                               |
| `tools/genicon`          | Draws the icon and checks the committed icon files               |
