//go:build !windows

package main

import "fmt"

// Optic only runs on Windows. This stub lets `go build ./...` and the tests work
// on Linux/macOS, where the release is cross-compiled (see scripts/build.sh).
func main() {
	fmt.Println("Optic is a Windows app. Build it with scripts/build.sh (GOOS=windows).")
}
