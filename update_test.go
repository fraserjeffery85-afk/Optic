package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func signed(t *testing.T, mutate func(*updateManifest)) ([]byte, string, string, []byte) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	inst := []byte("pretend installer bytes")
	sum := sha256.Sum256(inst)
	var m updateManifest
	m.Format, m.App, m.Version, m.Notes = 1, "Optic", "1.2.0", "notes"
	m.Installer.Name, m.Installer.Size, m.Installer.SHA256 = "Optic-Setup-1.2.0.exe", int64(len(inst)), hex.EncodeToString(sum[:])
	if mutate != nil {
		mutate(&m)
	}
	body, _ := json.Marshal(m)
	return body, base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body)), base64.StdEncoding.EncodeToString(pub), inst
}

func TestManifestOK(t *testing.T) {
	body, sig, pub, inst := signed(t, nil)
	m, err := verifyManifest(body, sig+"\n", pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkInstaller(inst, m); err != nil {
		t.Fatal(err)
	}
	if checkInstaller(append(inst, 'x'), m) == nil {
		t.Fatal("tampered installer accepted")
	}
}

func TestManifestRejects(t *testing.T) {
	body, sig, pub, _ := signed(t, nil)
	tampered := append([]byte{}, body...)
	tampered[len(tampered)-3] ^= 1
	if _, err := verifyManifest(tampered, sig, pub); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	_, _, otherPub, _ := signed(t, nil)
	if _, err := verifyManifest(body, sig, otherPub); err == nil {
		t.Fatal("wrong key accepted")
	}
	for name, f := range map[string]func(*updateManifest){
		"name":     func(m *updateManifest) { m.Installer.Name = "evil.exe" },
		"mismatch": func(m *updateManifest) { m.Installer.Name = "Optic-Setup-9.9.9.exe" },
		"app":      func(m *updateManifest) { m.App = "Other" },
		"version":  func(m *updateManifest) { m.Version = "1.2" },
		"sha":      func(m *updateManifest) { m.Installer.SHA256 = "abc" },
	} {
		b, s, p, _ := signed(t, f)
		if _, err := verifyManifest(b, s, p); err == nil {
			t.Fatalf("%s: bad manifest accepted", name)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("1.1.0", "1.0.3") != 1 || compareVersions("1.0.3", "1.0.10") != -1 || compareVersions("2.0.0", "2.0.0") != 0 {
		t.Fatal("compare broken")
	}
}

func TestPublicKeyValid(t *testing.T) {
	if b, err := base64.StdEncoding.DecodeString(updatePublicKey); err != nil || len(b) != ed25519.PublicKeySize {
		t.Fatal("updatePublicKey is not a valid Ed25519 public key")
	}
}
