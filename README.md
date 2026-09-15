# Windows Audio Output Switcher

A small, self-contained tray utility for Windows that lets you cycle through
your audio playback devices with a single hotkey, and shows an on-screen
overlay telling you which one is now active.

## What it does

- **Cycles the default playback device.** Press **Win+S** (the default
  hotkey, changeable — see *Configure* below), left-click the tray icon,
  or pick a device from the right-click menu, and Windows' default audio
  output moves to the next active playback device in the list, wrapping
  back to the first one after the last. This updates the default for all
  three roles Windows tracks (console, multimedia, communications), so it
  takes effect for every application immediately — the same as changing
  it by hand in the Windows sound settings.
- **`Win+S` actually works for this**, even though Windows normally
  reserves it for Search (same goes for any other reserved combo you
  configure it to instead). See [how](#binding-win-shortcuts) below.
- **Shows an on-screen overlay on every switch** — styled after Windows'
  own volume/brightness OSD, not a toast notification — naming the device
  (or its alias, see below) that is now active, so you get instant
  confirmation of what you just switched to. A burst of rapid switches
  only ever shows the latest one.
- **Lives in the system tray**:
  - **Left-click** the tray icon to switch to the next output device.
  - Hovering it shows the app version and the currently active device.
  - **Right-click** opens a menu listing every currently active output
    device — click one to switch to it directly (the active one is
    checked) — followed by *Configure* and *Exit*.
- **Configure** opens `%APPDATA%\AudioOutputSwitcher\devices.yaml` in
  whatever app Windows has associated with `.yaml` files, for you to
  hand-edit:
  - `hotkey` — the global shortcut, e.g. `win+s` or `ctrl+alt+f9`;
    defaults to `win+s` if left blank. Switching to a bad or unregistrable
    value keeps whatever hotkey was already working active instead of
    leaving you with none.
  - `poll_seconds` — how often, in the background, to check for
    plugged/unplugged devices and pick up hand-edits to this file;
    defaults to `60` if left blank or set to `0` or less. Switching
    outputs always re-checks immediately regardless of this interval.
  - `outputs` — kept in sync with every device that's ever been seen: a
    newly connected device is added automatically, and a disconnected one
    keeps its row (never deleted automatically, only by editing it out
    yourself) — with, per device:
    - `alias` — the name shown for it in the right-click menu and the
      OSD; defaults to the real device name.
    - `skip` — set to `true` to leave it out of hotkey/left-click
      cycling. It's still shown in the right-click menu (as
      "*(excluded)*") and stays fully clickable there, so you can switch
      to it directly at any time.
    - `last_seen` — when it was last detected as active, updated
      automatically; useful for spotting stale entries worth deleting by
      hand.

  Edits are picked up automatically, no restart needed.

It's a single ~8 MB `.exe` with no installation dependencies, no admin
rights required, and nothing running except while you're logged in.

## Installing

1. Grab `Setup_AudioOutputSwitcher.exe` from the
   [latest release](../../releases/latest) and run it.
2. Choose **1) Install or update**. It downloads the newest
   `AudioOutputSwitcher.exe`, places it in your Startup folder, and starts
   it immediately — no reboot needed.
3. Running setup again at any time re-checks for updates. It always deploys
   under the same fixed filename, so there is only ever a single Startup
   entry, and it makes sure the process that ends up running is the one it
   just installed.

The switcher itself has no window; look for its icon in the system tray
(you may need to expand the "hidden icons" arrow the first time).

## Troubleshooting

The switcher has no console window, so if a switch doesn't seem to work,
check `%TEMP%\AudioOutputSwitcher.log` for details (hotkey registration
failures, COM errors, etc. are all logged there).

## Uninstalling

Run `Setup_AudioOutputSwitcher.exe` again and choose **2) Uninstall**. It
stops the running app, removes it from the Startup folder along with its
saved device config, and finally removes itself — nothing is left behind.

## Building from source

Requires Go 1.24+. All commands target `windows/amd64`:

```sh
GOOS=windows GOARCH=amd64 go build -o AudioOutputSwitcher.exe ./cmd/switcher
GOOS=windows GOARCH=amd64 go build -o Setup_AudioOutputSwitcher.exe ./cmd/setup
```

The official releases are built by
[`.github/workflows/release.yml`](.github/workflows/release.yml), which also
embeds `assets/icons/icon.ico` as each `.exe`'s icon resource. Pushing a tag
matching `v*.*.*` builds both binaries and publishes them on a new GitHub
release.

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
| `internal/audio`         | Core Audio API + `IPolicyConfig` bindings                        |
| `internal/llhotkey`      | Global hotkey via a low-level keyboard hook                      |
| `internal/hotkeycfg`     | Parses hotkey combo strings like `"ctrl+alt+f9"`                 |
| `internal/osd`           | The volume-OSD-style on-screen switch notification               |
| `internal/aumid`         | Locates a legacy Start Menu shortcut for uninstall cleanup       |
| `internal/outputconfig`  | Persists hotkey/poll interval/per-device settings to `devices.yaml` |
| `internal/install`       | Install/update/uninstall logic shared by `cmd/setup`             |
| `internal/updater`       | GitHub release lookup/download used by `internal/install`        |
| `assets/icons`           | Embedded tray/exe icon                                           |
