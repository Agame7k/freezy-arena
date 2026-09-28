// Copyright 2026 Team 254. All Rights Reserved.

package field

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strings"
	"testing"
)

func TestHubCannotPlayMatches(t *testing.T) {
	arena := setupTestArena(t)
	arena.SimulateHardware = true
	arena.EventSettings.MultiFieldRole = model.HubRole
	match := model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 1}
	assert.Nil(t, arena.Database.CreateMatch(&match))

	// The hub may load a match (e.g. to show it on its displays) but never start or simulate it.
	assert.Nil(t, arena.LoadMatch(&match))
	conditions := strings.Join(arena.getStartMatchConditions(), "; ")
	assert.Contains(t, conditions, "hub has no field")
	err := arena.PrepareSimulatedMatch(rand.New(rand.NewSource(1)), model.Qualification)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "hub has no field")
	}

	// Test matches are still allowed.
	assert.Nil(t, arena.LoadTestMatch())
	assert.NotContains(t, strings.Join(arena.getStartMatchConditions(), "; "), "hub has no field")
}

func TestSimulationSkipsMatchMovedToAnotherField(t *testing.T) {
	arena := setupTestArena(t)
	arena.SimulateHardware = true
	arena.EventSettings.MultiFieldRole = model.NodeRole
	arena.EventSettings.FieldId = 1
	first := model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 1}
	second := model.Match{Type: model.Qualification, TypeOrder: 2, ShortName: "Q2", FieldId: 1}
	assert.Nil(t, arena.Database.CreateMatch(&first))
	assert.Nil(t, arena.Database.CreateMatch(&second))
	assert.Nil(t, arena.LoadMatch(&first))

	// The hub moves Q1 to field 2 while it is loaded here; the simulator must play Q2 instead.
	first.FieldId = 2
	assert.Nil(t, arena.Database.UpdateMatch(&first))
	assert.Nil(t, arena.PrepareSimulatedMatch(rand.New(rand.NewSource(1)), model.Qualification))
	assert.Equal(t, second.Id, arena.CurrentMatch.Id)
}
