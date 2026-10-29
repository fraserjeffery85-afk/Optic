# Optic by OB Industries

*See more in every shadow.*

Optic brightens shadows and boosts colour while you play, then puts your screen back to normal the moment you leave the game.

It never touches the game itself. It only adjusts your monitor's colour output (like your graphics card's colour settings), so it can't crash the game and anti-cheat has nothing to detect. Works with NVIDIA, AMD and Intel graphics.

## Install

1. Download **`Optic-Setup-<version>.exe`** from the **Releases** page of the Optic GitHub repository (right-hand side of the repository page) and run it.
   - Windows may say "Windows protected your PC" because the installer isn't signed with a paid certificate. Click **More info**, then **Run anyway**.
2. Click **Install**. Optic installs just for you (no administrator password), adds shortcuts to your desktop and Start menu, and opens when it's done.
3. MX Bikes is found automatically through Steam. For other games, click **+ Add a game** and pick the game's .exe.
4. Click a look. The big preview shows "Normal" vs "With Optic" on your latest game screenshot. Drag the white line to compare.
5. Play (start the game from the MXB App or Steam as usual). Optic switches on by itself while you're in the game.

Tip: open the settings (gear icon) and turn on **Start with Windows**. Optic then waits in the tray (by the clock) and is always ready when you play.

## Updates

When a new version is out, Optic shows a bar at the top: **Download and install**, **What's new**, **Skip this version** or **Not now**. Nothing downloads until you click. Optic then closes, updates and opens again in a few seconds, keeping your looks and settings.

- Updates are signed by OB Industries. Optic checks every download against its signed checksum and refuses anything that doesn't match.
- Settings → **Check for updates** checks straight away. You can also turn off the check when Optic starts.

## Setting up without the game open

You don't need the game running to set things up:
- The preview uses your newest MX Bikes screenshot automatically. Or click "Test picture", or "Choose a picture..." to use any image.
- "Try it on my screen" shows the look on your whole desktop while you adjust.
- With two monitors, put Optic on one screen and the game on the other. The look stays on the game's screen while you adjust.

## In-game keys (work while riding)

| Keys | What it does |
| --- | --- |
| Ctrl+Alt+F5 / F6 | Shadows down / up |
| Ctrl+Alt+F7 / F8 | Darker / brighter |
| Ctrl+Alt+F10 | Next look |
| Ctrl+Alt+F9 | Optic on / off |
| Ctrl+Alt+F12 | **Reset my screen** (back to normal, Optic switched off) |

Changes from F5-F8 are saved to the look you're using.

## Looks

Built in: Off, ReShade Style (Levels + Clarity), Shadow Detail, Night / Supercross, Bright Day / Glare, Punchy Colour, Max Visibility, Warm, Cool.

- Move a slider and you see the change live. The look then shows "Unsaved changes" with three buttons: Save look, Save as new look (puts the changes in a new look and leaves the original as it was) and Undo changes. Switching looks with unsaved changes asks whether to save first.
- Precise control: the - and + buttons nudge in half steps (hold to repeat), the arrow keys work on a selected slider (Shift = bigger steps), and you can click any slider's number to type an exact value.
- Double-click a slider or its name to put it back to normal.
- "Make your own" copies the current look so you can tweak it.
- The three main sliders are Shadows, Brightness and Colour. "More settings" has highlights, contrast, mid-tones, levels, black floor, warmth, tint and red/green/blue.
- Share: "Share (export)" saves a small file. Your friend clicks "Add a shared look" to load it.
- The Colour slider needs NVIDIA or AMD graphics. Everything else works on any PC.

## Race setup / Photo setup (for games with ReShade, like MX Bikes)

Optic doesn't need to launch the game. Pick a setup, then start MX Bikes from the MXB App or Steam as normal:
- Race setup: ReShade off, Optic on. No ReShade-related crashes.
- Photo setup: ReShade on with your presets, Optic paused.

ReShade can't be switched while the game is open (Windows locks its file), so if MX Bikes is running, Optic remembers your choice and switches ReShade the moment you close the game. Optic itself can be turned on or off any time, even mid-race, with Ctrl+Alt+F9.

Optic switches ReShade by renaming its DLL (e.g. opengl32.dll <-> opengl32.dll.off). Presets and shaders are never touched.

Why: in MX Bikes, any active ReShade effect can make the game lose its 2D drawing buffer, which leads to crashes. Racing with ReShade off avoids that.

## If something looks wrong

- Press Ctrl+Alt+F12, or use "Reset my screen" in the settings or the tray menu.
- If the PC crashed with a look on, just start Optic again. It restores your normal colours automatically.
- Your own monitor calibration is kept: looks are applied on top of it and removed cleanly.
- "Windows refused the colour change": Windows limits how strong a look can be. Use gentler settings, or Settings > "Allow stronger looks...", then sign out of Windows and back in.
- Screen flickering? Another program is changing your screen colours too (the old ClearRide, f.lux, Windows Night light, graphics-card colour settings). Optic detects this, stops re-applying and tells you. Close the other program. Optic offers to close the old ClearRide automatically.
- Windows HDR must be off. HDR ignores this kind of colour adjustment.
- Optic needs the Microsoft Edge WebView2 Runtime. Windows 10 and 11 almost always have it; if not, Optic offers the download link.
- Log file for troubleshooting: `%APPDATA%\Optic\optic.log`

## Known MX Bikes issue: the faint black box

Some players get a faint tinted rectangle over the screen ("like sunglasses"), usually after several races or track changes. It's an old MX Bikes bug, not caused by Optic. Workaround: in the game's video settings, switch to a different resolution and back, or restart the game.

## What's new pop-up

After every update, Optic shows what changed since the previous build, once. Open it again any time from Settings > What's new.

## Upgrading from ClearRide or an older Optic

Optic is the new name for ClearRide. Just run the installer: it closes the old app, copies your looks and settings across and replaces the old "Start with Windows" entry. You can then delete any old ClearRide.exe or Optic.exe you kept in a folder.

## Uninstall

Windows Settings → Apps → **Optic** → Uninstall. Optic closes, your screen goes back to normal, and the shortcuts and startup entry are removed. Your looks and settings stay in `%APPDATA%\Optic` in case you reinstall; delete that folder to remove them too.

---
Created by OB Industries.
