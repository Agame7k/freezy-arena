// Copyright 2026 Team 254. All Rights Reserved.

package model

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestFtaEventCrud(t *testing.T) {
	db := setupTestDb(t)

	events, err := db.GetAllFtaEvents()
	assert.Nil(t, err)
	assert.Empty(t, events)

	for _, event := range []FtaEvent{
		{MatchId: 1, TeamId: 254, Station: "R1", Type: FtaEventRobotLost, Severity: FtaSeverityBad},
		{MatchId: 1, TeamId: 1114, Station: "B1", Type: FtaEventEStop, Severity: FtaSeverityBad},
		{MatchId: 2, TeamId: 254, Station: "R2", Type: FtaEventLowBattery, Severity: FtaSeverityWarn},
	} {
		assert.Nil(t, db.CreateFtaEvent(&event))
	}

	events, err = db.GetFtaEventsByMatchId(1)
	assert.Nil(t, err)
	if assert.Len(t, events, 2) {
		assert.Equal(t, 254, events[0].TeamId)
		assert.Equal(t, 1114, events[1].TeamId)
	}
	events, err = db.GetFtaEventsByTeamId(254)
	assert.Nil(t, err)
	if assert.Len(t, events, 2) {
		assert.Equal(t, FtaEventRobotLost, events[0].Type)
		assert.Equal(t, FtaEventLowBattery, events[1].Type)
	}

	assert.Nil(t, db.TruncateFtaEvents())
	events, err = db.GetAllFtaEvents()
	assert.Nil(t, err)
	assert.Empty(t, events)
}

func TestFtaTeamMatchStatsCrud(t *testing.T) {
	db := setupTestDb(t)

	assert.Nil(t, db.CreateFtaTeamMatchStats(&FtaTeamMatchStats{MatchId: 1, TeamId: 254, MinBatteryVoltage: 9.5}))
	assert.Nil(t, db.CreateFtaTeamMatchStats(&FtaTeamMatchStats{MatchId: 1, TeamId: 1114}))
	assert.Nil(t, db.CreateFtaTeamMatchStats(&FtaTeamMatchStats{MatchId: 2, TeamId: 254, MinBatteryVoltage: 8.1}))

	stats, err := db.GetFtaTeamMatchStatsByTeamId(254)
	assert.Nil(t, err)
	if assert.Len(t, stats, 2) {
		assert.Equal(t, 9.5, stats[0].MinBatteryVoltage)
		assert.Equal(t, 8.1, stats[1].MinBatteryVoltage)
	}
	stats, err = db.GetFtaTeamMatchStatsByMatchId(1)
	assert.Nil(t, err)
	assert.Len(t, stats, 2)
}

func TestFtaNoteCrud(t *testing.T) {
	db := setupTestDb(t)

	now := time.Unix(1000, 0).UTC()
	note := FtaNote{TeamId: 254, Time: now, Tag: "radio", Text: "Radio rebooted mid-match"}
	assert.Nil(t, db.CreateFtaNote(&note))
	assert.Nil(t, db.CreateFtaNote(&FtaNote{TeamId: 254, Time: now, Tag: "can", Text: "Loose CAN connector"}))
	assert.Nil(t, db.CreateFtaNote(&FtaNote{TeamId: 1114, Time: now, Tag: "other", Text: "Asked about spare radio"}))

	notes, err := db.GetFtaNotesByTeamId(254)
	assert.Nil(t, err)
	if assert.Len(t, notes, 2) {
		// Newest first.
		assert.Equal(t, "can", notes[0].Tag)
		assert.Equal(t, "radio", notes[1].Tag)
	}

	fetched, err := db.GetFtaNoteById(note.Id)
	assert.Nil(t, err)
	assert.Equal(t, note, *fetched)

	assert.Nil(t, db.DeleteFtaNote(note.Id))
	notes, err = db.GetFtaNotesByTeamId(254)
	assert.Nil(t, err)
	assert.Len(t, notes, 1)
}

func TestFtaNoteValidation(t *testing.T) {
	db := setupTestDb(t)

	testCases := []struct {
		name  string
		note  FtaNote
		error string
	}{
		{"missing team", FtaNote{Tag: "radio", Text: "x"}, "note must be for a team"},
		{"blank text", FtaNote{TeamId: 254, Tag: "radio", Text: "  "}, "note text must not be empty"},
		{"unknown tag", FtaNote{TeamId: 254, Tag: "vibes", Text: "x"}, "invalid note tag \"vibes\""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := db.CreateFtaNote(&testCase.note)
			if assert.NotNil(t, err) {
				assert.Equal(t, testCase.error, err.Error())
			}
		})
	}
	notes, err := db.GetAllFtaNotes()
	assert.Nil(t, err)
	assert.Empty(t, notes)
}
