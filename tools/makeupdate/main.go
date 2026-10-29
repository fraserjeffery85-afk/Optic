// Command makeupdate writes the signed update manifest for a release:
//
//	go run ./tools/makeupdate -installer dist/Optic-Setup-1.1.0.exe -version 1.1.0 -out dist/update
//
// It produces update.json (version, release notes from CHANGELOG.md, installer
// name/size/SHA-256) and update.json.sig (base64 Ed25519 signature of
// update.json's exact bytes). The private key comes from the OPTIC_UPDATE_KEY
// environment variable (PEM text) or -key <file>.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type manifest struct {
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

func fail(f string, a ...interface{}) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }

func notesFor(version string) string {
	b, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		fail("can't read CHANGELOG.md: %v", err)
	}
	head := regexp.MustCompile(`^##\s+(\d+\.\d+\.\d+)`)
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if m := head.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			if in {
				break
			}
			in = m[1] == version
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	n := strings.TrimSpace(strings.Join(out, "\n"))
	if n == "" {
		fail("CHANGELOG.md has no section for %s", version)
	}
	return n
}

func main() {
	inst := flag.String("installer", "", "installer .exe")
	ver := flag.String("version", "", "version, e.g. 1.1.0")
	outDir := flag.String("out", "dist/update", "output folder")
	keyFile := flag.String("key", "", "private key PEM file (else $OPTIC_UPDATE_KEY)")
	flag.Parse()
	if *inst == "" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(*ver) {
		fail("usage: makeupdate -installer <exe> -version <x.y.z> [-out dir] [-key file]")
	}
	keyPEM := []byte(os.Getenv("OPTIC_UPDATE_KEY"))
	if *keyFile != "" {
		var err error
		if keyPEM, err = os.ReadFile(*keyFile); err != nil {
			fail("can't read key: %v", err)
		}
	}
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		fail("no signing key: set OPTIC_UPDATE_KEY (the UPDATE_SIGNING_KEY secret) or pass -key")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		fail("bad signing key: %v", err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		fail("signing key is not Ed25519")
	}
	data, err := os.ReadFile(*inst)
	if err != nil {
		fail("can't read installer: %v", err)
	}
	sum := sha256.Sum256(data)
	var m manifest
	m.Format, m.App, m.Version, m.Notes = 1, "Optic", *ver, notesFor(*ver)
	m.Installer.Name, m.Installer.Size, m.Installer.SHA256 = filepath.Base(*inst), int64(len(data)), hex.EncodeToString(sum[:])
	body, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail("%v", err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	if err := os.WriteFile(filepath.Join(*outDir, "update.json"), body, 0o644); err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "update.json.sig"), []byte(sig+"\n"), 0o644); err != nil {
		fail("%v", err)
	}
	fmt.Println("wrote", filepath.Join(*outDir, "update.json"), "and update.json.sig for", m.Installer.Name)
}
