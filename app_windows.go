//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

//go:embed ui/index.html
var indexHTML string

type App struct {
	cfg  *Config
	eng  *Engine
	w    webview2.WebView
	hwnd uintptr
	tray *Tray
	upd  *Updater

	mu      sync.Mutex
	preview bool
	dirty   bool
	dirtyAt time.Time
	exiting bool
	ourExe  string
	running map[string]bool
}

/* ------------------------------- startup -------------------------------- */

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--quit" { // used by the installer: ask a running Optic to close properly
			signalQuit()
			return
		}
	}
	if !singleInstance() {
		if h := findOurWindow(); h != 0 {
			bringToFront(h)
		} else {
			messageBox(appName+" is already running. Look for its icon in the system tray, near the clock.", appName, 0x40)
		}
		return
	}
	exe, _ := os.Executable()
	a := &App{cfg: loadConfig(), ourExe: strings.ToLower(filepath.Base(exe)), running: map[string]bool{}}
	var uiReady atomic.Bool
	go waitForQuit(func() { // the installer is updating or removing Optic
		if uiReady.Load() {
			a.w.Dispatch(a.exit)
			return
		}
		if a.eng != nil {
			a.eng.Restore()
		}
		logf("closed for the installer")
		os.Exit(0)
	})
	legacyWasRunning := closeLegacyClearRide()
	a.eng = NewEngine()
	if legacyWasRunning {
		a.eng.recoverFrom(filepath.Join(filepath.Dir(configDir()), legacyAppName, "restore.json"))
	}
	removeLegacyStartup()
	logf("%s %s started; displays=%v; saturation=%q; fullRange=%v", appName, appVersion, listDisplays(), a.eng.SaturationName(), fullRangeUnlocked())
	a.autodetectGames()
	if a.cfg.StartWithWindows { // keep the startup entry pointing at this exe (also replaces the old ClearRide one)
		setStartWithWindows(true)
	}

	startHidden := a.cfg.StartMinimized
	for _, arg := range os.Args[1:] {
		if arg == "--minimized" {
			startHidden = true
		}
	}

	dpi := systemDPI()
	a.w = webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(configDir(), "webview"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: appName, Width: uint(1340 * dpi / 96), Height: uint(940 * dpi / 96), IconId: 1, Center: true,
		},
	})
	if a.w == nil {
		logf("WebView2 runtime missing")
		if messageBox(appName+" needs the Microsoft Edge WebView2 Runtime (a free Microsoft component that most PCs already have).\n\nOpen the download page now?",
			appName, 0x4|0x30) == 6 {
			shellOpen("open", "https://go.microsoft.com/fwlink/p/?LinkId=2124703", "", "")
		}
		return
	}
	a.hwnd = uintptr(a.w.Window())
	centerWindow(a.hwnd, 1340*dpi/96, 940*dpi/96)
	a.w.SetSize(900*dpi/96, 640*dpi/96, webview2.HintMin)
	if startHidden {
		showWindow(a.hwnd, swHide)
	}
	subclassClose(a.hwnd, func() bool {
		if a.exiting {
			return true
		}
		a.setPreview(false)
		showWindow(a.hwnd, swHide)
		a.tray.Notify(appName, "Still running here in the tray. Right-click the icon to exit.")
		return false
	})

	a.tray = NewTray(appName)
	a.tray.OnOpen = a.show
	a.tray.OnMenu = func() []TrayItem {
		cfgMu.Lock()
		on := a.cfg.Enabled
		cfgMu.Unlock()
		return []TrayItem{
			{Text: "Open " + appName, Action: a.show},
			{},
			{Text: "Optic on", Checked: on, Action: func() { a.toggleEnabled(); a.notifyUI() }},
			{Text: "Reset my screen", Action: func() { a.emergency(); a.notifyUI() }},
			{},
			{Text: "Exit", Action: a.exit},
		}
	}

	a.bind()
	a.w.SetHtml(indexHTML)

	a.upd = newUpdater(a)
	cleanOldUpdates()
	go func() {
		cfgMu.Lock()
		off := a.cfg.NoUpdateCheck
		cfgMu.Unlock()
		if !off {
			time.Sleep(5 * time.Second)
			a.upd.Check(false)
		}
	}()
	go a.loop()
	go a.hotkeys()
	uiReady.Store(true)

	a.w.Run()
	a.shutdown()
}

func (a *App) show() { bringToFront(a.hwnd) }

func (a *App) exit() {
	a.exiting = true
	a.eng.Restore()
	a.tray.Remove()
	a.w.Terminate()
}

func (a *App) shutdown() {
	a.exiting = true
	a.eng.Restore()
	cfgMu.Lock()
	a.cfg.save()
	cfgMu.Unlock()
	if a.tray != nil {
		a.tray.Remove()
	}
	logf("exited cleanly")
}

func (a *App) autodetectGames() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	changed := false
	for i := range a.cfg.Games {
		g := &a.cfg.Games[i]
		if g.Folder == "" {
			if f, id := findSteamGameByExe(g.Exe); f != "" {
				g.Folder, g.AppID = f, id
				changed = true
			}
		}
	}
	if changed {
		a.cfg.save()
	}
}

func (a *App) markDirty() {
	a.mu.Lock()
	a.dirty, a.dirtyAt = true, time.Now()
	a.mu.Unlock()
}

// notifyUI asks the page to reload state (after hotkeys/tray changed something).
func (a *App) notifyUI() {
	a.w.Dispatch(func() { a.w.Eval("window.onStateChanged && window.onStateChanged()") })
}

/* --------------------------- state for the page -------------------------- */

type uiLook struct {
	Name     string   `json:"name"`
	BuiltIn  bool     `json:"builtIn"`
	Settings Settings `json:"settings"`
	Saved    Settings `json:"saved"`
	Dirty    bool     `json:"dirty"`
}

type uiGame struct {
	Name    string       `json:"name"`
	Exe     string       `json:"exe"`
	Folder  string       `json:"folder"`
	Preset  string       `json:"preset"`
	Running bool         `json:"running"`
	Reshade ReShadeState `json:"reshade"`
	Setup   string       `json:"setup"`
	Pending string       `json:"pending"`
}

type uiState struct {
	Version          string   `json:"version"`
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	Monitors         string   `json:"monitors"`
	SkipReShade      bool     `json:"skipReShade"`
	StartWithWindows bool     `json:"startWithWindows"`
	StartMinimized   bool     `json:"startMinimized"`
	UIScale          int      `json:"uiScale"`
	PreviewDesktop   bool     `json:"previewDesktop"`
	SatName          string   `json:"satName"`
	FullRange        bool     `json:"fullRange"`
	Editor           string   `json:"editor"`
	SelectedGame     int      `json:"selectedGame"`
	LastSeenVersion  string   `json:"lastSeenVersion"`
	CheckUpdates     bool     `json:"checkUpdates"`
	UpdatesEnabled   bool     `json:"updatesEnabled"`
	Fresh            bool     `json:"fresh"`
	Looks            []uiLook `json:"looks"`
	Games            []uiGame `json:"games"`
}

func (a *App) state() uiState {
	a.mu.Lock()
	preview := a.preview
	running := a.running
	a.mu.Unlock()
	cfgMu.Lock()
	defer cfgMu.Unlock()
	c := a.cfg
	if c.findPreset(c.EditorPreset) < 0 {
		c.EditorPreset = c.Presets[0].Name
	}
	s := uiState{
		Version: appVersion, Enabled: c.Enabled, Mode: c.Mode, Monitors: c.Monitors, SkipReShade: c.SkipWhenReShadeOn,
		StartWithWindows: c.StartWithWindows, StartMinimized: c.StartMinimized, UIScale: c.UIScale,
		PreviewDesktop: preview, SatName: a.eng.SaturationName(), FullRange: fullRangeUnlocked(),
		Editor: c.EditorPreset, SelectedGame: c.SelectedGame,
		LastSeenVersion: c.LastSeenVersion, Fresh: c.Fresh,
		CheckUpdates: !c.NoUpdateCheck, UpdatesEnabled: updateRepo != "",
	}
	for _, p := range c.Presets {
		saved := p.Settings
		if p.Saved != nil {
			saved = *p.Saved
		}
		s.Looks = append(s.Looks, uiLook{p.Name, p.BuiltIn, p.Settings, saved, p.Dirty()})
	}
	for _, g := range c.Games {
		s.Games = append(s.Games, uiGame{g.Name, g.Exe, g.Folder, g.Preset, running[g.Exe], reshadeState(g.Folder), g.Setup, g.Pending})
	}
	if s.Looks == nil {
		s.Looks = []uiLook{}
	}
	if s.Games == nil {
		s.Games = []uiGame{}
	}
	return s
}

/* ------------------------------ page API -------------------------------- */

func (a *App) bind() {
	b := func(name string, f interface{}) { a.w.Bind("api_"+name, f) }

	b("state", func() uiState { return a.state() })
	b("changelog", func() []ChangeEntry { return parseChangelog(changelogMD) })

	b("selectLook", func(name string) uiState {
		cfgMu.Lock()
		if a.cfg.findPreset(name) >= 0 {
			a.cfg.EditorPreset = name
			if g := a.cfg.SelectedGame; g >= 0 && g < len(a.cfg.Games) {
				a.cfg.Games[g].Preset = name
			}
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("saveLook", func(name, settingsJSON string) bool {
		var s Settings
		if json.Unmarshal([]byte(settingsJSON), &s) != nil {
			return false
		}
		cfgMu.Lock()
		if i := a.cfg.findPreset(name); i >= 0 {
			a.cfg.Presets[i].Settings = s.Sanitize()
		}
		cfgMu.Unlock()
		a.markDirty()
		return true
	})

	b("newLook", func() uiState {
		cfgMu.Lock()
		base := a.cfg.presetSettings(a.cfg.EditorPreset)
		name := a.cfg.uniqueName("My look")
		np := Preset{Name: name, Settings: base}
		np.MarkSaved()
		// unsaved changes move into the new look; the original goes back to its saved version
		if oi := a.cfg.findPreset(a.cfg.EditorPreset); oi >= 0 && a.cfg.Presets[oi].Saved != nil {
			a.cfg.Presets[oi].Settings = *a.cfg.Presets[oi].Saved
		}
		a.cfg.Presets = append(a.cfg.Presets, np)
		a.cfg.EditorPreset = name
		if g := a.cfg.SelectedGame; g >= 0 && g < len(a.cfg.Games) {
			a.cfg.Games[g].Preset = name
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("renameLook", func(old, nw string) (uiState, error) {
		nw = strings.TrimSpace(nw)
		if nw == "" {
			return a.state(), errors.New("please type a name")
		}
		cfgMu.Lock()
		i := a.cfg.findPreset(old)
		switch {
		case i < 0:
			cfgMu.Unlock()
			return a.state(), errors.New("look not found")
		case a.cfg.Presets[i].BuiltIn:
			cfgMu.Unlock()
			return a.state(), errors.New("built-in looks can't be renamed - use Make your own")
		case !strings.EqualFold(old, nw) && a.cfg.findPreset(nw) >= 0:
			cfgMu.Unlock()
			return a.state(), errors.New("there's already a look called " + nw)
		}
		a.cfg.Presets[i].Name = nw
		for gi := range a.cfg.Games {
			if a.cfg.Games[gi].Preset == old {
				a.cfg.Games[gi].Preset = nw
			}
		}
		if a.cfg.EditorPreset == old {
			a.cfg.EditorPreset = nw
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state(), nil
	})

	b("deleteLook", func(name string) uiState {
		cfgMu.Lock()
		if i := a.cfg.findPreset(name); i >= 0 && !a.cfg.Presets[i].BuiltIn {
			a.cfg.Presets = append(a.cfg.Presets[:i], a.cfg.Presets[i+1:]...)
			fallback := a.cfg.Presets[0].Name
			for gi := range a.cfg.Games {
				if a.cfg.Games[gi].Preset == name {
					a.cfg.Games[gi].Preset = fallback
				}
			}
			if a.cfg.EditorPreset == name {
				a.cfg.EditorPreset = fallback
			}
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("resetLook", func(name string) uiState {
		cfgMu.Lock()
		if i := a.cfg.findPreset(name); i >= 0 {
			s := NeutralSettings()
			for _, bp := range BuiltInPresets() {
				if bp.Name == a.cfg.Presets[i].Name {
					s = bp.Settings
				}
			}
			a.cfg.Presets[i].Settings = s
			a.cfg.Presets[i].MarkSaved()
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	// Save: the current settings become the look's saved version
	b("commitLook", func(name string) uiState {
		cfgMu.Lock()
		if i := a.cfg.findPreset(name); i >= 0 {
			a.cfg.Presets[i].MarkSaved()
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	// Undo changes: back to the last saved version
	b("revertLook", func(name string) uiState {
		cfgMu.Lock()
		if i := a.cfg.findPreset(name); i >= 0 && a.cfg.Presets[i].Saved != nil {
			a.cfg.Presets[i].Settings = *a.cfg.Presets[i].Saved
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	// Save as new look: the changes go into a new look; the original keeps its saved version
	b("saveAsNewLook", func(name, newName string) (uiState, error) {
		newName = strings.TrimSpace(newName)
		if newName == "" {
			return a.state(), errors.New("please type a name")
		}
		cfgMu.Lock()
		i := a.cfg.findPreset(name)
		if i < 0 {
			cfgMu.Unlock()
			return a.state(), errors.New("look not found")
		}
		if a.cfg.findPreset(newName) >= 0 {
			cfgMu.Unlock()
			return a.state(), errors.New("there's already a look called " + newName)
		}
		np := Preset{Name: newName, Settings: a.cfg.Presets[i].Settings}
		np.MarkSaved()
		if a.cfg.Presets[i].Saved != nil {
			a.cfg.Presets[i].Settings = *a.cfg.Presets[i].Saved
		}
		a.cfg.Presets = append(a.cfg.Presets, np)
		a.cfg.EditorPreset = newName
		if g := a.cfg.SelectedGame; g >= 0 && g < len(a.cfg.Games) {
			a.cfg.Games[g].Preset = newName
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state(), nil
	})

	b("exit", func() { a.w.Dispatch(a.exit) })
	b("updateState", func() UpdateState { return a.upd.State() })
	b("updateCheck", func() { go a.upd.Check(true) })
	b("updateDownload", func() { go a.upd.Download() })
	b("updateInstall", func() error { return a.upd.Install() })
	b("updateSkip", func() { a.upd.Skip() })

	// returns the saved path, or "" if cancelled
	b("exportLook", func(name string) (string, error) {
		cfgMu.Lock()
		i := a.cfg.findPreset(name)
		if i < 0 {
			cfgMu.Unlock()
			return "", errors.New("look not found")
		}
		text := exportPresetJSON(a.cfg.Presets[i])
		cfgMu.Unlock()
		safe := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`\/:*?"<>|`, r) {
				return '_'
			}
			return r
		}, name)
		path, ok := saveFileDialog(a.hwnd, "Save this look to share", safe+".optic.json",
			[][2]string{{"Optic look (*.json)", "*.json"}}, "json")
		if !ok {
			return "", nil
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return "", err
		}
		return path, nil
	})

	b("importLook", func() (uiState, error) {
		path, ok := openFileDialog(a.hwnd, "Add a shared look", [][2]string{{"Optic look (*.json)", "*.json"}, {"All files", "*.*"}}, "")
		if !ok {
			return a.state(), nil
		}
		p, err := importPreset(path)
		if err != nil {
			return a.state(), err
		}
		cfgMu.Lock()
		p.Name = a.cfg.uniqueName(p.Name)
		a.cfg.Presets = append(a.cfg.Presets, p)
		a.cfg.EditorPreset = p.Name
		cfgMu.Unlock()
		a.markDirty()
		return a.state(), nil
	})

	b("selectGame", func(i int) uiState {
		cfgMu.Lock()
		if i >= 0 && i < len(a.cfg.Games) {
			a.cfg.SelectedGame = i
			if a.cfg.findPreset(a.cfg.Games[i].Preset) >= 0 {
				a.cfg.EditorPreset = a.cfg.Games[i].Preset
			}
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("addGame", func() (uiState, error) {
		path, ok := openFileDialog(a.hwnd, "Pick the game's program (.exe)", [][2]string{{"Programs (*.exe)", "*.exe"}}, "")
		if !ok {
			return a.state(), nil
		}
		folder := filepath.Dir(path)
		g := Game{Name: filepath.Base(folder), Exe: strings.ToLower(filepath.Base(path)), Folder: folder, AppID: steamAppIDForFolder(folder)}
		cfgMu.Lock()
		for i, eg := range a.cfg.Games {
			if eg.Exe == g.Exe {
				a.cfg.Games[i].Folder, a.cfg.Games[i].AppID = g.Folder, g.AppID
				a.cfg.SelectedGame = i
				cfgMu.Unlock()
				a.markDirty()
				return a.state(), nil
			}
		}
		g.Preset = a.cfg.EditorPreset
		a.cfg.Games = append(a.cfg.Games, g)
		a.cfg.SelectedGame = len(a.cfg.Games) - 1
		cfgMu.Unlock()
		a.markDirty()
		return a.state(), nil
	})

	b("changeGameFolder", func(i int) (uiState, error) {
		cfgMu.Lock()
		if i < 0 || i >= len(a.cfg.Games) {
			cfgMu.Unlock()
			return a.state(), errors.New("add a game first")
		}
		g := a.cfg.Games[i]
		cfgMu.Unlock()
		path, ok := openFileDialog(a.hwnd, "Find "+g.Name+" ("+g.Exe+")", [][2]string{{g.Exe, g.Exe}, {"Programs (*.exe)", "*.exe"}}, g.Folder)
		if !ok {
			return a.state(), nil
		}
		folder := filepath.Dir(path)
		cfgMu.Lock()
		a.cfg.Games[i].Folder = folder
		a.cfg.Games[i].Exe = strings.ToLower(filepath.Base(path))
		a.cfg.Games[i].AppID = steamAppIDForFolder(folder)
		cfgMu.Unlock()
		a.markDirty()
		return a.state(), nil
	})

	b("removeGame", func(i int) uiState {
		cfgMu.Lock()
		if i >= 0 && i < len(a.cfg.Games) {
			a.cfg.Games = append(a.cfg.Games[:i], a.cfg.Games[i+1:]...)
			if a.cfg.SelectedGame >= len(a.cfg.Games) {
				a.cfg.SelectedGame = len(a.cfg.Games) - 1
			}
			if a.cfg.SelectedGame < 0 {
				a.cfg.SelectedGame = 0
			}
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("launch", func(mode string) error { return a.launch(mode) })
	b("setSetup", func(i int, setup string) (uiState, error) {
		err := a.setSetup(i, setup)
		return a.state(), err
	})

	b("setOption", func(key string, value interface{}) uiState {
		cfgMu.Lock()
		switch key {
		case "enabled":
			a.cfg.Enabled, _ = value.(bool)
		case "uiScale":
			if f, ok := value.(float64); ok && f >= 0 && f <= 2 {
				a.cfg.UIScale = int(f)
			}
		case "mode":
			if v, _ := value.(string); v == "running" || v == "focused" {
				a.cfg.Mode = v
			}
		case "monitors":
			if v, _ := value.(string); v == "all" || v == "game" {
				a.cfg.Monitors = v
			}
		case "skipReShade":
			a.cfg.SkipWhenReShadeOn, _ = value.(bool)
		case "checkUpdates":
			on, _ := value.(bool)
			a.cfg.NoUpdateCheck = !on
		case "startMinimized":
			a.cfg.StartMinimized, _ = value.(bool)
		case "startWithWindows":
			on, _ := value.(bool)
			if err := setStartWithWindows(on); err == nil {
				a.cfg.StartWithWindows = on
			} else {
				logf("start with Windows: %v", err)
			}
		}
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})

	b("setPreview", func(on bool) uiState { a.setPreview(on); return a.state() })
	b("emergency", func() uiState { a.emergency(); return a.state() })
	b("markSeen", func() uiState {
		cfgMu.Lock()
		a.cfg.LastSeenVersion = appVersion
		a.cfg.Fresh = false
		cfgMu.Unlock()
		a.markDirty()
		return a.state()
	})
	b("unlockRange", func() error { return unlockFullRange() })

	b("screenshot", func() string {
		cfgMu.Lock()
		games := append([]Game(nil), a.cfg.Games...)
		cfgMu.Unlock()
		return latestScreenshot(games)
	})
	b("pickImage", func() string {
		path, ok := openFileDialog(a.hwnd, "Choose a picture for the preview",
			[][2]string{{"Pictures (*.png;*.jpg;*.jpeg)", "*.png;*.jpg;*.jpeg"}}, filepath.Join(documentsDir(), "PiBoSo", "MX Bikes", "scrshots"))
		if !ok {
			return ""
		}
		return imageDataURL(path)
	})
}

func (a *App) setPreview(on bool) {
	a.mu.Lock()
	a.preview = on
	a.mu.Unlock()
}

func (a *App) toggleEnabled() bool {
	cfgMu.Lock()
	a.cfg.Enabled = !a.cfg.Enabled
	on := a.cfg.Enabled
	cfgMu.Unlock()
	a.markDirty()
	return on
}

func (a *App) emergency() {
	cfgMu.Lock()
	a.cfg.Enabled = false
	cfgMu.Unlock()
	a.setPreview(false)
	a.eng.Restore()
	a.markDirty()
	if a.tray != nil {
		a.tray.Notify(appName, "Screen reset and Optic switched off. Ctrl+Alt+F9 turns it back on.")
	}
}

func (a *App) launch(mode string) error {
	cfgMu.Lock()
	i := a.cfg.SelectedGame
	if i < 0 || i >= len(a.cfg.Games) {
		cfgMu.Unlock()
		return errors.New("add a game first")
	}
	g := a.cfg.Games[i]
	cfgMu.Unlock()
	return launchGame(g)
}

// setSetup picks Race or Photo for a game. ReShade is switched straight away
// if the game is closed, otherwise as soon as the game closes.
func (a *App) setSetup(i int, setup string) error {
	if setup != "race" && setup != "photo" {
		return errors.New("unknown setup")
	}
	cfgMu.Lock()
	if i < 0 || i >= len(a.cfg.Games) {
		cfgMu.Unlock()
		return errors.New("add a game first")
	}
	g := a.cfg.Games[i]
	cfgMu.Unlock()
	want := setup == "photo"
	st := reshadeState(g.Folder)
	if !st.Installed && want {
		return errors.New("ReShade isn't installed for " + g.Name + ", so there's nothing to switch on")
	}
	pending := ""
	if st.Installed && st.On != want {
		if runningProcesses()[g.Exe] {
			pending = map[bool]string{true: "on", false: "off"}[want]
		} else if err := setReShade(g.Folder, want); err != nil {
			return err
		}
	}
	cfgMu.Lock()
	if i < len(a.cfg.Games) && a.cfg.Games[i].Exe == g.Exe {
		a.cfg.Games[i].Setup = setup
		a.cfg.Games[i].Pending = pending
	}
	cfgMu.Unlock()
	a.markDirty()
	logf("setup %s for %s (pending=%q)", setup, g.Name, pending)
	return nil
}

/* ---------------------------- background loop --------------------------- */

type uiStatus struct {
	Text   string   `json:"text"`
	Error  string   `json:"error"`
	Active bool     `json:"active"`
	Games  []uiGame `json:"games"`
}

func (a *App) loop() {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	var last string
	tick := 0
	var running map[string]bool
	var applied *Desired // what the engine was last told
	flips := 0           // consecutive ticks wanting the opposite on/off state
	for range t.C {
		if a.exiting {
			return
		}
		tick++
		if running == nil || tick%2 == 0 {
			running = runningProcesses()
			a.mu.Lock()
			a.running = running
			a.mu.Unlock()
		}
		fgExe, fgHwnd := foregroundProcess()

		cfgMu.Lock()
		c := *a.cfg
		games := append([]Game(nil), a.cfg.Games...)
		editor := a.cfg.presetSettings(a.cfg.EditorPreset)
		presets := map[string]Settings{}
		for _, p := range a.cfg.Presets {
			presets[p.Name] = p.Settings
		}
		cfgMu.Unlock()

		// a ReShade switch chosen while the game was open: do it now it has closed
		for gi, g := range games {
			if g.Pending == "" || running[g.Exe] {
				continue
			}
			if err := setReShade(g.Folder, g.Pending == "on"); err != nil {
				logf("pending ReShade switch for %s failed: %v", g.Name, err)
				continue
			}
			cfgMu.Lock()
			if gi < len(a.cfg.Games) && a.cfg.Games[gi].Exe == g.Exe {
				a.cfg.Games[gi].Pending = ""
			}
			cfgMu.Unlock()
			games[gi].Pending = ""
			a.markDirty()
			a.notifyUI()
			a.w.Dispatch(func() {
				a.tray.Notify(appName, "ReShade switched "+map[bool]string{true: "ON", false: "OFF"}[g.Pending == "on"]+" for "+g.Name+".")
			})
		}

		a.mu.Lock()
		preview := a.preview
		saveNow := a.dirty && time.Since(a.dirtyAt) > 800*time.Millisecond
		if saveNow {
			a.dirty = false
		}
		a.mu.Unlock()
		if saveNow {
			cfgMu.Lock()
			a.cfg.save()
			cfgMu.Unlock()
		}

		var d *Desired
		text := "Waiting for your game. Your screen is normal."
		if len(games) == 0 {
			text = "Add a game to get started."
		}
		if preview {
			d = &Desired{Settings: editor, Displays: listDisplays(), Label: "preview"}
			text = "Showing the look on your screen. Switch off 'Try it on my screen' when you're done."
		} else if !c.Enabled {
			text = "Optic is switched off."
		} else {
			for _, g := range games {
				var active bool
				if c.Mode == "running" {
					active = running[g.Exe]
				} else {
					// in the game, or in Optic while the game runs (to adjust)
					active = fgExe == g.Exe || (fgExe == a.ourExe && running[g.Exe])
				}
				if !active {
					if running[g.Exe] {
						text = g.Name + " is open. The look comes on when you click back into the game."
					}
					continue
				}
				if g.Setup == "photo" && reshadeState(g.Folder).On {
					text = g.Name + " is in Photo setup: Optic is paused so your ReShade presets look right."
					break
				}
				if c.SkipWhenReShadeOn && reshadeState(g.Folder).On {
					text = g.Name + ": ReShade is on, so Optic is paused (see Settings)."
					break
				}
				var displays []string
				if c.Monitors == "all" {
					displays = listDisplays()
				} else {
					hwnd := fgHwnd
					if fgExe != g.Exe {
						hwnd = mainWindowOf(g.Exe)
					}
					if m := monitorOfWindow(hwnd); hwnd != 0 && m != "" {
						displays = []string{m}
					} else {
						displays = listDisplays()
					}
				}
				d = &Desired{Settings: presets[g.Preset], Displays: displays, Label: g.Name}
				text = "On: " + g.Preset + " in " + g.Name
				break
			}
		}
		// focus changes (alt-tab, minimising a window) can briefly report no
		// window at all: only switch on/off once the new state holds for 2 ticks
		if (d == nil) != (applied == nil) && !preview {
			flips++
			if flips < 2 {
				d = applied
			} else {
				flips = 0
			}
		} else {
			flips = 0
		}
		applied = d
		if running[legacyExe] {
			text = "The old ClearRide is running too and will fight with Optic (screen flicker). Close ClearRide from its tray icon."
		}
		a.eng.Set(d)
		errText := ""
		if d != nil {
			errText = a.eng.Errors()
		}
		st := uiStatus{Text: text, Error: errText, Active: d != nil}
		for _, g := range games {
			st.Games = append(st.Games, uiGame{Name: g.Name, Exe: g.Exe, Running: running[g.Exe], Reshade: reshadeState(g.Folder), Setup: g.Setup, Pending: g.Pending})
		}
		b, _ := json.Marshal(st)
		if s := string(b); s != last {
			last = s
			a.w.Dispatch(func() { a.w.Eval("window.onStatus && window.onStatus(" + s + ")") })
			tip := text
			if len(tip) > 120 {
				tip = tip[:120]
			}
			a.w.Dispatch(func() { a.tray.SetTooltip(tip) })
		}
	}
}

/* -------------------------------- hotkeys ------------------------------- */

const (
	vkF5, vkF6, vkF7, vkF8, vkF9, vkF10, vkF12 = 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7B
)

func (a *App) onHotkey(vk int) {
	switch vk {
	case vkF9:
		on := a.toggleEnabled()
		a.w.Dispatch(func() { a.tray.Notify(appName, map[bool]string{true: "Optic ON", false: "Optic OFF"}[on]) })
	case vkF10:
		a.nextLook()
	case vkF12:
		a.w.Dispatch(a.emergency)
	case vkF5:
		a.adjust(func(s *Settings) { s.Shadows -= 10 })
	case vkF6:
		a.adjust(func(s *Settings) { s.Shadows += 10 })
	case vkF7:
		a.adjust(func(s *Settings) { s.Brightness -= 5 })
	case vkF8:
		a.adjust(func(s *Settings) { s.Brightness += 5 })
	}
	a.notifyUI()
}

// activeGameIndex: the game in focus, else the selected one.
func (a *App) activeGameIndex() int {
	fg, _ := foregroundProcess()
	for i, g := range a.cfg.Games {
		if g.Exe == fg {
			return i
		}
	}
	if a.cfg.SelectedGame < len(a.cfg.Games) {
		return a.cfg.SelectedGame
	}
	return -1
}

func (a *App) adjust(f func(*Settings)) {
	cfgMu.Lock()
	name := a.cfg.EditorPreset
	if gi := a.activeGameIndex(); gi >= 0 {
		name = a.cfg.Games[gi].Preset
	}
	if i := a.cfg.findPreset(name); i >= 0 {
		s := a.cfg.Presets[i].Settings
		f(&s)
		a.cfg.Presets[i].Settings = s.Sanitize()
	}
	cfgMu.Unlock()
	a.markDirty()
}

func (a *App) nextLook() {
	cfgMu.Lock()
	gi := a.activeGameIndex()
	if gi < 0 || len(a.cfg.Presets) == 0 {
		cfgMu.Unlock()
		return
	}
	cur := a.cfg.findPreset(a.cfg.Games[gi].Preset)
	next := a.cfg.Presets[(cur+1)%len(a.cfg.Presets)].Name
	a.cfg.Games[gi].Preset = next
	a.cfg.EditorPreset = next
	game := a.cfg.Games[gi].Name
	cfgMu.Unlock()
	a.markDirty()
	a.w.Dispatch(func() { a.tray.Notify(appName, game+": "+next) })
}

func (a *App) hotkeys() {
	keys := []int{vkF5, vkF6, vkF7, vkF8, vkF9, vkF10, vkF12}
	registered := registerHotkeys(keys, a.onHotkey)
	var poll []int
	for _, k := range keys {
		if !registered[k] {
			poll = append(poll, k)
		}
	}
	if len(poll) == 0 {
		return
	}
	const vkCtrl, vkAlt = 0x11, 0x12
	prev := map[int]bool{}
	t := time.NewTicker(25 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if a.exiting {
			return
		}
		mods := keyDown(vkCtrl) && keyDown(vkAlt)
		for _, k := range poll {
			down := mods && keyDown(k)
			if down && !prev[k] {
				a.onHotkey(k)
			}
			prev[k] = down
		}
	}
}
