# Windows Audio Output Switcher

A small, self-contained tray utility for Windows that lets you cycle through
your audio playback devices with a single hotkey, and shows a notification
telling you which one is now active.

## What it does

- **Cycles the default playback device.** Press the configured hotkey
  (`Win+S` by default) and Windows' default audio output moves to the
  next active playback device in the list, wrapping back to the first one
  after the last. This updates the default for all three roles Windows
  tracks (console, multimedia, communications), so it takes effect for
  every application immediately — the same as changing it by hand in the
  Windows sound settings.
- **Shows a toast notification on every switch**, naming the device that is
  now active, so you get instant confirmation of what you just switched to.
- **Lives in the system tray** with a small icon that reflects whether
  switching is currently enabled:
  - **Left-click** the tray icon to toggle switching on/off.
  - **Right-click** opens a menu with *Enable*, *Disable* and *Exit*.
  - While disabled, the hotkey is ignored and the icon turns grey.
- **Remembers its settings** (hotkey and enabled/disabled state) across
  restarts, in `%APPDATA%\AudioOutputSwitcher\config.json`.

It's a single ~8 MB `.exe` with no installation dependencies, no admin
rights required, and nothing running except while you're logged in.

## Installing

1. Grab `AudioOutputSwitcherSetup.exe` from the
   [latest release](../../releases/latest) and run it.
2. It downloads the newest `AudioOutputSwitcher.exe`, places it in your
   Startup folder, and starts it immediately — no reboot needed.
3. Running the installer again at any time re-checks for updates. It always
   deploys under the same fixed filename, so there is only ever a single
   Startup entry, and it makes sure the process that ends up running is the
   one it just installed.

The switcher itself has no window; look for its icon in the system tray
(you may need to expand the "hidden icons" arrow the first time).

## Configuring the hotkey

Edit `%APPDATA%\AudioOutputSwitcher\config.json` and restart the app:

```json
{
  "hotkey": "win+s",
  "enabled": true
}
```

Combine any of `ctrl`, `alt`, `shift`, `win` with a letter, digit, function
key (`f1`-`f20`), or one of `space`, `enter`, `escape`, `tab`, `delete`,
`left`, `right`, `up`, `down` — for example `"ctrl+shift+space"`.

> **Note:** Windows reserves several `Win+<letter>` combinations for the
> shell itself (Search, Explorer, Run, lock screen, ...). On some Windows
> versions the shell intercepts these before this app's hotkey ever fires,
> so `Win+S` may keep opening Windows Search instead of switching devices.
> If that happens, pick a combo that includes `ctrl`, `alt` or `shift`
> instead, e.g. `"ctrl+alt+f9"`.

## Uninstalling

Grab `AudioOutputSwitcherUninstall.exe` from the same release and run it. It
stops the running app, removes it from the Startup folder, deletes its
configuration, and finally removes itself — nothing is left behind.

## Building from source

Requires Go 1.24+. All commands target `windows/amd64`:

```sh
GOOS=windows GOARCH=amd64 go build -o AudioOutputSwitcher.exe ./cmd/switcher
GOOS=windows GOARCH=amd64 go build -o AudioOutputSwitcherSetup.exe ./cmd/installer
GOOS=windows GOARCH=amd64 go build -o AudioOutputSwitcherUninstall.exe ./cmd/uninstaller
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

## Project layout

| Path                    | Purpose                                                    |
| ------------------------ | ----------------------------------------------------------- |
| `cmd/switcher`           | The tray application                                       |
| `cmd/installer`          | Downloads the latest release into the Startup folder        |
| `cmd/uninstaller`        | Removes everything the installer set up                     |
| `internal/audio`         | Core Audio API + `IPolicyConfig` bindings                    |
| `internal/hotkeycfg`     | Parses hotkey combo strings like `"ctrl+alt+f9"`            |
| `internal/notifier`      | Toast notifications                                          |
| `internal/appstate`      | Persisted settings (`config.json`)                          |
| `internal/updater`       | GitHub release lookup/download shared by installer/uninstaller |
| `assets/icons`           | Embedded tray/exe icon                                       |
