package main

import (
	"math"
	"testing"
)

func TestNeutralIsIdentity(t *testing.T) {
	id := IdentityRamp()
	r := NeutralSettings().Ramp(&id)
	for c := 0; c < 3; c++ {
		for i := 0; i < 256; i++ {
			if d := int(r[c][i]) - int(id[c][i]); d < -1 || d > 1 {
				t.Fatalf("neutral not identity at c=%d i=%d: %d vs %d", c, i, r[c][i], id[c][i])
			}
		}
	}
}

func TestMonotonicAndEndpoints(t *testing.T) {
	for _, p := range BuiltInPresets() {
		cv := p.Settings.Curves()
		for c := 0; c < 3; c++ {
			for i := 1; i < 256; i++ {
				if cv[c][i]+1e-9 < cv[c][i-1] {
					t.Fatalf("%s not monotonic c=%d i=%d", p.Name, c, i)
				}
			}
		}
	}
	// extremes must stay monotonic too
	s := NeutralSettings()
	s.Shadows, s.Highlights, s.Contrast, s.Gamma = 100, -100, -50, 0.5
	cv := s.Curves()
	for i := 1; i < 256; i++ {
		if cv[0][i]+1e-9 < cv[0][i-1] {
			t.Fatalf("extreme not monotonic at %d", i)
		}
	}
}

func TestShadowLiftBrightensDarks(t *testing.T) {
	s := NeutralSettings()
	s.Shadows = 100
	cv := s.Curves()
	if cv[0][85] < 85.0/255+0.2 { // +0.25 at x=1/3
		t.Fatalf("shadow lift too weak: %v", cv[0][85])
	}
	if math.Abs(cv[0][255]-1) > 1e-9 || cv[0][0] != 0 {
		t.Fatalf("endpoints moved")
	}
}

func TestWhitePoint221(t *testing.T) {
	s := NeutralSettings()
	s.WhitePoint = 221
	cv := s.Curves()
	if cv[1][221] < 0.999 {
		t.Fatalf("221 should map to white, got %v", cv[1][221])
	}
	if math.Abs(cv[1][110]-110.0/221) > 0.01 {
		t.Fatalf("mid mapping wrong %v", cv[1][110])
	}
}

func TestComposeWithCalibration(t *testing.T) {
	orig := IdentityRamp()
	for i := 0; i < 256; i++ { // fake calibration: red channel dimmed 10%
		orig[0][i] = uint16(float64(i*257) * 0.9)
	}
	r := NeutralSettings().Ramp(&orig)
	if r[0][255] != orig[0][255] || r[1][255] != 65535 {
		t.Fatalf("calibration not preserved: %d %d", r[0][255], r[1][255])
	}
}

func TestSanitize(t *testing.T) {
	s := Settings{Gamma: math.NaN(), WhitePoint: 9999, Red: -5}
	s = s.Sanitize()
	if s.Gamma != 1 || s.WhitePoint != 255 || s.Red != 50 {
		t.Fatalf("bad sanitize %+v", s)
	}
}
