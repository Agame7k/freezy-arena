// Copyright 2026 Team 254. All Rights Reserved.

package tournament

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConferenceRankings(t *testing.T) {
	teams := []model.Team{
		{Id: 1, ConferenceId: 1}, {Id: 2, ConferenceId: 2}, {Id: 3, ConferenceId: 1}, {Id: 4, ConferenceId: 2},
		{Id: 5, ConferenceId: 1},
	}
	rankings := game.Rankings{
		{TeamId: 2, Rank: 1, PreviousRank: 3},
		{TeamId: 3, Rank: 2, PreviousRank: 1},
		{TeamId: 1, Rank: 3, PreviousRank: 2},
		{TeamId: 5, Rank: 4, PreviousRank: 0},
		{TeamId: 4, Rank: 5, PreviousRank: 4},
	}

	north := ConferenceRankings(rankings, teams, 1)
	if assert.Equal(t, 3, len(north)) {
		assert.Equal(t, 3, north[0].TeamId)
		assert.Equal(t, 1, north[0].ConferenceRank)
		assert.Equal(t, 1, north[0].PreviousConferenceRank)
		assert.Equal(t, 2, north[0].Rank)
		assert.Equal(t, 1, north[1].TeamId)
		assert.Equal(t, 2, north[1].ConferenceRank)
		assert.Equal(t, 2, north[1].PreviousConferenceRank)
		assert.Equal(t, 5, north[2].TeamId)
		assert.Equal(t, 3, north[2].ConferenceRank)
		assert.Equal(t, 0, north[2].PreviousConferenceRank)
	}

	south := ConferenceRankings(rankings, teams, 2)
	if assert.Equal(t, 2, len(south)) {
		assert.Equal(t, 2, south[0].TeamId)
		assert.Equal(t, 1, south[0].ConferenceRank)
		// Team 2 was previously ranked 3rd overall, behind no other South team.
		assert.Equal(t, 1, south[0].PreviousConferenceRank)
		assert.Equal(t, 4, south[1].TeamId)
		assert.Equal(t, 2, south[1].PreviousConferenceRank)
	}

	all := ConferenceRankings(rankings, teams, 0)
	if assert.Equal(t, 5, len(all)) {
		assert.Equal(t, 2, all[0].ConferenceId)
		assert.Equal(t, 1, all[0].ConferenceRank)
		assert.Equal(t, 1, all[1].ConferenceRank)
		assert.Equal(t, 2, all[4].ConferenceRank)
	}

	filtered := FilterRankingsByConference(rankings, teams, 1)
	if assert.Equal(t, 3, len(filtered)) {
		assert.Equal(t, game.Ranking{TeamId: 3, Rank: 1, PreviousRank: 1}, filtered[0])
	}
}
