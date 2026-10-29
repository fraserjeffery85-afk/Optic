package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// updateRepo is the GitHub repository releases come from ("owner/name"). The
// release workflow sets it with -ldflags "-X main.updateRepo=$GITHUB_REPOSITORY";
// builds made anywhere else leave it empty and never check for updates.
var updateRepo = ""

// updatePublicKey verifies update.json.sig (Ed25519). Its private half is the
// GitHub secret UPDATE_SIGNING_KEY. Made with: go run ./tools/keygen <file>
const updatePublicKey = "FqEyGWD0pc8FD0zDslcG/snB2annyInzdZK3avsF4iM="

type updateManifest struct {
	Format    int    `json:"format"`
	App       string `json:"app"`
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	Installer struct {
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"installer"`
}

var (
	reVersion   = regexp.MustCompile(`^\d{1,4}\.\d{1,5}\.\d{1,6}$`)
	reInstaller = regexp.MustCompile(`^Optic-Setup-\d{1,4}\.\d{1,5}\.\d{1,6}\.exe$`)
	reHex64     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const maxInstallerSize = 100 << 20

// verifyManifest checks the signature first, then the manifest's shape.
func verifyManifest(body []byte, sigText, pubB64 string) (*updateManifest, error) {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("this copy of Optic has no valid update key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigText))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return nil, errors.New("the update's signature isn't valid, so it wasn't installed")
	}
	var m updateManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.New("the update information is unreadable")
	}
	switch {
	case m.Format != 1, m.App != "Optic":
		return nil, errors.New("the update information is for a different app or format")
	case !reVersion.MatchString(m.Version):
		return nil, errors.New("the update's version is invalid")
	case !reInstaller.MatchString(m.Installer.Name), m.Installer.Name != "Optic-Setup-"+m.Version+".exe":
		return nil, errors.New("the update's installer name is invalid")
	case m.Installer.Size <= 0 || m.Installer.Size > maxInstallerSize:
		return nil, errors.New("the update's installer size is invalid")
	case !reHex64.MatchString(m.Installer.SHA256):
		return nil, errors.New("the update's checksum is invalid")
	case len(m.Notes) > 20000:
		return nil, errors.New("the update's notes are too long")
	}
	return &m, nil
}

// checkInstaller makes sure a downloaded installer is exactly the signed one.
func checkInstaller(data []byte, m *updateManifest) error {
	sum := sha256.Sum256(data)
	if int64(len(data)) != m.Installer.Size || hex.EncodeToString(sum[:]) != m.Installer.SHA256 {
		return errors.New("the download doesn't match its signed checksum, so it wasn't installed")
	}
	return nil
}

// compareVersions returns 1 if a > b, -1 if a < b, 0 if equal.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}
