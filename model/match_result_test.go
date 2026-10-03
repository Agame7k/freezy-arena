// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package model

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetNonexistentMatchResult(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	match, err := db.GetMatchResultForMatch(1114)
	assert.Nil(t, err)
	assert.Nil(t, match)
}

func TestMatchResultCrud(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	matchResult := BuildTestMatchResult(254, 5)
	assert.Nil(t, db.CreateMatchResult(matchResult))
	matchResult2, err := db.GetMatchResultForMatch(254)
	assert.Nil(t, err)
	assert.Equal(t, matchResult, matchResult2)

	matchResult.BlueScore.EndgameTowerStatuses =
		[3]game.TowerStatus{game.TowerLevel1, game.TowerNone, game.TowerLevel2}
	assert.Nil(t, db.UpdateMatchResult(matchResult))
	matchResult2, err = db.GetMatchResultForMatch(254)
	assert.Nil(t, err)
	assert.Equal(t, matchResult, matchResult2)

	assert.Nil(t, db.DeleteMatchResult(matchResult.Id))
	matchResult2, err = db.GetMatchResultForMatch(254)
	assert.Nil(t, err)
	assert.Nil(t, matchResult2)
}

func TestTruncateMatchResults(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	matchResult := BuildTestMatchResult(254, 1)
	assert.Nil(t, db.CreateMatchResult(matchResult))
	assert.Nil(t, db.TruncateMatchResults())
	matchResult2, err := db.GetMatchResultForMatch(254)
	assert.Nil(t, err)
	assert.Nil(t, matchResult2)
}

func TestGetMatchResultForMatch(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	matchResult := BuildTestMatchResult(254, 2)
	assert.Nil(t, db.CreateMatchResult(matchResult))
	matchResult2 := BuildTestMatchResult(254, 5)
	assert.Nil(t, db.CreateMatchResult(matchResult2))
	matchResult3 := BuildTestMatchResult(254, 4)
	assert.Nil(t, db.CreateMatchResult(matchResult3))

	// Should return the match result with the highest play number (i.e. the most recent).
	matchResult4, err := db.GetMatchResultForMatch(254)
	assert.Nil(t, err)
	assert.Equal(t, matchResult2, matchResult4)
}

func TestCorrectPlayoffScoreResetsDqState(t *testing.T) {
	matchResult := NewMatchResult()
	matchResult.RedScore.PlayoffDq = true
	matchResult.BlueScore.PlayoffDq = true
	matchResult.RedCards = map[string]string{"1": "red"}
	matchResult.BlueCards = map[string]string{}

	matchResult.CorrectPlayoffScore()
	assert.Equal(t, true, matchResult.RedScore.PlayoffDq)
	assert.Equal(t, false, matchResult.BlueScore.PlayoffDq)

	matchResult.RedCards = map[string]string{}
	matchResult.BlueCards = map[string]string{"4": "dq"}

	matchResult.CorrectPlayoffScore()
	assert.Equal(t, false, matchResult.RedScore.PlayoffDq)
	assert.Equal(t, true, matchResult.BlueScore.PlayoffDq)
}

func TestApplyPlayoffAllianceCards(t *testing.T) {
	match := &Match{Red1: 1, Red2: 2, Red3: 3, Blue1: 4, Blue2: 5, Blue3: 6}
	testCases := []struct {
		name          string
		redCards      map[string]string
		expectedCards map[string]string
	}{
		{"no cards", map[string]string{}, map[string]string{}},
		{
			"one team's card applies to the alliance",
			map[string]string{"2": "yellow"},
			map[string]string{"1": "yellow", "2": "yellow", "3": "yellow"},
		},
		{
			"most serious card wins",
			map[string]string{"1": "yellow", "3": "red"},
			map[string]string{"1": "red", "2": "red", "3": "red"},
		},
		{
			"dq outranks red",
			map[string]string{"2": "red", "3": "dq"},
			map[string]string{"1": "dq", "2": "dq", "3": "dq"},
		},
		{"cleared cards are removed", map[string]string{"1": "", "2": ""}, map[string]string{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			matchResult := NewMatchResult()
			matchResult.RedCards = testCase.redCards
			matchResult.BlueCards = map[string]string{"5": "yellow"}
			matchResult.ApplyPlayoffAllianceCards(match)
			assert.Equal(t, testCase.expectedCards, matchResult.RedCards)
			assert.Equal(t, map[string]string{"4": "yellow", "5": "yellow", "6": "yellow"}, matchResult.BlueCards)
		})
	}
}
