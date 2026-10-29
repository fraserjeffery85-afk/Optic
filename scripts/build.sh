#!/usr/bin/env bash
# Builds Optic.exe and the one-file installer (dist/Optic-Setup-<version>.exe).
# Works on Linux (and the GitHub Actions runner) - needs Go, mingw-w64 binutils (windres) and NSIS.
#
#   scripts/build.sh            build the exe + installer
#   scripts/build.sh --sign     also write dist/update.json + .sig (needs OPTIC_UPDATE_KEY or -key file)
#
# The version comes from version.go (appVersion). UPDATE_REPO (owner/name) turns on
# in-app update checks; GitHub Actions sets it to the repository automatically.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(sed -n 's/^const appVersion = "\(.*\)"/\1/p' version.go)
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "couldn't read appVersion from version.go"; exit 1; }
REPO="${UPDATE_REPO:-${GITHUB_REPOSITORY:-}}"
WINDRES="${WINDRES:-x86_64-w64-mingw32-windres}"
echo "Building Optic $VERSION${REPO:+ (updates from $REPO)}"

rm -rf dist && mkdir -p dist/build

# icon, manifest and version details baked into the exe
sed -e "s/@VERSION@/$VERSION/g" -e "s/@VERCOMMA@/${VERSION//./,},0/g" res/app.rc.in > res/app.rc
(cd res && "$WINDRES" --preprocessor=cat -O coff -o ../rsrc_windows_amd64.syso app.rc)

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-H windowsgui -s -w -X main.updateRepo=$REPO" -o dist/build/Optic.exe .

(cd installer && makensis -V2 -DVERSION="$VERSION" -DSRC=../dist/build -DOUT="../dist/Optic-Setup-$VERSION.exe" optic.nsi)
echo "Installer: dist/Optic-Setup-$VERSION.exe"

if [[ "${1:-}" == "--sign" ]]; then
  go run ./tools/makeupdate -installer "dist/Optic-Setup-$VERSION.exe" -version "$VERSION" -out dist/update ${OPTIC_UPDATE_KEY_FILE:+-key "$OPTIC_UPDATE_KEY_FILE"}
  echo "Signed: dist/update/update.json + update.json.sig"
fi
