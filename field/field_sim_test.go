// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/led"
	"github.com/Team254/cheesy-arena/plc"
	"github.com/stretchr/testify/assert"
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestEnableSimulation(t *testing.T) {
	arena := setupTestArena(t)
	assert.NotNil(t, arena.SetSimFieldEStop(true))
	arena.EnableSimulation()
	assert.True(t, arena.SimControlsEnabled)
	_, ok := arena.Plc.(*plc.SimPlc)
	assert.True(t, ok)

	// A configured PLC is left in place, so its inputs can't be simulated.
	arena = setupTestArena(t)
	arena.Plc.SetAddress("10.0.100.40")
	realPlc := arena.Plc
	arena.EnableSimulation()
	assert.Same(t, realPlc, arena.Plc)
	assert.NotNil(t, arena.SetSimFieldEStop(true))
	assert.NotNil(t, arena.AddSimHubFuel("red", 1))
	assert.False(t, arena.GetFieldSimStatus().SimulatedPlc)
}

func TestFieldSimStatusWithSimulatedPlc(t *testing.T) {
	arena := setupTestArena(t)
	arena.EnableSimulation()
	arena.Update()

	// Before the match, the arena's PLC logic drives the field lights through the simulated PLC.
	status := arena.GetFieldSimStatus()
	assert.True(t, status.PlcEnabled)
	assert.True(t, status.SimulatedPlc)
	assert.False(t, status.FieldEStopPressed)
	assert.True(t, fieldSimCoil(arena, status, "fieldResetLight"))
	assert.True(t, fieldSimCoil(arena, status, "stackLightRed"))
	assert.True(t, fieldSimCoil(arena, status, "stackLightBlue"))
	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.GreenMode, redMode)
	assert.Equal(t, led.GreenMode, blueMode)
	assert.Equal(t, "R1", status.Stations[0].Id)
	assert.Equal(t, "B3", status.Stations[5].Id)
	assert.Equal(t, "No Team Assigned", strings.TrimSpace(status.Stations[0].Sign.RearText))
	assert.Equal(t, "00:20", status.RedTimer.FrontText)
	assert.Equal(t, "#ffc8b4", status.RedTimer.FrontColor)

	// The stop buttons go through the PLC inputs to the arena.
	assert.Nil(t, arena.SetSimStationStop("B2", false, true))
	assert.NotNil(t, arena.SetSimStationStop("B4", false, true))
	arena.Update()
	status = arena.GetFieldSimStatus()
	assert.True(t, status.Stations[4].EStopPressed)
	assert.True(t, status.Stations[4].EStop)
	assert.Nil(t, arena.SetSimStationStop("B2", false, false))
	arena.Update()
	assert.False(t, arena.AllianceStations["B2"].EStop)

	// Fuel counted through the simulated Hub sensors is scored by the arena.
	assert.Nil(t, arena.StartHubSimMatch())
	arena.Update()
	assert.Equal(t, AutoPeriod, arena.MatchState)
	assert.Nil(t, arena.AddSimHubFuel("red", 3))
	assert.Nil(t, arena.AddSimHubFuel("blue", 1))
	assert.NotNil(t, arena.AddSimHubFuel("green", 1))
	assert.NotNil(t, arena.AddSimHubFuel("red", -1))
	arena.Update()
	status = arena.GetFieldSimStatus()
	assert.True(t, status.FtaReady)
	assert.Equal(t, 3, status.RedFuel)
	assert.Equal(t, 3, status.RedActiveFuel)
	assert.Equal(t, 1, status.BlueFuel)
	assert.True(t, fieldSimCoil(arena, status, "redHubMotor"))
	assert.True(t, fieldSimCoil(arena, status, "redHubLight"))
	assert.True(t, fieldSimCoil(arena, status, "stackLightGreen"))
	assert.False(t, fieldSimCoil(arena, status, "stackLightRed"))

	// Red scored more Fuel in auto, so it wins auto when teleop starts.
	arena.MatchStartTime = time.Now().Add(-game.GetDurationToTeleopStart() - time.Second)
	arena.Update()
	arena.Update()
	assert.Equal(t, TeleopPeriod, arena.MatchState)
	assert.Equal(t, "red", arena.GetHubLedSnapshot().AutoWinner)

	// Pressing the field E-stop aborts the match, and no new match can start until it is released.
	assert.Nil(t, arena.SetSimFieldEStop(true))
	arena.Update()
	assert.Equal(t, PostMatch, arena.MatchState)
	assert.True(t, arena.GetFieldSimStatus().FieldEStopPressed)
	err := arena.StartHubSimMatch()
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "field emergency stop is active")
	}
	assert.Nil(t, arena.SetSimFieldEStop(false))
	arena.Update()
	assert.Nil(t, arena.StartHubSimMatch())
}

func TestTeamSignNextTeamWithNoTeamAssigned(t *testing.T) {
	arena := setupTestArena(t)
	sign := &TeamSign{nextMatchTeamId: 1503}
	arena.AllianceStationDisplayMode = "logo"
	arena.MatchState = PostMatch

	frontText, _, rearText := sign.generateTexts(arena, "R1", true, "00:00", "")
	assert.Equal(t, " 2026", frontText)
	assert.Equal(t, "Next Team Up: 1503", rearText)
}

func TestFieldSimSignColor(t *testing.T) {
	assert.Equal(t, "#ff0000", fieldSimSignColor(redColor))
	assert.Equal(t, "#000000", fieldSimSignColor(color.RGBA{255, 0, 0, 0}))
	assert.Equal(t, "#800000", fieldSimSignColor(color.RGBA{255, 0, 0, 128}))
}

func fieldSimCoil(arena *Arena, status FieldSimStatus, name string) bool {
	for i, coilName := range arena.Plc.GetCoilNames() {
		if coilName == name {
			return status.Coils[i] == '1'
		}
	}
	return false
}
