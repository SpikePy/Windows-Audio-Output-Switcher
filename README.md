# Windows Audio Output Switcher

A small, self-contained tray utility for Windows that lets you cycle through
your audio playback devices with a single hotkey, and shows a notification
telling you which one is now active.

## What it does

- **Cycles the default playback device.** Press **Win+S**, left-click the
  tray icon, or pick a device from the right-click menu, and Windows'
  default audio output moves to the next active playback device in the
  list, wrapping back to the first one after the last. This updates the
  default for all three roles Windows tracks (console, multimedia,
  communications), so it takes effect for every application immediately —
  the same as changing it by hand in the Windows sound settings.
- **`Win+S` actually works for this**, even though Windows normally
  reserves it for Search. See [how](#binding-win-shortcuts) below.
- **Shows a toast notification on every switch**, naming the device that is
  now active, so you get instant confirmation of what you just switched to.
  Pressing the hotkey again quickly replaces the previous toast instead of
  piling up a stack of them.
- **Lives in the system tray**:
  - **Left-click** the tray icon to switch to the next output device.
  - **Right-click** opens a menu listing every currently active output
    device — click one to switch to it directly (the active one is
    checked) — followed by *Configure Outputs...* and *Exit*.
- **Configure Outputs** opens a small window listing every output
  (including ones that aren't plugged in right now) with a checkbox for
  each. Unchecking one excludes it from `Win+S`/left-click cycling — it's
  simply skipped over, and shown as "*(excluded from cycling)*" in the
  right-click device list — but stays fully clickable there, so you can
  still switch to it directly at any time. (Windows menu items can't be
  greyed out without also disabling the click, so this label is the
  closest equivalent that keeps it usable.) The choice is saved to
  `%APPDATA%\AudioOutputSwitcher\outputs.yaml`; a device you've never
  seen before is included by default, and one you've excluded keeps that
  setting even while it's disconnected.

It's a single ~8 MB `.exe` with no installation dependencies, no admin
rights required, and nothing running except while you're logged in.

## Installing

1. Grab `Install_AudioOutputSwitcher.exe` from the
   [latest release](../../releases/latest) and run it.
2. It downloads the newest `AudioOutputSwitcher.exe`, places it in your
   Startup folder, and starts it immediately — no reboot needed.
3. Running the installer again at any time re-checks for updates. It always
   deploys under the same fixed filename, so there is only ever a single
   Startup entry, and it makes sure the process that ends up running is the
   one it just installed.

The switcher itself has no window; look for its icon in the system tray
(you may need to expand the "hidden icons" arrow the first time).

## Troubleshooting

The switcher has no console window, so if a switch or notification doesn't
seem to work, check `%TEMP%\AudioOutputSwitcher.log` for details (hotkey
registration failures, COM errors, failed notifications, etc. are all
logged there).

## Uninstalling

Grab `Uninstall_AudioOutputSwitcher.exe` from the same release and run it. It
stops the running app, removes it from the Startup folder, and finally
removes itself — nothing is left behind.

## Building from source

Requires Go 1.24+. All commands target `windows/amd64`:

```sh
GOOS=windows GOARCH=amd64 go build -o AudioOutputSwitcher.exe ./cmd/switcher
GOOS=windows GOARCH=amd64 go build -o Install_AudioOutputSwitcher.exe ./cmd/installer
GOOS=windows GOARCH=amd64 go build -o Uninstall_AudioOutputSwitcher.exe ./cmd/uninstaller
```

The official releases are built by
[`.github/workflows/release.yml`](.github/workflows/release.yml), which also
embeds `assets/icons/icon.ico` as each `.exe`'s icon resource. Pushing a tag
matching `v*.*.*` builds all three binaries and publishes them on a new
GitHub release.

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

| Path                | Purpose                                                          |
| ------------------- | ----------------------------------------------------------------- |
| `cmd/switcher`       | The tray application                                             |
| `cmd/installer`      | Downloads the latest release into the Startup folder             |
| `cmd/uninstaller`    | Removes everything the installer set up                          |
| `internal/audio`        | Core Audio API + `IPolicyConfig` bindings                       |
| `internal/llhotkey`     | Global hotkey via a low-level keyboard hook                     |
| `internal/hotkeycfg`    | Parses hotkey combo strings like `"ctrl+alt+f9"`                |
| `internal/notifier`     | Toast notifications                                              |
| `internal/configwindow` | The native "Configure Outputs" window                           |
| `internal/outputconfig` | Persists excluded outputs to `outputs.yaml`                     |
| `internal/updater`      | GitHub release lookup/download shared by installer/uninstaller  |
| `assets/icons`          | Embedded tray/exe icon                                           |
