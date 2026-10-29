# Changelog

Each section here appears in Optic's "What's new" pop-up and as the release notes on GitHub. Newest first. Format: `## <version> (<date>)`, an optional "Changes since …" line, then `- **Title**: description` bullets.

## 1.1.0 (29 October 2026)
Changes since Optic V1.0.3
- **One-file installer**: Optic now comes as Optic-Setup.exe. It installs for you only (no admin password), adds desktop and Start menu shortcuts, opens Optic when it's done, and appears in Windows Settings → Apps for uninstalling.
- **Automatic updates**: when a new version is out, Optic shows a bar with Download and install, What's new and Skip this version. Nothing downloads until you click.
- **Signed updates**: Optic only installs updates signed by OB Industries and checks every download against its signed checksum.
- **Check for updates**: in Settings, with an option to turn off the check when Optic starts.

## 1.0.3 (29 October 2026)
Changes since Optic V1.0.2
- **More precise sliders**: sliders now move in half steps, with − and + buttons for exact nudges (hold to repeat). Arrow keys work too (Shift for bigger steps).
- **Type an exact value**: click any slider's number and type the value you want.
- **Clear saving**: changes still show live, but each look now says Saved or Unsaved changes, with Save look, Save as new look and Undo changes buttons.
- **No lost changes**: switching looks with unsaved changes asks whether to save them first.
- **Clearer delete**: Delete look is clearly labelled and asks to confirm. Built-in looks can't be deleted, and say so.
- **Exit Optic button**: in Settings, to close Optic completely without hunting for the tray icon.

## 1.0.2 (28 October 2026)
Changes since Optic V1.0.1
- **Fixed: screen flicker**: if the old ClearRide was still running, both apps fought over your screen colours and the screen flickered. Optic now offers to close ClearRide when it starts, and removes ClearRide's old startup entry.
- **Flicker protection**: if any other program keeps changing your screen colours (f.lux, Windows Night light, graphics-card colour settings), Optic stops re-applying and tells you, instead of flickering.
- **Smoother alt-tab**: switching windows or minimising Optic no longer briefly toggles the look on and off.

## 1.0.1 (28 October 2026)
Changes since Optic V1.0
- **ReShade switch without launching**: pick Race setup or Photo setup in Optic, then start MX Bikes from the MXB App or Steam as normal. Optic no longer needs to launch the game.
- **Switch any time**: change setup while the game is open and Optic remembers it, switching ReShade the moment you close the game.
- **ReShade status at a glance**: the ReShade card shows whether ReShade is ON or OFF right now.

## 1.0.0 (28 October 2026)
Changes since ClearRide 2.0.1
- **A new name: Optic**: ClearRide is now Optic, because it works with MX Bikes and any other game you add.
- **New logo and look**: the split-O logo in white and red, a red-and-white theme, and a new desktop and taskbar icon.
- **Created by OB Industries**: shown in the header, at the bottom of the window, in Settings and in the program's file details.
- **Your settings come with you**: looks and settings from ClearRide are copied across automatically, and "Start with Windows" now starts Optic.
- **What's new pop-up**: this window. It appears once after each update, and you can open it again from Settings.
