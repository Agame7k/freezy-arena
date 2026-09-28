// Copyright 2026 Team 254. All Rights Reserved.
//
// Functions for deriving per-conference rankings from the overall qualification rankings.

package tournament

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"sort"
)

// ConferenceRanking is an overall ranking annotated with the team's conference and its rank within that conference.
type ConferenceRanking struct {
	game.Ranking
	ConferenceId           int
	ConferenceRank         int
	PreviousConferenceRank int
}

// ConferenceRankings returns the rankings of the teams in the given conference, ordered by overall rank and annotated
// with their rank within the conference. The previous conference rank is derived in the same way from each team's
// previous overall rank so that rank change arrows keep working. If conferenceId is zero, all teams are returned, each
// annotated with the rank within its own conference.
func ConferenceRankings(rankings game.Rankings, teams []model.Team, conferenceId int) []ConferenceRanking {
	teamConferences := make(map[int]int, len(teams))
	for _, team := range teams {
		teamConferences[team.Id] = team.ConferenceId
	}

	// Compute the current rank within each conference.
	sortedRankings := make(game.Rankings, len(rankings))
	copy(sortedRankings, rankings)
	sort.SliceStable(
		sortedRankings,
		func(i, j int) bool {
			return sortedRankings[i].Rank < sortedRankings[j].Rank
		},
	)
	currentRanks := make(map[int]int)
	counts := make(map[int]int)
	for _, ranking := range sortedRankings {
		conference := teamConferences[ranking.TeamId]
		counts[conference]++
		currentRanks[ranking.TeamId] = counts[conference]
	}

	// Compute the previous rank within each conference, among teams that had a previous rank.
	previousRankings := make(game.Rankings, 0, len(rankings))
	for _, ranking := range rankings {
		if ranking.PreviousRank > 0 {
			previousRankings = append(previousRankings, ranking)
		}
	}
	sort.SliceStable(
		previousRankings,
		func(i, j int) bool {
			return previousRankings[i].PreviousRank < previousRankings[j].PreviousRank
		},
	)
	previousRanks := make(map[int]int)
	previousCounts := make(map[int]int)
	for _, ranking := range previousRankings {
		conference := teamConferences[ranking.TeamId]
		previousCounts[conference]++
		previousRanks[ranking.TeamId] = previousCounts[conference]
	}

	conferenceRankings := make([]ConferenceRanking, 0, len(sortedRankings))
	for _, ranking := range sortedRankings {
		teamConference := teamConferences[ranking.TeamId]
		if conferenceId != 0 && teamConference != conferenceId {
			continue
		}
		conferenceRankings = append(
			conferenceRankings,
			ConferenceRanking{
				Ranking:                ranking,
				ConferenceId:           teamConference,
				ConferenceRank:         currentRanks[ranking.TeamId],
				PreviousConferenceRank: previousRanks[ranking.TeamId],
			},
		)
	}
	return conferenceRankings
}

// FilterRankingsByConference returns the rankings for only the given conference, renumbered so that Rank and
// PreviousRank refer to positions within the conference.
func FilterRankingsByConference(rankings game.Rankings, teams []model.Team, conferenceId int) game.Rankings {
	filtered := game.Rankings{}
	for _, conferenceRanking := range ConferenceRankings(rankings, teams, conferenceId) {
		ranking := conferenceRanking.Ranking
		ranking.Rank = conferenceRanking.ConferenceRank
		ranking.PreviousRank = conferenceRanking.PreviousConferenceRank
		filtered = append(filtered, ranking)
	}
	return filtered
}
