# Windows Audio Output Switcher

A small, self-contained tray utility for Windows that lets you cycle through
your audio playback devices with a single hotkey, and shows an on-screen
overlay telling you which one is now active.

It's a single ~4 MB `.exe` with no installation dependencies, no admin
rights required, and nothing running except while you're logged in.

## What it does

- **Cycles the default playback device.** Press **Win+A** (the default
  hotkey, changeable), left-click the tray icon, or pick a device from the
  right-click menu, and Windows' default audio output moves to the next
  active playback device in the list, wrapping back to the first one after
  the last. It takes effect for every application immediately — the same as
  changing it by hand in the Windows sound settings.
- **`Win+A` actually works for this**, even though Windows normally
  reserves it for Quick Settings (same goes for any other reserved combo
  you configure it to instead). See
  [how](DETAILS.md#binding-win-shortcuts).
- **Shows an on-screen overlay on every switch** — styled after Windows'
  own volume/brightness OSD, not a toast notification — naming the device
  that is now active, so you get instant confirmation of what you just
  switched to. A burst of rapid switches only ever shows the latest one.
- **Lives in the system tray**:
  - **Left-click** the tray icon to switch to the next output device.
  - Hovering it shows the app version and the currently active device.
  - **Right-click** opens a menu listing every currently active output
    device — click one to switch to it directly (the active one is
    checked) — followed by *Configure* and *Exit*.
- **Configure** opens `%APPDATA%\AudioOutputSwitcher\devices.yaml` in
  whatever app Windows has associated with `.yaml` files. There you can
  rename a device (`alias`), leave one out of the cycling (`skip`), and
  change the `hotkey` — or switch it off entirely, leaving the tray icon
  and menu as the only way to switch. Edits are picked up automatically,
  no restart needed. Every field is described in the
  [configuration reference](DETAILS.md#configuration).

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

## Uninstalling

Run `Setup_AudioOutputSwitcher.exe` again and choose **2) Uninstall**. It
stops the running app, removes it from the Startup folder along with its
saved device config, and finally removes itself — nothing is left behind.

## Something not working?

The switcher has no console window, so failures (a hotkey that couldn't be
registered, COM errors, ...) go to `%TEMP%\AudioOutputSwitcher.log` — see
[Troubleshooting](DETAILS.md#troubleshooting).

## More

[DETAILS.md](DETAILS.md) has the full configuration reference, how to build
from source, how the switching and the `Win+` hotkey actually work, and
what lives where in the repository.
