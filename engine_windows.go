//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Engine owns every change made to the displays and guarantees it can put
// them back: original ramps are captured before the first change and written
// to a recovery file, so even a crash or power cut can be undone next launch.
type Engine struct {
	mu        sync.Mutex
	sat       Saturator
	orig      map[string][3][256]uint16 // captured originals per display
	applied   map[string][3][256]uint16 // what we last wrote
	cur       *Desired
	lastCheck time.Time
	RampError string // last ramp failure, for the UI
	SatError  string

	// another program fighting over the ramp (old ClearRide, f.lux, Night light, GPU software...)
	overwrites  []time.Time
	pausedUntil time.Time
	Conflict    string
}

type Desired struct {
	Settings Settings
	Displays []string
	Label    string
}

func (d *Desired) same(o *Desired) bool {
	if d == nil || o == nil {
		return d == o
	}
	if d.Settings != o.Settings || len(d.Displays) != len(o.Displays) {
		return false
	}
	for i := range d.Displays {
		if d.Displays[i] != o.Displays[i] {
			return false
		}
	}
	return true
}

type recovery struct {
	Ramps      map[string][3][256]uint16 `json:"ramps"`
	Saturation map[string]int            `json:"saturation"`
	Vendor     string                    `json:"vendor"`
}

func recoveryPath() string { return filepath.Join(configDir(), "restore.json") }

func NewEngine() *Engine {
	e := &Engine{sat: detectSaturator(), orig: map[string][3][256]uint16{}, applied: map[string][3][256]uint16{}}
	e.recoverFromCrash()
	return e
}

func (e *Engine) SaturationName() string {
	if e.sat == nil {
		return ""
	}
	return e.sat.Name()
}

// recoverFromCrash undoes changes left behind by a previous run that didn't
// shut down cleanly.
func (e *Engine) recoverFromCrash() { e.recoverFrom(recoveryPath()) }

// recoverFrom applies a recovery file (ours, or one left by the old ClearRide).
func (e *Engine) recoverFrom(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var r recovery
	if json.Unmarshal(b, &r) == nil {
		for name, ramp := range r.Ramps {
			rp := ramp
			setRamp(name, &rp)
		}
		if e.sat != nil && len(r.Saturation) > 0 && r.Vendor == e.sat.Name() {
			e.sat.RestoreFrom(r.Saturation)
		}
		logf("restored screen colours from %s", path)
	}
	os.Remove(path)
}

func (e *Engine) writeRecovery() {
	r := recovery{Ramps: e.orig}
	if e.sat != nil {
		r.Saturation = e.sat.Originals()
		r.Vendor = e.sat.Name()
	}
	if b, err := json.Marshal(r); err == nil {
		tmp := recoveryPath() + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			os.Rename(tmp, recoveryPath())
		}
	}
}

// Set makes the displays match d (nil = everything back to normal).
// Safe to call often; it only touches hardware when something changed, plus
// a periodic check that re-applies if a game or driver reset the ramp.
func (e *Engine) Set(d *Desired) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d != nil {
		ds := append([]string(nil), d.Displays...)
		sort.Strings(ds)
		d = &Desired{Settings: d.Settings.Sanitize(), Displays: ds, Label: d.Label}
	}
	if d == nil {
		if e.cur != nil || len(e.orig) > 0 {
			e.restoreLocked()
		}
		return
	}
	changed := !d.same(e.cur)
	if !changed && time.Since(e.lastCheck) < time.Second {
		return
	}
	if !changed && time.Now().Before(e.pausedUntil) {
		return // backing off: something else keeps changing the colours
	}
	if !changed && !e.pausedUntil.IsZero() && time.Now().After(e.pausedUntil) {
		e.pausedUntil = time.Time{}
		e.Conflict = ""
		e.overwrites = nil
	}
	e.lastCheck = time.Now()

	want := map[string]bool{}
	for _, name := range d.Displays {
		want[name] = true
	}
	// displays no longer targeted go back to their original ramp
	for name, o := range e.orig {
		if !want[name] {
			op := o
			setRamp(name, &op)
			delete(e.orig, name)
			delete(e.applied, name)
		}
	}
	e.RampError = ""
	newCapture := false
	for _, name := range d.Displays {
		base, captured := e.orig[name]
		if !captured {
			r, ok := getRamp(name)
			if !ok || !SaneRamp(&r) {
				r = IdentityRamp()
			}
			base = r
			e.orig[name] = r
			newCapture = true
		}
		if !changed {
			// keep-alive: only rewrite if someone replaced our ramp
			if now, ok := getRamp(name); ok {
				prev := e.applied[name]
				if rampClose(&now, &prev) {
					continue
				}
			}
			// someone replaced our ramp. Re-apply, but if it keeps happening,
			// stop: two programs fighting over the colours = flicker.
			nowT := time.Now()
			kept := e.overwrites[:0]
			for _, t := range e.overwrites {
				if nowT.Sub(t) < 20*time.Second {
					kept = append(kept, t)
				}
			}
			e.overwrites = append(kept, nowT)
			logf("keep-alive: colours on %s were changed by something else (%d in 20s)", name, len(e.overwrites))
			if len(e.overwrites) >= 4 {
				e.pausedUntil = nowT.Add(60 * time.Second)
				e.Conflict = "Another program keeps changing your screen colours (for example the old ClearRide, f.lux, Windows Night light or graphics-card colour settings). Optic has stopped re-applying to prevent flicker - close the other program."
				logf("conflict detected: pausing keep-alive for 60s")
				break
			}
		}
		ramp := d.Settings.Ramp(&base)
		if setRamp(name, &ramp) {
			e.applied[name] = ramp
		} else {
			logf("SetDeviceGammaRamp refused on %s", name)
			e.RampError = "Windows refused the colour change on " + name +
				". Try smaller values, turn off HDR, or use 'Unlock full range'."
		}
	}
	if e.sat != nil && (changed || newCapture) {
		e.SatError = ""
		if err := e.sat.Apply(d.Displays, d.Settings.Saturation); err != nil {
			e.SatError = err.Error()
			logf("saturation: %v", err)
		}
	}
	if newCapture || changed {
		e.writeRecovery()
	}
	e.cur = d
}

// rampClose tolerates drivers that store the ramp at lower precision.
func rampClose(a, b *[3][256]uint16) bool {
	for c := 0; c < 3; c++ {
		for i := 0; i < 256; i++ {
			d := int(a[c][i]) - int(b[c][i])
			if d > 512 || d < -512 {
				return false
			}
		}
	}
	return true
}

func (e *Engine) restoreLocked() {
	for name, o := range e.orig {
		op := o
		setRamp(name, &op)
	}
	if e.sat != nil {
		e.sat.Restore()
	}
	e.orig = map[string][3][256]uint16{}
	e.applied = map[string][3][256]uint16{}
	e.cur = nil
	e.overwrites = nil
	os.Remove(recoveryPath())
}

// Restore puts everything back immediately (exit, panic hotkey).
func (e *Engine) Restore() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restoreLocked()
}

func (e *Engine) Active() *Desired {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cur
}

func (e *Engine) Errors() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.TrimSpace(e.Conflict + " " + e.RampError + " " + e.SatError)
}
