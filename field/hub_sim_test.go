// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/led"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestGetHubLedTimeline(t *testing.T) {
	setupTestArena(t)
	teleopStartSec := game.GetDurationToTeleopStart().Seconds()
	shift1StartSec := teleopStartSec + float64(game.MatchTiming.TransitionShiftDurationSec)
	shiftSec := float64(game.MatchTiming.ShiftDurationSec)
	teleopEndSec := game.GetDurationToTeleopEnd().Seconds()

	tests := []struct {
		name       string
		redWonAuto bool
		sec        float64
		redMode    led.Mode
		blueMode   led.Mode
	}{
		{"auto", true, 1, led.RedStartupMode, led.BlueStartupMode},
		{"pause", true, teleopStartSec - 0.5, led.RedMode, led.BlueMode},
		{"transition, red won", true, teleopStartSec + 1, led.RedAdvantageMode, led.BlueMode},
		{"transition, blue won", false, teleopStartSec + 1, led.RedMode, led.BlueAdvantageMode},
		{"transition warning", true, shift1StartSec - 1, led.RedPulseMode, led.BlueMode},
		{"shift 1, red won", true, shift1StartSec + 1, led.OffMode, led.BlueMode},
		{"shift 1, blue won", false, shift1StartSec + 1, led.RedMode, led.OffMode},
		{"shift 1 warning", true, shift1StartSec + shiftSec - 1, led.OffMode, led.BluePulseMode},
		{"shift 2", true, shift1StartSec + shiftSec + 1, led.RedMode, led.OffMode},
		{"shift 2 warning", true, shift1StartSec + 2*shiftSec - 1, led.RedPulseMode, led.OffMode},
		{"shift 4 ends without warning", true, shift1StartSec + 4*shiftSec - 1, led.RedMode, led.OffMode},
		{"endgame", true, shift1StartSec + 4*shiftSec + 1, led.RedMode, led.BlueMode},
		{"endgame warning", true, teleopEndSec - 1, led.RedPulseMode, led.BluePulseMode},
		{"scoring assessment", true, teleopEndSec + 1, led.WhiteMode, led.WhiteMode},
		{"after scoring assessment", true, teleopEndSec + hubLightScoringAssessmentSec + 0.5, led.OffMode, led.OffMode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redMode, blueMode := timelineModesAt(GetHubLedTimeline(test.redWonAuto), test.sec)
			assert.Equal(t, test.redMode, redMode)
			assert.Equal(t, test.blueMode, blueMode)
		})
	}

	// Segments should cover the whole timeline without gaps or redundant splits.
	timeline := GetHubLedTimeline(true)
	assert.Equal(t, 0.0, timeline.Segments[0].StartSec)
	assert.Equal(t, timeline.EndSec, timeline.Segments[len(timeline.Segments)-1].EndSec)
	for i := 1; i < len(timeline.Segments); i++ {
		previous, segment := timeline.Segments[i-1], timeline.Segments[i]
		assert.InDelta(t, previous.EndSec, segment.StartSec, 1e-9)
		assert.False(t, previous.RedMode == segment.RedMode && previous.BlueMode == segment.BlueMode)
	}

	var shiftNames []string
	for _, shift := range timeline.Shifts {
		shiftNames = append(shiftNames, shift.Name)
	}
	assert.Equal(
		t,
		[]string{"Auto", "Pause", "Transition", "Shift 1", "Shift 2", "Shift 3", "Shift 4", "Endgame", "Post-Match"},
		shiftNames,
	)
	assert.Equal(t, HubShiftSpan{"Shift 1", shift1StartSec, shift1StartSec + shiftSec, false, true}, timeline.Shifts[3])
	assert.Equal(t, teleopEndSec, timeline.Shifts[7].EndSec)
}

func TestHubSimControlsRequireHubSim(t *testing.T) {
	arena := setupTestArena(t)

	assert.NotNil(t, arena.StartHubSimMatch())
	assert.NotNil(t, arena.AbortHubSimMatch())
	assert.NotNil(t, arena.ResetHubSimMatch())
	assert.NotNil(t, arena.SkipHubSimMatchTime(5))
	assert.NotNil(t, arena.SetHubSimAutoWinner("red"))
	assert.Equal(t, PreMatch, arena.MatchState)

	// A forced winner only applies to test matches with the simulator enabled.
	arena.hubSimAutoWinner = "red"
	assert.Equal(t, "", arena.hubSimAutoWinnerOverride())
	arena.SimControlsEnabled = true
	assert.Equal(t, "red", arena.hubSimAutoWinnerOverride())
	arena.CurrentMatch = &model.Match{Type: model.Qualification, ShortName: "Q1"}
	assert.Equal(t, "", arena.hubSimAutoWinnerOverride())

	// Results of a real match must not be thrown away by the simulator.
	arena.MatchState = PostMatch
	err := arena.StartHubSimMatch()
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "Q1")
	}
	assert.NotNil(t, arena.ResetHubSimMatch())
	assert.Equal(t, PostMatch, arena.MatchState)
	assert.Equal(t, model.Qualification, arena.CurrentMatch.Type)
}

func TestHubSimMatchMatchesTimeline(t *testing.T) {
	arena := setupTestArena(t)
	arena.SimControlsEnabled = true
	assert.NotNil(t, arena.SetHubSimAutoWinner("green"))
	assert.Nil(t, arena.SetHubSimAutoWinner("blue"))
	assert.NotNil(t, arena.SkipHubSimMatchTime(5))

	assert.Nil(t, arena.StartHubSimMatch())
	assert.Equal(t, model.Test, arena.CurrentMatch.Type)
	for _, allianceStation := range arena.AllianceStations {
		assert.True(t, allianceStation.Bypass)
	}
	arena.Update()
	assert.Equal(t, AutoPeriod, arena.MatchState)
	assert.NotNil(t, arena.StartHubSimMatch())

	// Skipping ahead moves the match clock.
	assert.NotNil(t, arena.SkipHubSimMatchTime(0))
	assert.NotNil(t, arena.SkipHubSimMatchTime(game.GetDurationToTeleopEnd().Seconds()+1))
	startTime := arena.MatchStartTime
	assert.Nil(t, arena.SkipHubSimMatchTime(5))
	assert.Equal(t, startTime.Add(-5*time.Second), arena.MatchStartTime)

	// The live arena LEDs should follow the timeline for the forced auto winner.
	timeline := GetHubLedTimeline(false)
	for _, sec := range []float64{2.5, 21.5, 23.5, 30.5, 33.5, 55.5, 60.5, 80.5, 109.5, 130.5, 135.5, 160.5} {
		arena.MatchStartTime = time.Now().Add(-time.Duration(sec * float64(time.Second)))
		arena.Update()
		arena.Update()
		redMode, blueMode := arena.Leds.GetModes()
		expectedRedMode, expectedBlueMode := timelineModesAt(timeline, sec)
		assert.Equal(t, expectedRedMode, redMode, "red at %.1f s", sec)
		assert.Equal(t, expectedBlueMode, blueMode, "blue at %.1f s", sec)
	}

	snapshot := arena.GetHubLedSnapshot()
	assert.Equal(t, TeleopPeriod, snapshot.MatchState)
	assert.Equal(t, "blue", snapshot.AutoWinner)
	assert.Equal(t, "blue", snapshot.SimAutoWinner)
	assert.Equal(t, "Endgame", snapshot.Shift)
	assert.True(t, snapshot.RedActive)
	assert.True(t, snapshot.BlueActive)
	assert.Len(t, snapshot.Red, 64*6)

	assert.Nil(t, arena.AbortHubSimMatch())
	arena.Update()
	assert.Equal(t, PostMatch, arena.MatchState)
	assert.NotNil(t, arena.SkipHubSimMatchTime(5))

	assert.Nil(t, arena.ResetHubSimMatch())
	assert.Equal(t, PreMatch, arena.MatchState)
	assert.Equal(t, model.Test, arena.CurrentMatch.Type)
	snapshot = arena.GetHubLedSnapshot()
	assert.Equal(t, "", snapshot.AutoWinner)
	assert.Equal(t, "", snapshot.Shift)
}

func TestEncodeHubPixels(t *testing.T) {
	var pixels [64]led.Color
	pixels[0] = led.Color{R: 255, G: 1, B: 16}
	pixels[63] = led.Blue
	encoded := encodeHubPixels(pixels[:])
	assert.Len(t, encoded, 64*6)
	assert.Equal(t, "ff0110", encoded[:6])
	assert.Equal(t, "0000ff", encoded[63*6:])
}

func timelineModesAt(timeline HubLedTimeline, sec float64) (led.Mode, led.Mode) {
	for _, segment := range timeline.Segments {
		if sec >= segment.StartSec && sec < segment.EndSec {
			return segment.RedMode, segment.BlueMode
		}
	}
	return -1, -1
}
