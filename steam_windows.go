//go:build windows

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// steamLibraries returns every Steam library folder on this PC.
func steamLibraries() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	sp, _, err := k.GetStringValue("SteamPath")
	if err != nil || sp == "" {
		return nil
	}
	sp = filepath.Clean(strings.ReplaceAll(sp, "/", `\`))
	libs := []string{sp}
	b, err := os.ReadFile(filepath.Join(sp, "steamapps", "libraryfolders.vdf"))
	if err == nil {
		re := regexp.MustCompile(`"path"\s+"([^"]+)"`)
		for _, m := range re.FindAllSubmatch(b, -1) {
			p := filepath.Clean(strings.ReplaceAll(string(m[1]), `\\`, `\`))
			dup := false
			for _, l := range libs {
				if strings.EqualFold(l, p) {
					dup = true
				}
			}
			if !dup {
				libs = append(libs, p)
			}
		}
	}
	return libs
}

var (
	reInstallDir = regexp.MustCompile(`"installdir"\s+"([^"]+)"`)
	reAppID      = regexp.MustCompile(`"appid"\s+"(\d+)"`)
)

// findSteamGameByExe looks through Steam libraries for a game containing exe.
func findSteamGameByExe(exe string) (folder, appID string) {
	for _, lib := range steamLibraries() {
		manifests, _ := filepath.Glob(filepath.Join(lib, "steamapps", "appmanifest_*.acf"))
		for _, m := range manifests {
			b, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			id := reAppID.FindSubmatch(b)
			dir := reInstallDir.FindSubmatch(b)
			if id == nil || dir == nil {
				continue
			}
			f := filepath.Join(lib, "steamapps", "common", string(dir[1]))
			if _, err := os.Stat(filepath.Join(f, exe)); err == nil {
				return f, string(id[1])
			}
		}
	}
	return "", ""
}

// steamAppIDForFolder finds the app id when the user picked a game exe manually.
func steamAppIDForFolder(folder string) string {
	common := filepath.Dir(folder)
	if !strings.EqualFold(filepath.Base(common), "common") {
		return ""
	}
	steamapps := filepath.Dir(common)
	manifests, _ := filepath.Glob(filepath.Join(steamapps, "appmanifest_*.acf"))
	for _, m := range manifests {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		dir := reInstallDir.FindSubmatch(b)
		id := reAppID.FindSubmatch(b)
		if dir != nil && id != nil && strings.EqualFold(string(dir[1]), filepath.Base(folder)) {
			return string(id[1])
		}
	}
	return ""
}

/* --------------------------- ReShade switch ---------------------------- */

var reshadeDLLs = []string{"opengl32.dll", "dxgi.dll", "d3d9.dll", "d3d10.dll", "d3d11.dll", "d3d12.dll", "dinput8.dll"}

type ReShadeState struct {
	Installed bool   `json:"installed"`
	On        bool   `json:"on"`
	DLL       string `json:"dll"` // file name, e.g. opengl32.dll
}

type rsCacheEntry struct {
	size int64
	mod  int64
	isRS bool
}

var (
	rsCacheMu sync.Mutex
	rsCache   = map[string]rsCacheEntry{}
)

// isReShadeFile checks the DLL really is ReShade (cached by size+date, since
// it is asked about several times a second).
func isReShadeFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	rsCacheMu.Lock()
	e, ok := rsCache[path]
	rsCacheMu.Unlock()
	if ok && e.size == fi.Size() && e.mod == fi.ModTime().UnixNano() {
		return e.isRS
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	is := bytes.Contains(b, []byte("crosire")) || bytes.Contains(b, []byte("ReShade"))
	rsCacheMu.Lock()
	rsCache[path] = rsCacheEntry{fi.Size(), fi.ModTime().UnixNano(), is}
	rsCacheMu.Unlock()
	return is
}

func reshadeState(folder string) ReShadeState {
	if folder == "" {
		return ReShadeState{}
	}
	if _, err := os.Stat(filepath.Join(folder, "ReShade.ini")); err != nil {
		return ReShadeState{}
	}
	for _, d := range reshadeDLLs {
		p := filepath.Join(folder, d)
		if _, err := os.Stat(p); err == nil && isReShadeFile(p) {
			return ReShadeState{Installed: true, On: true, DLL: d}
		}
		if _, err := os.Stat(p + ".off"); err == nil {
			return ReShadeState{Installed: true, On: false, DLL: d}
		}
	}
	return ReShadeState{}
}

// setReShade switches ReShade on/off by renaming its DLL. Presets, shaders and
// settings are never touched.
func setReShade(folder string, on bool) error {
	st := reshadeState(folder)
	if !st.Installed {
		return errors.New("ReShade isn't installed in this game's folder")
	}
	if st.On == on {
		return nil
	}
	p := filepath.Join(folder, st.DLL)
	var err error
	if on {
		err = os.Rename(p+".off", p)
	} else {
		err = os.Rename(p, p+".off")
	}
	if err != nil {
		return errors.New("couldn't rename " + st.DLL + ". Make sure the game is closed. (" + err.Error() + ")")
	}
	return nil
}

func launchGame(g Game) error {
	if g.AppID != "" {
		if shellOpen("open", "steam://rungameid/"+g.AppID, "", "") == nil {
			return nil
		}
	}
	if g.Folder != "" {
		exe := filepath.Join(g.Folder, g.Exe)
		if _, err := os.Stat(exe); err == nil {
			if err := shellOpen("open", exe, "", g.Folder); err != nil {
				return errors.New("couldn't start " + g.Name + " (" + err.Error() + "). Start it from Steam instead - the ReShade switch has already been done")
			}
			return nil
		}
	}
	return errors.New("couldn't start " + g.Name + ". Start it from Steam instead - the ReShade switch has already been done")
}

/* --------------------------- Windows settings --------------------------- */

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setStartWithWindows(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_ = k.DeleteValue(legacyAppName)
	if !on {
		err := k.DeleteValue(appName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(appName, `"`+exe+`" --minimized`)
}

const icmKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ICM`

func fullRangeUnlocked() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, icmKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("GdiIcmGammaRange")
	return err == nil && v >= 256
}

// unlockFullRange asks for admin once and lets Windows accept stronger curves.
func unlockFullRange() error {
	return shellOpen("runas", "reg.exe",
		`add "HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ICM" /v GdiIcmGammaRange /t REG_DWORD /d 256 /f`, "")
}

// removeLegacyStartup deletes the old ClearRide "start with Windows" entry.
func removeLegacyStartup() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if k.DeleteValue(legacyAppName) == nil {
		logf("removed old ClearRide startup entry")
	}
}
