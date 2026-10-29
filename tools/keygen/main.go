// Command keygen makes a new Ed25519 key pair for signing Optic updates.
//
//	go run ./tools/keygen optic-update-signing-key.pem
//
// It writes the PRIVATE key to the file you name (refusing to overwrite one) and
// prints the PUBLIC key to paste into update_windows.go (updatePublicKey).
// Keep the private key secret: store it as the GitHub secret UPDATE_SIGNING_KEY
// and keep an offline backup. Anyone holding it can sign updates.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: keygen <private-key-file.pem>")
		os.Exit(2)
	}
	out := os.Args[1]
	if _, err := os.Stat(out); err == nil {
		fmt.Fprintln(os.Stderr, out+" already exists; refusing to overwrite a signing key")
		os.Exit(1)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		panic(err)
	}
	fmt.Println("Private key written to", out)
	fmt.Println("Public key (put in update_windows.go, updatePublicKey):")
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
}
