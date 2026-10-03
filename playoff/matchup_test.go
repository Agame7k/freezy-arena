// Copyright 2022 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package playoff

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMatchupAllianceSourceDisplayNames(t *testing.T) {
	// Test double-elimination.
	matchup, _, err := newDoubleEliminationBracket(8)
	assert.Nil(t, err)

	assert.Equal(t, "W M11", matchup.RedAllianceSourceDisplayName())
	assert.Equal(t, "W M13", matchup.BlueAllianceSourceDisplayName())

	matchGroups, err := collectMatchGroups(matchup)
	assert.Nil(t, err)
	match13 := matchGroups["M13"].(*Matchup)
	assert.Equal(t, "L M11", match13.RedAllianceSourceDisplayName())
	assert.Equal(t, "W M12", match13.BlueAllianceSourceDisplayName())

	// Test single-elimination.
	matchup, _, err = newSingleEliminationBracket(5)
	assert.Nil(t, err)

	assert.Equal(t, "W SF1", matchup.RedAllianceSourceDisplayName())
	assert.Equal(t, "W SF2", matchup.BlueAllianceSourceDisplayName())

	matchGroups, err = collectMatchGroups(matchup)
	assert.Nil(t, err)
	sf1 := matchGroups["SF1"].(*Matchup)
	assert.Nil(t, err)
	assert.Equal(t, "A 1", sf1.RedAllianceSourceDisplayName())
	assert.Equal(t, "W QF2", sf1.BlueAllianceSourceDisplayName())
}

func TestMatchupStatusText(t *testing.T) {
	matchup := Matchup{NumWinsToAdvance: 1}

	leader, status := matchup.StatusText()
	assert.Equal(t, "", leader)
	assert.Equal(t, "", status)

	matchup.RedAllianceWins = 1
	leader, status = matchup.StatusText()
	assert.Equal(t, "red", leader)
	assert.Equal(t, "Red Advances 1-0", status)

	matchup.RedAllianceWins = 0
	matchup.BlueAllianceWins = 2
	leader, status = matchup.StatusText()
	assert.Equal(t, "blue", leader)
	assert.Equal(t, "Blue Advances 2-0", status)

	matchup.NumWinsToAdvance = 3
	matchup.BlueAllianceWins = 2
	leader, status = matchup.StatusText()
	assert.Equal(t, "blue", leader)
	assert.Equal(t, "Blue Leads 2-0", status)

	matchup.RedAllianceWins = 2
	leader, status = matchup.StatusText()
	assert.Equal(t, "", leader)
	assert.Equal(t, "Series Tied 2-2", status)

	matchup.BlueAllianceWins = 1
	leader, status = matchup.StatusText()
	assert.Equal(t, "red", leader)
	assert.Equal(t, "Red Leads 2-1", status)

	matchup.id = "F"
	matchup.RedAllianceWins = 3
	leader, status = matchup.StatusText()
	assert.Equal(t, "red", leader)
	assert.Equal(t, "Red Wins 3-1", status)

	matchup.RedAllianceWins = 2
	matchup.BlueAllianceWins = 4
	leader, status = matchup.StatusText()
	assert.Equal(t, "blue", leader)
	assert.Equal(t, "Blue Wins 4-2", status)

	matchup.RedAllianceWins = 0
	matchup.BlueAllianceWins = 0
	leader, status = matchup.StatusText()
	assert.Equal(t, "", leader)
	assert.Equal(t, "", status)
}

func TestMatchupHideUnnecessaryMatches(t *testing.T) {
	qf1 := Matchup{
		id:                 "QF1",
		NumWinsToAdvance:   2,
		redAllianceSource:  allianceSelectionSource{1},
		blueAllianceSource: allianceSelectionSource{8},
		matchSpecs: []*matchSpec{
			newSingleEliminationMatch("Quarterfinal", "QF", 1, 1, 1),
			newSingleEliminationMatch("Quarterfinal", "QF", 1, 2, 5),
			newSingleEliminationMatch("Quarterfinal", "QF", 1, 3, 9),
		},
	}

	matchSpecs, err := collectMatchSpecs(&qf1)
	assert.Nil(t, err)
	for _, matchSpec := range matchSpecs {
		assert.False(t, matchSpec.isHidden)
	}

	playoffMatchResults := map[int]playoffMatchResult{1: {game.BlueWonMatch}}
	qf1.update(playoffMatchResults)
	for _, matchSpec := range matchSpecs {
		assert.False(t, matchSpec.isHidden)
	}

	// Check that the third match is hidden if the first two are won by the same alliance.
	playoffMatchResults[5] = playoffMatchResult{game.BlueWonMatch}
	qf1.update(playoffMatchResults)
	assert.False(t, matchSpecs[0].isHidden)
	assert.False(t, matchSpecs[1].isHidden)
	assert.True(t, matchSpecs[2].isHidden)

	// Check that the third match is unhidden if the prior outcome is reversed.
	playoffMatchResults[5] = playoffMatchResult{game.RedWonMatch}
	qf1.update(playoffMatchResults)
	for _, matchSpec := range matchSpecs {
		assert.False(t, matchSpec.isHidden)
	}
}

func TestMatchupOvertime(t *testing.T) {
	final := Matchup{
		id:                 "F",
		NumWinsToAdvance:   2,
		redAllianceSource:  allianceSelectionSource{1},
		blueAllianceSource: allianceSelectionSource{8},
		matchSpecs:         newFinalMatches(1),
	}

	matchSpecs, err := collectMatchSpecs(&final)
	assert.Nil(t, err)
	for i := 0; i < 3; i++ {
		assert.False(t, matchSpecs[i].isHidden)
	}
	for i := 3; i < 6; i++ {
		assert.True(t, matchSpecs[i].isHidden)
	}

	playoffMatchResults := map[int]playoffMatchResult{1: {game.RedWonMatch}, 2: {game.TieMatch}}
	final.update(playoffMatchResults)
	for i := 0; i < 3; i++ {
		assert.False(t, matchSpecs[i].isHidden)
	}
	for i := 3; i < 6; i++ {
		assert.True(t, matchSpecs[i].isHidden)
	}

	playoffMatchResults[3] = playoffMatchResult{game.BlueWonMatch}
	final.update(playoffMatchResults)
	for i := 0; i < 4; i++ {
		assert.False(t, matchSpecs[i].isHidden)
	}
	for i := 4; i < 6; i++ {
		assert.True(t, matchSpecs[i].isHidden)
	}

	playoffMatchResults[4] = playoffMatchResult{game.TieMatch}
	final.update(playoffMatchResults)
	for i := 0; i < 5; i++ {
		assert.False(t, matchSpecs[i].isHidden)
	}
	for i := 5; i < 6; i++ {
		assert.True(t, matchSpecs[i].isHidden)
	}

	playoffMatchResults[5] = playoffMatchResult{game.BlueWonMatch}
	final.update(playoffMatchResults)
	for i := 0; i < 5; i++ {
		assert.False(t, matchSpecs[i].isHidden)
	}
	for i := 5; i < 6; i++ {
		assert.True(t, matchSpecs[i].isHidden)
	}
}

func TestMatchupSeriesDescription(t *testing.T) {
	upperBracket := &Matchup{}
	for _, test := range []struct {
		name       string
		matchup    Matchup
		wantStatus string
		wantStakes string
	}{
		{"upper bracket single match", Matchup{id: "M1", NumWinsToAdvance: 1, losingAllianceDestination: upperBracket}, "", ""},
		{"elimination single match", Matchup{id: "M5", NumWinsToAdvance: 1}, "", "Loser is eliminated"},
		{"final not started", Matchup{id: "F", NumWinsToAdvance: 2, RedAllianceId: 4, BlueAllianceId: 5}, "Best of 3", ""},
		{
			"final red leads",
			Matchup{id: "F", NumWinsToAdvance: 2, RedAllianceId: 4, BlueAllianceId: 5, RedAllianceWins: 1},
			"Alliance 4 leads 1-0",
			"Alliance 4 can win the event",
		},
		{
			"final blue leads",
			Matchup{id: "F", NumWinsToAdvance: 2, RedAllianceId: 4, BlueAllianceId: 5, BlueAllianceWins: 1},
			"Alliance 5 leads 1-0",
			"Alliance 5 can win the event",
		},
		{
			"final tied",
			Matchup{
				id: "F", NumWinsToAdvance: 2, RedAllianceId: 4, BlueAllianceId: 5, RedAllianceWins: 1, BlueAllianceWins: 1,
			},
			"Series tied 1-1",
			"Winner takes the event",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stakes := test.matchup.SeriesDescription()
			assert.Equal(t, test.wantStatus, status)
			assert.Equal(t, test.wantStakes, stakes)
		})
	}
}
