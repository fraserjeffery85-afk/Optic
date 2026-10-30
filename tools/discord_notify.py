#!/usr/bin/env python3
"""Announce an Optic release in a Discord channel (standard library only).

Usage:
    python tools/discord_notify.py <version> <notes.md> <release url> [--intro docs/discord-intro.md]

Reads the webhook URL from DISCORD_WEBHOOK. Without it, prints a message and
exits 0, so a release never fails because of Discord. A URL ending in /github
(GitHub's own webhook format) is accepted and the suffix stripped.
"""
import json
import os
import re
import sys
import urllib.error
import urllib.request

BRAND_COLOUR = 0xE3262F  # Optic red
DESC_LIMIT = 4096        # Discord embed description limit
DIVIDER = "━━━━━━━━━━━━━━━━━━━━"


def repo_from(url):
    m = re.match(r"https://github\.com/([^/]+/[^/]+)/", url)
    return os.environ.get("GITHUB_REPOSITORY") or (m.group(1) if m else "fraserjeffery85-afk/Optic")


def webhook_url(raw):
    url = raw.strip().rstrip("/")
    if url.endswith("/github"):
        url = url[: -len("/github")]
    return url + ("&" if "?" in url else "?") + "wait=true"


def fit(text, tail, more):
    """Trim text at a line break so text + tail fits in one embed description."""
    if len(text) + len(tail) <= DESC_LIMIT:
        return text + tail
    room = DESC_LIMIT - len(tail) - len(more) - 1
    cut = text[:room]
    if "\n" in cut:
        cut = cut[: cut.rfind("\n")]
    return cut.rstrip() + "\n" + more + tail


def build_payload(version, notes, release_url, intro=""):
    repo = repo_from(release_url)
    logo = f"https://raw.githubusercontent.com/{repo}/v{version}/docs/optic-logo.png?v={version}"
    setup = f"Optic-Setup-{version}.exe"
    asset = f"https://github.com/{repo}/releases/download/v{version}/{setup}"
    body = notes.strip()
    if intro.strip():
        body = intro.strip() + "\n\n" + DIVIDER + "\n\n" + body
    tail = f"\n\n**[Download {setup}]({asset})** · [Release page]({release_url})"
    return {
        "username": "Optic",
        "avatar_url": logo,
        "allowed_mentions": {"parse": []},
        "embeds": [{
            "title": f"Optic {version} is out",
            "url": release_url,
            "color": BRAND_COLOUR,
            "thumbnail": {"url": logo},
            "description": fit(body, tail, f"… [full notes]({release_url})"),
            "footer": {"text": "Already installed? Open Optic and it will offer the update."},
        }],
    }


def main(argv):
    args, intro_file = [], None
    it = iter(argv)
    for a in it:
        if a == "--intro":
            intro_file = next(it, None)
        else:
            args.append(a)
    if len(args) != 3:
        print(__doc__.strip())
        return 2
    version, notes_path, release_url = args
    version = version.lstrip("v")

    raw = os.environ.get("DISCORD_WEBHOOK", "")
    if not raw.strip():
        print("DISCORD_WEBHOOK isn't set, so nothing was posted to Discord.")
        return 0

    with open(notes_path, encoding="utf-8") as f:
        notes = f.read()
    intro = ""
    if intro_file:
        with open(intro_file, encoding="utf-8") as f:
            # skip comment lines (<!-- ... -->) so the file can carry editing tips
            intro = re.sub(r"<!--.*?-->", "", f.read(), flags=re.S).strip()

    payload = build_payload(version, notes, release_url, intro)
    req = urllib.request.Request(
        webhook_url(raw),
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json", "User-Agent": "Optic-release-announcer"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as res:
            print(f"Posted Optic {version} to Discord (HTTP {res.status}).")
            return 0
    except urllib.error.HTTPError as e:
        print(f"Discord refused the post: HTTP {e.code} {e.read().decode('utf-8', 'replace')[:500]}")
    except urllib.error.URLError as e:
        print(f"Couldn't reach Discord: {e.reason}")
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
