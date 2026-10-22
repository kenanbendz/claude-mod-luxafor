package main

import "testing"

func TestHue(t *testing.T) {
	for h, want := range map[float64]rgb{0: {255, 0, 0}, 60: {255, 255, 0}, 120: {0, 255, 0}, 240: {0, 0, 255}, 360: {255, 0, 0}, -120: {0, 0, 255}} {
		if got := hue(h); got != want {
			t.Errorf("hue(%v) = %v, want %v", h, got, want)
		}
	}
}
