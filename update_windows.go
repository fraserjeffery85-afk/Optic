//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UpdateState is what the page shows in the update bar.
type UpdateState struct {
	Status     string  `json:"status"` // disabled idle checking uptodate available downloading ready error
	Current    string  `json:"current"`
	Version    string  `json:"version"`
	Notes      string  `json:"notes"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"` // 0..1
	Error      string  `json:"error"`
	ReleaseURL string  `json:"releaseUrl"`
	Manual     bool    `json:"manual"` // the person asked (show "up to date" etc.)
}

type Updater struct {
	a            *App
	mu           sync.Mutex
	st           UpdateState
	man          *updateManifest
	installerURL string
	file         string
	busy         bool
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func newUpdater(a *App) *Updater {
	u := &Updater{a: a, st: UpdateState{Status: "idle", Current: appVersion}}
	if updateRepo == "" {
		u.st.Status = "disabled"
	}
	return u
}

func (u *Updater) State() UpdateState { u.mu.Lock(); defer u.mu.Unlock(); return u.st }

func (u *Updater) set(f func(*UpdateState)) {
	u.mu.Lock()
	f(&u.st)
	st := u.st
	u.mu.Unlock()
	b, _ := json.Marshal(st)
	u.a.w.Dispatch(func() { u.a.w.Eval("window.onUpdate && window.onUpdate(" + string(b) + ")") })
}

func get(url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Optic/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.New("couldn't reach GitHub - check your internet connection")
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, errors.New("no published release was found")
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub replied %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, errors.New("the download was interrupted")
	}
	if int64(len(b)) > limit {
		return nil, errors.New("a release file is larger than expected")
	}
	return b, nil
}

// Check looks for a newer release. Manual checks also report "up to date" and
// show skipped versions.
func (u *Updater) Check(manual bool) {
	if updateRepo == "" {
		if manual {
			u.set(func(s *UpdateState) {
				s.Status, s.Manual = "disabled", true
				s.Error = "Update checks only work in copies installed from the official Optic release."
			})
		}
		return
	}
	u.mu.Lock()
	if u.busy || u.st.Status == "downloading" || u.st.Status == "ready" {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()
	u.set(func(s *UpdateState) { s.Status, s.Error, s.Manual = "checking", "", manual })

	fail := func(err error) {
		logf("update check: %v", err)
		u.set(func(s *UpdateState) {
			if manual {
				s.Status, s.Error = "error", err.Error()
			} else {
				s.Status = "idle" // a silent check never nags
			}
		})
	}
	body, err := get("https://api.github.com/repos/"+updateRepo+"/releases/latest", 1<<20)
	if err != nil {
		fail(err)
		return
	}
	var rel struct {
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.Unmarshal(body, &rel) != nil {
		fail(errors.New("GitHub's reply was unreadable"))
		return
	}
	asset := func(n string) string {
		for _, a := range rel.Assets {
			if a.Name == n {
				return a.URL
			}
		}
		return ""
	}
	mURL, sURL := asset("update.json"), asset("update.json.sig")
	if mURL == "" || sURL == "" {
		fail(errors.New("the latest release has no update information"))
		return
	}
	mBytes, err := get(mURL, 1<<20)
	if err != nil {
		fail(err)
		return
	}
	sig, err := get(sURL, 4096)
	if err != nil {
		fail(err)
		return
	}
	m, err := verifyManifest(mBytes, string(sig), updatePublicKey)
	if err != nil {
		fail(err)
		return
	}
	cfgMu.Lock()
	skipped := u.a.cfg.SkipVersion == m.Version
	cfgMu.Unlock()
	if compareVersions(m.Version, appVersion) <= 0 || (skipped && !manual) {
		u.set(func(s *UpdateState) {
			s.Status = map[bool]string{true: "uptodate", false: "idle"}[manual]
		})
		return
	}
	iURL := asset(m.Installer.Name)
	if iURL == "" {
		fail(errors.New("the release is missing its installer"))
		return
	}
	u.mu.Lock()
	u.man, u.installerURL = m, iURL
	u.mu.Unlock()
	logf("update available: %s", m.Version)
	u.set(func(s *UpdateState) {
		s.Status, s.Version, s.Notes, s.Size, s.Progress = "available", m.Version, m.Notes, m.Installer.Size, 0
		s.ReleaseURL = rel.HTMLURL
	})
}

// Download fetches the installer with progress and checks it against the
// signed checksum.
func (u *Updater) Download() {
	u.mu.Lock()
	m, url := u.man, u.installerURL
	if m == nil || u.st.Status == "downloading" {
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	u.set(func(s *UpdateState) { s.Status, s.Progress, s.Error = "downloading", 0, "" })
	fail := func(err error) {
		logf("update download: %v", err)
		u.set(func(s *UpdateState) { s.Status, s.Error = "error", err.Error() })
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Optic/"+appVersion)
	client := &http.Client{Timeout: 15 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		fail(errors.New("couldn't reach GitHub - check your internet connection"))
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail(fmt.Errorf("GitHub replied %d", res.StatusCode))
		return
	}
	buf := make([]byte, 0, m.Installer.Size)
	chunk := make([]byte, 64<<10)
	last := time.Now()
	for {
		n, rerr := res.Body.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if int64(len(buf)) > m.Installer.Size {
			fail(errors.New("the download is larger than expected, so it wasn't installed"))
			return
		}
		if time.Since(last) > 150*time.Millisecond {
			last = time.Now()
			p := float64(len(buf)) / float64(m.Installer.Size)
			u.set(func(s *UpdateState) { s.Progress = p })
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			fail(errors.New("the download was interrupted - try again"))
			return
		}
	}
	if err := checkInstaller(buf, m); err != nil {
		fail(err)
		return
	}
	dir := filepath.Join(configDir(), "updates")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, m.Installer.Name)
	if err := os.WriteFile(file, buf, 0o644); err != nil {
		fail(errors.New("couldn't save the update: " + err.Error()))
		return
	}
	u.mu.Lock()
	u.file = file
	u.mu.Unlock()
	logf("update %s downloaded and verified", m.Version)
	u.set(func(s *UpdateState) { s.Status, s.Progress = "ready", 1 })
}

// Install runs the verified installer silently; it closes Optic, updates it and
// opens the new version.
func (u *Updater) Install() error {
	u.mu.Lock()
	file, m := u.file, u.man
	u.mu.Unlock()
	if file == "" || m == nil {
		return errors.New("download the update first")
	}
	data, err := os.ReadFile(file) // re-check right before running it
	if err != nil {
		return err
	}
	if err := checkInstaller(data, m); err != nil {
		return err
	}
	if err := shellOpen("open", file, "/S /UPDATE", ""); err != nil {
		return errors.New("couldn't start the installer: " + err.Error())
	}
	logf("running installer for %s", m.Version)
	go func() { time.Sleep(400 * time.Millisecond); u.a.w.Dispatch(u.a.exit) }()
	return nil
}

func (u *Updater) Skip() {
	u.mu.Lock()
	v := u.st.Version
	u.mu.Unlock()
	cfgMu.Lock()
	u.a.cfg.SkipVersion = v
	cfgMu.Unlock()
	u.a.markDirty()
	u.set(func(s *UpdateState) { s.Status = "idle" })
}

// cleanOldUpdates removes downloaded installers once they're no longer needed.
func cleanOldUpdates() {
	dir := filepath.Join(configDir(), "updates")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
}
