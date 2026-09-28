// Copyright 2026 Team 254. All Rights Reserved.

package playoff

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strings"
	"testing"
	"time"
)

var stressChampionshipFormats = []model.ChampionshipFormat{
	model.ChampionsSeriesChampionship,
	model.CrossoverSemisChampionship,
	model.DoubleDeckerChampionship,
	model.NoChampionship,
}

// Every pair of plausible conference short names must produce a tournament without clashing match names, whatever the
// brackets and championship format.
func TestMultiConferenceShortNamePairs(t *testing.T) {
	shortNames := []string{
		"A", "B", "C", "D", "E", "F", "L", "M", "N", "O", "Q", "S", "T", "W", "X", "Z", "CH", "CC", "CS", "CO", "NM",
		"CM", "SF", "QF", "EF", "MF", "OT", "NMR", "CMR", "FIN", "M1A", "n", "cm",
	}
	brackets := []struct {
		playoffType  model.PlayoffType
		numAlliances int
	}{
		{model.DoubleEliminationPlayoff, 8},
		{model.DoubleEliminationPlayoff, 4},
		{model.SingleEliminationPlayoff, 16},
		{model.SingleEliminationPlayoff, 2},
	}
	for _, first := range shortNames {
		for _, second := range shortNames {
			if strings.EqualFold(first, second) {
				continue
			}
			for _, bracket := range brackets {
				for _, format := range stressChampionshipFormats {
					conferences := testConferences(bracket.playoffType, bracket.numAlliances, bracket.playoffType, 8)
					conferences[0].ShortName, conferences[1].ShortName = first, second
					_, err := NewMultiConferencePlayoffTournament(
						conferences, MultiConferenceOptions{ChampionshipFormat: format, ChampionshipSeriesLength: 3},
					)
					assert.Nil(t, err, "%s/%s %v-%d %v", first, second, bracket.playoffType, bracket.numAlliances, format)
				}
			}
		}
	}
}

func TestMultiConferenceRejectsAmbiguousShortNames(t *testing.T) {
	for _, names := range [][2]string{{"N", "n"}, {"N1", "N"}, {"S", "S2"}, {"CM", "cm"}} {
		conferences := testConferences(model.DoubleEliminationPlayoff, 8, model.DoubleEliminationPlayoff, 8)
		conferences[0].ShortName, conferences[1].ShortName = names[0], names[1]
		_, err := NewMultiConferencePlayoffTournament(conferences, MultiConferenceOptions{})
		assert.NotNil(t, err, "%v", names)
	}
}

// Plays random tournaments (random winners, occasional ties, mixed series lengths) for every bracket combination and
// championship format and checks that each one finishes with a champion from the right place.
func TestMultiConferenceRandomPlaythroughs(t *testing.T) {
	type bracketConfig struct {
		playoffType  model.PlayoffType
		numAlliances int
	}
	var bracketConfigs []bracketConfig
	for _, numAlliances := range []int{2, 3, 5, 8, 11, 16} {
		bracketConfigs = append(bracketConfigs, bracketConfig{model.SingleEliminationPlayoff, numAlliances})
	}
	for numAlliances := 4; numAlliances <= 8; numAlliances++ {
		bracketConfigs = append(bracketConfigs, bracketConfig{model.DoubleEliminationPlayoff, numAlliances})
	}
	seriesOptions := []map[string]int{
		{"EF": 1, "QF": 1, "SF": 1, "F": 1},
		{"EF": 1, "QF": 3, "SF": 3, "F": 5},
		{"EF": 3, "QF": 1, "SF": 5, "F": 3},
	}

	random := rand.New(rand.NewSource(254))
	for i, first := range bracketConfigs {
		second := bracketConfigs[(i*7+3)%len(bracketConfigs)]
		for _, format := range stressChampionshipFormats {
			conferences := testConferences(
				first.playoffType, first.numAlliances, second.playoffType, second.numAlliances,
			)
			conferences[0].SeriesLengths = seriesOptions[random.Intn(len(seriesOptions))]
			conferences[1].SeriesLengths = seriesOptions[random.Intn(len(seriesOptions))]
			championshipSeries := []int{1, 3, 5}[random.Intn(3)]
			name := fmt.Sprintf(
				"%v-%d/%v-%d/%v/Bo%d", first.playoffType, first.numAlliances, second.playoffType,
				second.numAlliances, format, championshipSeries,
			)
			tournament, err := NewMultiConferencePlayoffTournament(
				conferences,
				MultiConferenceOptions{
					ChampionshipFormat: format, ChampionshipSeriesLength: championshipSeries, MultiField: true,
				},
			)
			if !assert.Nil(t, err, name) {
				continue
			}
			database := setupTestDb(t)
			createTestConferenceAlliances(t, database, conferences)
			assert.Nil(t, tournament.CreateMatchesAndBreaks(database, time.Unix(1000, 0)), name)
			assert.Nil(t, tournament.UpdateMatches(database), name)
			playRandomTournament(t, database, tournament, random, name)

			firstAllianceIds := map[int]bool{}
			for id := 1; id <= first.numAlliances; id++ {
				firstAllianceIds[id] = true
			}
			assert.True(t, tournament.IsConferenceComplete(1), name)
			assert.True(t, tournament.IsConferenceComplete(2), name)
			assert.True(t, firstAllianceIds[tournament.ConferenceFinal(1).WinningAllianceId()], name)
			assert.False(t, firstAllianceIds[tournament.ConferenceFinal(2).WinningAllianceId()], name)
			if format == model.NoChampionship {
				assert.Equal(t, 0, tournament.WinningAllianceId(), name)
			} else {
				assert.NotEqual(t, 0, tournament.WinningAllianceId(), name)
				assert.NotEqual(t, 0, tournament.FinalistAllianceId(), name)
				assert.NotEqual(t, tournament.WinningAllianceId(), tournament.FinalistAllianceId(), name)
			}

			// Every playoff match that was played must have had both alliances and the matching teams.
			matches, err := database.GetMatchesByType(model.Playoff, true)
			assert.Nil(t, err)
			for _, match := range matches {
				if match.IsComplete() {
					assert.NotZero(t, match.PlayoffRedAlliance, "%s %s", name, match.ShortName)
					assert.NotZero(t, match.PlayoffBlueAlliance, "%s %s", name, match.ShortName)
					assert.Equal(t, 100*match.PlayoffRedAlliance+2, match.Red1, "%s %s", name, match.ShortName)
					assert.Equal(t, 100*match.PlayoffBlueAlliance+2, match.Blue1, "%s %s", name, match.ShortName)
				}
			}
		}
	}
}

// Plays every match of the tournament with a random result (ties are only used where tiebreakers don't apply).
func playRandomTournament(
	t *testing.T, database *model.Database, tournament *PlayoffTournament, random *rand.Rand, name string,
) {
	for iterations := 0; iterations < 1000 && !tournament.IsComplete(); iterations++ {
		matches, err := database.GetMatchesByType(model.Playoff, false)
		assert.Nil(t, err)
		var playable []*model.Match
		for i, match := range matches {
			if !match.IsComplete() && match.PlayoffRedAlliance > 0 && match.PlayoffBlueAlliance > 0 {
				playable = append(playable, &matches[i])
			}
		}
		if len(playable) == 0 {
			if tournament.FinalMatchup() == nil && tournament.IsConferenceComplete(1) &&
				tournament.IsConferenceComplete(2) {
				return
			}
			assert.Fail(t, "no playable match found before the tournament completed", name)
			return
		}
		// Play any playable match, not always the first, as two fields would.
		next := playable[random.Intn(len(playable))]
		switch roll := random.Intn(10); {
		case roll == 0 && !next.UseTiebreakCriteria:
			next.Status = game.TieMatch
		case roll < 5:
			next.Status = game.RedWonMatch
		default:
			next.Status = game.BlueWonMatch
		}
		assert.Nil(t, database.UpdateMatch(next))
		assert.Nil(t, tournament.UpdateMatches(database), name)
	}
	if tournament.FinalMatchup() != nil {
		assert.True(t, tournament.IsComplete(), name)
	}
}
