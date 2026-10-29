package main

import "math"

// Settings is one colour "look". Every effect here is a per-pixel tone/colour
// curve, so it can be applied through the display gamma ramp without touching
// the game process at all.
type Settings struct {
	BlackPoint  float64 `json:"blackPoint"`  // 0..127   input black (Levels)
	WhitePoint  float64 `json:"whitePoint"`  // 128..255 input white (Levels)
	Brightness  float64 `json:"brightness"`  // -50..50  % gain
	Contrast    float64 `json:"contrast"`    // -50..50
	Gamma       float64 `json:"gamma"`       // 0.50..2.00 (>1 = brighter mid-tones)
	Shadows     float64 `json:"shadows"`     // -50..100 lift/crush dark tones
	Highlights  float64 `json:"highlights"`  // -100..50 tame/boost bright tones
	BlackFloor  float64 `json:"blackFloor"`  // 0..30 % raise pure black
	Saturation  float64 `json:"saturation"`  // 0..100 % extra colour (GPU driver)
	Temperature float64 `json:"temperature"` // -50..50 cool..warm
	Tint        float64 `json:"tint"`        // -50..50 green..magenta
	Red         float64 `json:"red"`         // 50..150 %
	Green       float64 `json:"green"`       // 50..150 %
	Blue        float64 `json:"blue"`        // 50..150 %
}

func NeutralSettings() Settings {
	return Settings{WhitePoint: 255, Gamma: 1, Red: 100, Green: 100, Blue: 100}
}

// Sanitize clamps every value into its supported range (protects against
// hand-edited or imported preset files).
func (s Settings) Sanitize() Settings {
	c := func(v, lo, hi, def float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return def
		}
		return math.Max(lo, math.Min(hi, v))
	}
	s.BlackPoint = c(s.BlackPoint, 0, 127, 0)
	s.WhitePoint = c(s.WhitePoint, 128, 255, 255)
	s.Brightness = c(s.Brightness, -50, 50, 0)
	s.Contrast = c(s.Contrast, -50, 50, 0)
	s.Gamma = c(s.Gamma, 0.5, 2.0, 1)
	s.Shadows = c(s.Shadows, -50, 100, 0)
	s.Highlights = c(s.Highlights, -100, 50, 0)
	s.BlackFloor = c(s.BlackFloor, 0, 30, 0)
	s.Saturation = c(s.Saturation, 0, 100, 0)
	s.Temperature = c(s.Temperature, -50, 50, 0)
	s.Tint = c(s.Tint, -50, 50, 0)
	s.Red = c(s.Red, 50, 150, 100)
	s.Green = c(s.Green, 50, 150, 100)
	s.Blue = c(s.Blue, 50, 150, 100)
	return s
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// toneCurve applies the channel-independent part of the look to x in [0,1].
func (s Settings) toneCurve(x float64) float64 {
	// Levels
	bp, wp := s.BlackPoint/255, s.WhitePoint/255
	x = clamp01((x - bp) / (wp - bp))
	// Brightness (gain)
	x = clamp01(x * (1 + s.Brightness/100))
	// Shadows / highlights: smooth bumps that leave 0 and 1 fixed.
	// x(1-x)^2 peaks at x=1/3, x^2(1-x) peaks at x=2/3; both peak at 4/27.
	const peak = 4.0 / 27.0
	sh := x * (1 - x) * (1 - x) / peak
	hi := x * x * (1 - x) / peak
	x = clamp01(x + 0.25*(s.Shadows/100)*sh + 0.25*(s.Highlights/100)*hi)
	// Contrast: blend toward an S-curve (or its inverse) around mid-grey.
	if c := s.Contrast / 50; c > 0 {
		sCurve := x * x * (3 - 2*x)
		x = x + (sCurve-x)*c
	} else if c < 0 {
		inv := 0.5 - math.Sin(math.Asin(1-2*x)/3)
		x = x + (inv-x)*(-c)
	}
	// Gamma (>1 brightens mid-tones)
	if x > 0 {
		x = math.Pow(x, 1/s.Gamma)
	}
	return clamp01(x)
}

// channelGains returns per-channel multipliers from temperature, tint and RGB.
func (s Settings) channelGains() [3]float64 {
	t, tn := s.Temperature/50, s.Tint/50
	wb := [3]float64{1 + 0.18*t, 1 - 0.12*tn, 1 - 0.18*t}
	if tn < 0 { // negative tint = more green -> pull red/blue down instead
		wb = [3]float64{(1 + 0.18*t) * (1 + 0.12*tn), 1, (1 - 0.18*t) * (1 + 0.12*tn)}
	}
	m := math.Max(wb[0], math.Max(wb[1], wb[2]))
	user := [3]float64{s.Red / 100, s.Green / 100, s.Blue / 100}
	var g [3]float64
	for i := range g {
		g[i] = wb[i] / m * user[i]
	}
	return g
}

// Curves returns the three output curves (R,G,B), 256 entries each, in [0,1].
func (s Settings) Curves() [3][256]float64 {
	s = s.Sanitize()
	gains := s.channelGains()
	floor := s.BlackFloor / 100
	var out [3][256]float64
	for i := 0; i < 256; i++ {
		base := s.toneCurve(float64(i) / 255)
		for c := 0; c < 3; c++ {
			y := clamp01(base * gains[c])
			out[c][i] = floor + (1-floor)*y
		}
	}
	return out
}

// Ramp composes the look with the monitor's original ramp (so the user's own
// calibration stays in place underneath) and returns a gamma ramp.
func (s Settings) Ramp(orig *[3][256]uint16) [3][256]uint16 {
	curves := s.Curves()
	var r [3][256]uint16
	for c := 0; c < 3; c++ {
		for i := 0; i < 256; i++ {
			pos := curves[c][i] * 255
			if orig == nil {
				r[c][i] = uint16(math.Round(pos / 255 * 65535))
				continue
			}
			lo := int(math.Floor(pos))
			if lo >= 255 {
				r[c][i] = orig[c][255]
				continue
			}
			f := pos - float64(lo)
			v := float64(orig[c][lo])*(1-f) + float64(orig[c][lo+1])*f
			r[c][i] = uint16(math.Round(v))
		}
	}
	return r
}

// IdentityRamp is the default linear ramp.
func IdentityRamp() [3][256]uint16 {
	var r [3][256]uint16
	for c := 0; c < 3; c++ {
		for i := 0; i < 256; i++ {
			r[c][i] = uint16(i * 257)
		}
	}
	return r
}

// SaneRamp reports whether a ramp read back from Windows looks usable as a
// base (monotonic, not all-zero). Otherwise we fall back to identity.
func SaneRamp(r *[3][256]uint16) bool {
	for c := 0; c < 3; c++ {
		if r[c][255] < 0x4000 {
			return false
		}
		for i := 1; i < 256; i++ {
			if r[c][i] < r[c][i-1] {
				return false
			}
		}
	}
	return true
}

// Preset is a named look.
type Preset struct {
	Name     string    `json:"name"`
	Settings Settings  `json:"settings"`        // what is in use right now (shown live)
	Saved    *Settings `json:"saved,omitempty"` // the last saved version; differs = unsaved changes
	BuiltIn  bool      `json:"builtIn,omitempty"`
}

// Dirty reports unsaved changes.
func (p Preset) Dirty() bool { return p.Saved != nil && *p.Saved != p.Settings }

// MarkSaved makes the current settings the saved version.
func (p *Preset) MarkSaved() { s := p.Settings; p.Saved = &s }

func BuiltInPresets() []Preset {
	n := NeutralSettings
	mk := func(name string, f func(*Settings)) Preset {
		s := n()
		f(&s)
		saved := s
		return Preset{Name: name, Settings: s, Saved: &saved, BuiltIn: true}
	}
	return []Preset{
		mk("Off", func(s *Settings) {}),
		// Recreates a common ReShade gameplay setup: Levels white point 221
		// plus Clarity's dark-tone boost approximated with a shadow lift.
		mk("ReShade Style (Levels + Clarity)", func(s *Settings) {
			s.WhitePoint = 221
			s.Shadows = 40
			s.Contrast = 6
		}),
		mk("Shadow Detail", func(s *Settings) {
			s.Shadows = 60
			s.Highlights = -20
			s.Gamma = 1.08
		}),
		mk("Night / Supercross", func(s *Settings) {
			s.Shadows = 80
			s.Gamma = 1.15
			s.BlackFloor = 4
			s.Saturation = 15
		}),
		mk("Bright Day / Glare", func(s *Settings) {
			s.Highlights = -50
			s.Shadows = 20
			s.Contrast = 8
		}),
		mk("Punchy Colour", func(s *Settings) {
			s.Contrast = 15
			s.Saturation = 35
			s.Shadows = 15
		}),
		mk("Max Visibility", func(s *Settings) {
			s.WhitePoint = 230
			s.Shadows = 100
			s.Highlights = -30
			s.Gamma = 1.2
			s.BlackFloor = 6
			s.Contrast = 10
			s.Saturation = 30
		}),
		mk("Warm", func(s *Settings) { s.Temperature = 25 }),
		mk("Cool", func(s *Settings) { s.Temperature = -25 }),
	}
}
