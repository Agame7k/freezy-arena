// Copyright 2026 Team 254. All Rights Reserved.
//
// Helpers for simulating matches without any field hardware or robots, for development and multi-field dry runs.

package field

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"math/rand"
	"time"
)

// RandomScore generates a plausible random score for one alliance.
func RandomScore(random *rand.Rand) game.Score {
	var score game.Score
	for i := range score.AutoTowerStatuses {
		score.AutoTowerStatuses[i] = game.TowerStatus(random.Intn(2))
		score.EndgameTowerStatuses[i] = game.TowerStatus(random.Intn(4))
	}
	for shift := range score.Hub.ShiftCounts {
		score.Hub.ShiftCounts[shift] = random.Intn(25)
	}
	return score
}

// PrepareSimulatedMatch makes sure a match is loaded (loading or claiming the next one if the current match is a test
// match or has already been played) and fills in random realtime scores for it, as though it had just been played.
func (arena *Arena) PrepareSimulatedMatch(random *rand.Rand, matchType model.MatchType) error {
	if !arena.SimulateHardware {
		return fmt.Errorf("match simulation is only available when running with -simulate")
	}
	if arena.EventSettings.IsHub() {
		return fmt.Errorf("the hub has no field of its own; simulate matches on the field nodes")
	}
	if arena.MatchState != PreMatch && arena.MatchState != PostMatch {
		return fmt.Errorf("can't simulate a match while one is in progress")
	}
	if arena.MatchState == PostMatch {
		if err := arena.ResetMatch(); err != nil {
			return err
		}
	}

	if arena.CurrentMatch.Type != model.Test {
		// Pick up any changes to the loaded match (e.g. a playoff match filled in by the hub since it was loaded).
		if match, err := arena.Database.GetMatchById(arena.CurrentMatch.Id); err == nil && match != nil &&
			(!match.IsLineupEqual(
				arena.CurrentMatch.Red1, arena.CurrentMatch.Red2, arena.CurrentMatch.Red3,
				arena.CurrentMatch.Blue1, arena.CurrentMatch.Blue2, arena.CurrentMatch.Blue3,
			) || match.PlayoffRedAlliance != arena.CurrentMatch.PlayoffRedAlliance ||
				match.PlayoffBlueAlliance != arena.CurrentMatch.PlayoffBlueAlliance || !arena.matchIsOnThisField(match)) {
			if err = arena.LoadMatch(match); err != nil {
				// The match may have been moved to the other field; move on to the next one.
				if err = arena.LoadTestMatch(); err != nil {
					return err
				}
			}
		}
	}
	notReady := arena.CurrentMatch.Type == model.Playoff &&
		(arena.CurrentMatch.PlayoffRedAlliance == 0 || arena.CurrentMatch.PlayoffBlueAlliance == 0)
	if arena.CurrentMatch.Type == model.Test || arena.CurrentMatch.IsComplete() || notReady ||
		arena.CurrentMatch.Type != matchType {
		if err := arena.loadNextSimulatedMatch(matchType); err != nil {
			return err
		}
	}
	if arena.CurrentMatch.Type == model.Test {
		return fmt.Errorf("there are no more %s matches to simulate on this field", matchType)
	}

	arena.CurrentMatch.StartedAt = time.Now()
	arena.RedRealtimeScore.CurrentScore = RandomScore(random)
	arena.BlueRealtimeScore.CurrentScore = RandomScore(random)
	arena.RedRealtimeScore.FoulsCommitted = true
	arena.BlueRealtimeScore.FoulsCommitted = true
	arena.RealtimeScoreNotifier.Notify()
	return nil
}

func (arena *Arena) loadNextSimulatedMatch(matchType model.MatchType) error {
	matches, err := arena.Database.GetMatchesByType(matchType, false)
	if err != nil {
		return err
	}
	for _, match := range arena.FilterMatchesForField(matches) {
		if !match.IsComplete() && (match.Type != model.Playoff || match.PlayoffRedAlliance > 0 && match.PlayoffBlueAlliance > 0) {
			return arena.LoadMatch(&match)
		}
	}
	if arena.usesDynamicClaims(matchType) {
		return arena.LoadNextAvailableMatch()
	}
	return arena.LoadTestMatch()
}
