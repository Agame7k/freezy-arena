// Copyright 2026 Team 254. All Rights Reserved.

package plc

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSimPlc(t *testing.T) {
	plc := NewSimPlc()
	plc.SetAddress("10.0.100.40")
	assert.True(t, plc.IsEnabled())
	assert.True(t, plc.IsHealthy())
	assert.False(t, plc.GetFieldEStop())
	for name, connected := range plc.GetArmorBlockStatuses() {
		assert.True(t, connected, name)
	}
	redEStops, blueEStops := plc.GetTeamEStops()
	assert.Equal(t, [3]bool{false, false, false}, redEStops)
	assert.Equal(t, [3]bool{false, false, false}, blueEStops)

	assert.Nil(t, plc.SetStationStop("R2", false, true))
	assert.Nil(t, plc.SetStationStop("B3", true, true))
	assert.NotNil(t, plc.SetStationStop("R4", false, true))
	redEStops, _ = plc.GetTeamEStops()
	_, blueAStops := plc.GetTeamAStops()
	assert.Equal(t, [3]bool{false, true, false}, redEStops)
	assert.Equal(t, [3]bool{false, false, true}, blueAStops)
	assert.Nil(t, plc.SetStationStop("R2", false, false))
	redEStops, _ = plc.GetTeamEStops()
	assert.Equal(t, [3]bool{false, false, false}, redEStops)

	plc.SetFieldEStop(true)
	assert.True(t, plc.GetFieldEStop())
	plc.SetFieldEStop(false)
	assert.False(t, plc.GetFieldEStop())

	assert.Nil(t, plc.AddHubFuel(3, 0))
	assert.Nil(t, plc.AddHubFuel(1, 2))
	assert.NotNil(t, plc.AddHubFuel(-1, 0))
	redCount, blueCount := plc.GetHubCounts()
	assert.Equal(t, 4, redCount)
	assert.Equal(t, 2, blueCount)

	// Starting a new match clears the counts but keeps the ArmorBlocks connected.
	plc.ResetMatch()
	redCount, blueCount = plc.GetHubCounts()
	assert.Equal(t, 0, redCount)
	assert.Equal(t, 0, blueCount)
	for name, connected := range plc.GetArmorBlockStatuses() {
		assert.True(t, connected, name)
	}
}
