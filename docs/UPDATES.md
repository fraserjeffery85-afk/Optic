# Updates: one-time setup and releasing

Optic checks GitHub Releases each time it starts. When there's a newer version it asks before downloading, checks the download's signature, then runs the new installer, which closes Optic, updates it and opens it again.

## One-time setup (about 10 minutes)

1. **Create the repository.** On github.com create a **public** repository, for example `Optic`. It must be public so the people you share Optic with can download updates without logging in.
2. **Add the signing key secret.** In the repository open *Settings → Secrets and variables → Actions → New repository secret*. Name it `UPDATE_SIGNING_KEY` and paste the whole contents of `optic-update-signing-key.pem`, including the `BEGIN` and `END` lines.
   - Keep a backup of that file somewhere safe and offline (a USB stick or password manager). Don't share it and never commit it (`.gitignore` already blocks `*.pem`).
   - If it leaks, someone could sign fake updates. If it's lost, you'd need a new key (`go run ./tools/keygen`), and everyone would have to download one new installer by hand.
3. **Put the code in the repository.** Easiest: tell Claude the repository name (e.g. `yourname/Optic`) and Claude pushes the code. Or upload the contents of the repo zip yourself (GitHub → *Add file → Upload files*, drag everything in, commit to `main`).
4. **First release.** Pushing to `main` starts the *Release* action (*Actions* tab). When it finishes, the release page has `Optic-Setup-<version>.exe`. You and anyone you share it with run it once. From then on, updates come through Optic.

The repository name is built into each release, so copies built anywhere else (or the old zip versions) don't check for updates. That's why everyone needs to install once from the release.

## Releasing an update

1. Change the code, raise `appVersion` in `version.go` (e.g. 1.1.0 → 1.1.1) and add a `## 1.1.1 (date)` section at the top of `CHANGELOG.md`. That section becomes the release notes, the update bar's *What's new* text and the in-app *What's new* pop-up. The tests fail if the two versions don't match.
2. Push to `main`. The *Release* action sees a version that hasn't been released, then tests, builds, signs and publishes it. GitHub creates the `v1.1.1` tag itself. Pushes that don't change the version publish nothing.
3. The next time anyone opens Optic, they see "Optic 1.1.1 is available".

You can also start the action by hand: *Actions → Release → Run workflow*. Or ask Claude to "release it".

## What happens on each computer

- About 5 seconds after Optic starts, it asks GitHub for the latest release (a few kilobytes). The installer (about 3 MB) downloads only when someone clicks **Download and install**.
- The download is checked against the SHA-256 in `update.json`, and `update.json` must carry a valid Ed25519 signature from your key. Anything else is refused.
- **Skip this version** hides that version until a newer one appears. Settings → *Check for updates* still finds it. The start-up check can be turned off in Settings.
- Downloaded installers go to `%APPDATA%\Optic\updates` and are deleted the next time Optic starts.
- The installer runs silently (`/S /UPDATE`): it asks the running Optic to close (Optic puts the screen colours back first), replaces `Optic.exe` in `%LOCALAPPDATA%\Programs\Optic`, and opens it again. Looks and settings in `%APPDATA%\Optic` are never touched.

## Discord announcements

Each new release is posted in your Discord channel with the full release notes and a download link. `tools/discord_notify.py` writes the post; `release.yml` runs it right after publishing.

1. **Create the webhook.** In Discord: *Server Settings → Integrations → Webhooks → New Webhook*, choose the channel, then *Copy Webhook URL*.
2. **Add the secret.** In the GitHub repository: *Settings → Secrets and variables → Actions → New repository secret*, name `DISCORD_WEBHOOK`, value = the webhook URL. (A URL ending in `/github` works too; the suffix is removed.) Without this secret, releases still publish and the Discord step just says it skipped.
3. **Delete any old GitHub → Discord webhook.** If you ever added the Discord URL under the repository's *Settings → Webhooks* (the `/github` kind), delete it there. It only posts the release title, and it would double up with the new posts.

**Re-posting.** *Actions → Announce latest release on Discord → Run workflow* posts the latest release again. Leave **include_intro** ticked to start the post with the introduction from `docs/discord-intro.md` (edit that file to change the wording), or untick it for a plain release post. Normal release posts never include the intro.
