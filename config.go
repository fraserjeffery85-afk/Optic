package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Game struct {
	Name    string `json:"name"`
	Exe     string `json:"exe"`     // lower-case file name, e.g. mxbikes.exe
	Folder  string `json:"folder"`  // install folder (for the ReShade switch)
	AppID   string `json:"appId"`   // Steam app id if known
	Preset  string `json:"preset"`  // preset name used for this game
	Setup   string `json:"setup"`   // "race" (ReShade off, Optic on) | "photo" (ReShade on, Optic paused) | ""
	Pending string `json:"pending"` // ReShade switch waiting for the game to close: "on" | "off" | ""
}

type Config struct {
	Version           int      `json:"version"`
	Presets           []Preset `json:"presets"`
	Games             []Game   `json:"games"`
	Enabled           bool     `json:"enabled"`
	Mode              string   `json:"mode"`     // "focused" | "running"
	Monitors          string   `json:"monitors"` // "game" | "all"
	SkipWhenReShadeOn bool     `json:"skipWhenReShadeOn"`
	StartWithWindows  bool     `json:"startWithWindows"`
	StartMinimized    bool     `json:"startMinimized"`
	EditorPreset      string   `json:"editorPreset"`
	UIScale           int      `json:"uiScale"`
	SelectedGame      int      `json:"selectedGame"`
	LastSeenVersion   string   `json:"lastSeenVersion"` // for the "What's new" pop-up
	SkipVersion       string   `json:"skipVersion"`     // "Skip this version" in the update bar
	NoUpdateCheck     bool     `json:"noUpdateCheck"`   // don't check for updates at start-up
	Fresh             bool     `json:"-"`               // first ever start on this PC
}

// renamed built-in looks from version 1
var presetRenames = map[string]string{
	"Neutral (off)":               "Off",
	"OB Gameplay (from ReShade)":  "ReShade Style (Levels + Clarity)",
	"Night / Supercross Stadium":  "Night / Supercross",
	"Low Vision - Max Visibility": "Max Visibility",
}

var cfgMu sync.Mutex

var migrateOnce sync.Once

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	p := filepath.Join(d, appName)
	migrateOnce.Do(func() {
		// carry settings and looks over from ClearRide (the app's earlier name)
		old := filepath.Join(d, legacyAppName)
		if _, err := os.Stat(filepath.Join(p, "config.json")); err != nil {
			_ = os.MkdirAll(p, 0o755)
			for _, f := range []string{"config.json", "restore.json"} {
				if b, err := os.ReadFile(filepath.Join(old, f)); err == nil {
					_ = os.WriteFile(filepath.Join(p, f), b, 0o644)
				}
			}
		}
	})
	_ = os.MkdirAll(p, 0o755)
	return p
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

func defaultConfig() *Config {
	return &Config{
		Version:           1,
		Presets:           BuiltInPresets(),
		Games:             []Game{{Name: "MX Bikes", Exe: "mxbikes.exe", Preset: "Shadow Detail"}},
		Enabled:           true,
		Mode:              "focused",
		Monitors:          "game",
		SkipWhenReShadeOn: false,
		EditorPreset:      "Shadow Detail",
		UIScale:           1,
	}
}

func loadConfig() *Config {
	b, err := os.ReadFile(configPath())
	if err != nil {
		c := defaultConfig()
		c.Fresh = true
		return c
	}
	c := &Config{}
	if json.Unmarshal(b, c) != nil {
		_ = os.Rename(configPath(), configPath()+".broken")
		return defaultConfig()
	}
	for old, nw := range presetRenames {
		if i := c.findPreset(old); i >= 0 && c.findPreset(nw) < 0 {
			c.Presets[i].Name = nw
		}
		for gi := range c.Games {
			if c.Games[gi].Preset == old {
				c.Games[gi].Preset = nw
			}
		}
		if c.EditorPreset == old {
			c.EditorPreset = nw
		}
	}
	// make sure built-ins exist (new versions may add some)
	for _, bp := range BuiltInPresets() {
		if c.findPreset(bp.Name) < 0 {
			c.Presets = append(c.Presets, bp)
		}
	}
	for i := range c.Presets {
		c.Presets[i].Settings = c.Presets[i].Settings.Sanitize()
		if c.Presets[i].Saved == nil { // looks from before saving existed count as saved
			c.Presets[i].MarkSaved()
		} else {
			sv := c.Presets[i].Saved.Sanitize()
			c.Presets[i].Saved = &sv
		}
	}
	if c.Mode != "running" {
		c.Mode = "focused"
	}
	if c.Monitors != "all" {
		c.Monitors = "game"
	}
	if c.UIScale < 0 || c.UIScale > 2 {
		c.UIScale = 1
	}
	if c.SelectedGame < 0 || c.SelectedGame >= len(c.Games) {
		c.SelectedGame = 0
	}
	return c
}

func (c *Config) save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

func (c *Config) findPreset(name string) int {
	for i, p := range c.Presets {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

func (c *Config) presetSettings(name string) Settings {
	if i := c.findPreset(name); i >= 0 {
		return c.Presets[i].Settings
	}
	return NeutralSettings()
}

func (c *Config) presetNames() []string {
	n := make([]string, len(c.Presets))
	for i, p := range c.Presets {
		n[i] = p.Name
	}
	return n
}

func (c *Config) uniqueName(base string) string {
	name := base
	for i := 2; c.findPreset(name) >= 0; i++ {
		name = base + " " + itoa(i)
	}
	return name
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Preset files for sharing.
type presetFile struct {
	App    string `json:"app"`
	Preset Preset `json:"preset"`
}

func exportPresetJSON(p Preset) string {
	p.BuiltIn = false
	p.Saved = nil
	b, _ := json.MarshalIndent(presetFile{App: appName, Preset: p}, "", "  ")
	return string(b)
}

func importPreset(path string) (Preset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, err
	}
	return parsePreset(b)
}

func parsePreset(b []byte) (Preset, error) {
	var pf presetFile
	if err := json.Unmarshal(b, &pf); err != nil || pf.Preset.Name == "" {
		var p Preset
		if err2 := json.Unmarshal(b, &p); err2 != nil || p.Name == "" {
			return Preset{}, errors.New("this file isn't a " + appName + " look")
		}
		pf.Preset = p
	}
	pf.Preset.BuiltIn = false
	pf.Preset.Settings = pf.Preset.Settings.Sanitize()
	pf.Preset.MarkSaved()
	return pf.Preset, nil
}
