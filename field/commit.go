// Copyright 2026 Team 254. All Rights Reserved.
//
// Shared pipeline for saving a committed match result and applying its consequences (cards, rankings, playoff
// progression, awards, publishing and backups). Used both by a standalone FMS and by a multi-field hub receiving results
// from its nodes.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"log"
	"time"
)

// PrepareMatchResult applies the scoring adjustments common to every commit path: it corrects a playoff score for any
// disqualifications, stamps the commit time and determines the match status from the scores.
func PrepareMatchResult(match *model.Match, matchResult *model.MatchResult) {
	if match.Type == model.Playoff {
		// Adjust the score if necessary for a playoff DQ.
		matchResult.CorrectPlayoffScore()
	}
	match.ScoreCommittedAt = time.Now()
	redScoreSummary := matchResult.RedScoreSummary()
	blueScoreSummary := matchResult.BlueScoreSummary()
	match.Status, _ = game.DetermineMatchStatus(redScoreSummary, blueScoreSummary, match.UseTiebreakCriteria)
}

// SaveMatchResult persists the match and its result, assigning the next play number to a new result.
func (arena *Arena) SaveMatchResult(match *model.Match, matchResult *model.MatchResult) error {
	if matchResult.PlayNumber == 0 {
		// Determine the play number for this new match result.
		prevMatchResult, err := arena.Database.GetMatchResultForMatch(match.Id)
		if err != nil {
			return err
		}
		if prevMatchResult != nil {
			matchResult.PlayNumber = prevMatchResult.PlayNumber + 1
		} else {
			matchResult.PlayNumber = 1
		}

		// Save the match result record to the database.
		if err = arena.Database.CreateMatchResult(matchResult); err != nil {
			return err
		}
	} else if matchResult.Id == 0 {
		// A result with a predetermined play number (e.g. received from a node).
		if err := arena.Database.CreateMatchResult(matchResult); err != nil {
			return err
		}
	} else {
		// We are updating a match result record that already exists.
		if err := arena.Database.UpdateMatchResult(matchResult); err != nil {
			return err
		}
	}
	return arena.Database.UpdateMatch(match)
}

// ApplyCommittedResult saves the given match and result to the database, supplanting any previous result for the
// match, and then runs every post-commit step. It returns the updated rankings if the match affects them.
func (arena *Arena) ApplyCommittedResult(
	match *model.Match, matchResult *model.MatchResult, isMatchReviewEdit bool,
) (game.Rankings, error) {
	return arena.ApplyCommittedResultAt(match, matchResult, isMatchReviewEdit, time.Time{})
}

// ApplyCommittedResultAt is like ApplyCommittedResult but records the given time (e.g. when a field node committed the
// result, which may be well before the hub receives it after an outage) as the commit time, unless it is zero or in the
// future.
func (arena *Arena) ApplyCommittedResultAt(
	match *model.Match, matchResult *model.MatchResult, isMatchReviewEdit bool, committedAt time.Time,
) (game.Rankings, error) {
	PrepareMatchResult(match, matchResult)
	if !committedAt.IsZero() && committedAt.Before(match.ScoreCommittedAt) {
		match.ScoreCommittedAt = committedAt
	}
	if match.Type == model.Test {
		return nil, nil
	}
	if err := arena.SaveMatchResult(match, matchResult); err != nil {
		return nil, err
	}
	return arena.RunPostCommitSteps(match, isMatchReviewEdit)
}

// RunPostCommitSteps updates cards, rankings, playoff matches and awards following a saved match result, publishes
// results if configured, and backs up the database.
func (arena *Arena) RunPostCommitSteps(match *model.Match, isMatchReviewEdit bool) (game.Rankings, error) {
	var updatedRankings game.Rankings

	if match.ShouldUpdateCards() {
		// Regenerate the residual yellow cards that teams may carry.
		if err := tournament.CalculateTeamCards(arena.Database, match.Type); err != nil {
			return nil, err
		}
	}

	if match.ShouldUpdateRankings() {
		// Recalculate all the rankings.
		rankings, err := tournament.CalculateRankings(arena.Database, isMatchReviewEdit)
		if err != nil {
			return nil, err
		}
		updatedRankings = rankings
	}

	if match.ShouldUpdatePlayoffMatches() {
		if err := arena.Database.UpdateAllianceFromMatch(
			match.PlayoffRedAlliance, [3]int{match.Red1, match.Red2, match.Red3},
		); err != nil {
			return nil, err
		}
		if err := arena.Database.UpdateAllianceFromMatch(
			match.PlayoffBlueAlliance, [3]int{match.Blue1, match.Blue2, match.Blue3},
		); err != nil {
			return nil, err
		}

		// Populate any subsequent playoff matches.
		if err := arena.UpdatePlayoffTournament(); err != nil {
			return nil, err
		}

		if err := arena.updatePlayoffAwards(match); err != nil {
			return nil, err
		}
	}

	if arena.EventSettings.TbaPublishingEnabled && match.Type != model.Practice {
		// Publish asynchronously to The Blue Alliance.
		go func() {
			if err := arena.TbaClient.PublishMatches(arena.Database); err != nil {
				log.Printf("Failed to publish matches: %s", err.Error())
			}
			if match.ShouldUpdateRankings() {
				if err := arena.TbaClient.PublishRankings(arena.Database); err != nil {
					log.Printf("Failed to publish rankings: %s", err.Error())
				}
			}
		}()
	}

	if arena.EventSettings.NexusAutoQueueEnabled && !isMatchReviewEdit {
		// Trigger Nexus AutoQueue asynchronously, ignoring errors.
		go func() {
			arena.NexusClient.AutoQueue(match.LongName, match.TypeOrder, match.Status)
		}()
	}

	// Back up the database, but don't error out if it fails.
	err := arena.Database.Backup(
		arena.EventSettings.Name, fmt.Sprintf("post_%s_match_%s", match.Type, match.ShortName),
	)
	if err != nil {
		log.Println(err)
	}

	arena.notifyDataChanged()
	return updatedRankings, nil
}

// Generates the winner and finalist awards for any bracket that the given playoff match completed.
func (arena *Arena) updatePlayoffAwards(match *model.Match) error {
	playoffTournament := arena.PlayoffTournament
	if !playoffTournament.IsMultiConference() {
		// Generate awards if the tournament is over.
		if playoffTournament.IsComplete() {
			return tournament.CreateOrUpdateWinnerAndFinalistAwards(
				arena.Database, playoffTournament.WinningAllianceId(), playoffTournament.FinalistAllianceId(),
			)
		}
		return nil
	}

	for _, conference := range playoffTournament.Conferences() {
		if match.ConferenceId != conference.Id || !playoffTournament.IsConferenceComplete(conference.Id) {
			continue
		}
		final := playoffTournament.ConferenceFinal(conference.Id)
		if err := tournament.CreateOrUpdateConferenceAwards(
			arena.Database, &conference, final.WinningAllianceId(), final.LosingAllianceId(),
		); err != nil {
			return err
		}
	}
	if match.ConferenceId == 0 && playoffTournament.FinalMatchup() != nil && playoffTournament.IsComplete() {
		return tournament.CreateOrUpdateEventChampionAwards(
			arena.Database, playoffTournament.WinningAllianceId(), playoffTournament.FinalistAllianceId(),
		)
	}
	return nil
}
