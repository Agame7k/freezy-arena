// Copyright 2026 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Contains the set of display modes for the hub LEDs.

package led

type Mode int

const (
	OffMode Mode = iota
	RedMode
	BlueMode
	GreenMode
	PurpleMode
	WhiteMode
	RedPulseMode
	BluePulseMode
	RedStartupMode
	BlueStartupMode
	RedAdvantageMode
	BlueAdvantageMode
	RainbowMode
	Side1TestMode
	Side2TestMode
	Side3TestMode
	Side4TestMode
)

var ModeNames = map[Mode]string{
	OffMode:           "Off",
	RedMode:           "Red",
	BlueMode:          "Blue",
	GreenMode:         "Field Safe",
	PurpleMode:        "Field Cleanup",
	WhiteMode:         "Scoring Assessment",
	RedPulseMode:      "Red Pulse",
	BluePulseMode:     "Blue Pulse",
	RedStartupMode:    "Red Startup",
	BlueStartupMode:   "Blue Startup",
	RedAdvantageMode:  "Red Advantage",
	BlueAdvantageMode: "Blue Advantage",
	RainbowMode:       "Rainbow",
	Side1TestMode:     "Test: Facing Driver Station",
	Side2TestMode:     "Test: Facing Audience",
	Side3TestMode:     "Test: Facing Center",
	Side4TestMode:     "Test: Facing Scoring Table",
}

// Returns the solid color associated with the given mode.
func colorForMode(mode Mode) Color {
	switch mode {
	case RedMode:
		return Red
	case BlueMode:
		return Blue
	case GreenMode:
		return Green
	case PurpleMode:
		return Purple
	case WhiteMode:
		return White
	default:
		return Black
	}
}

// modeSwatchCycles covers a full pulse and the startup fill, the longest of the mode sequences.
const modeSwatchCycles = 2*pulseHalfPeriod + startupCycles

// ModeSwatch renders the mode on a scratch zone for a few seconds of update cycles and returns its brightest average
// color and whether it animates, for drawing previews of the mode such as the simulator timeline.
func ModeSwatch(mode Mode, baseColor Color) (Color, bool) {
	scratch := zone{currentMode: mode}
	var swatch Color
	var previousPixels [numPixels]Color
	animated := false
	for cycle := 0; cycle < modeSwatchCycles; cycle++ {
		scratch.updatePixels(baseColor)
		if cycle > 0 && scratch.pixels != previousPixels {
			animated = true
		}
		previousPixels = scratch.pixels

		var sum [3]int
		for _, pixel := range scratch.pixels {
			sum[0] += int(pixel.R)
			sum[1] += int(pixel.G)
			sum[2] += int(pixel.B)
		}
		average := Color{byte(sum[0] / numPixels), byte(sum[1] / numPixels), byte(sum[2] / numPixels)}
		if max(average.R, average.G, average.B) > max(swatch.R, swatch.G, swatch.B) {
			swatch = average
		}
	}
	return swatch, animated
}
