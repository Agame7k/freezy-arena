// Copyright 2026 Team 254. All Rights Reserved.

package playoff

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
	"time"
)

func testConferences(
	firstType model.PlayoffType, firstAlliances int, secondType model.PlayoffType, secondAlliances int,
) []model.Conference {
	return []model.Conference{
		{
			Id:             1,
			Name:           "North",
			ShortName:      "N",
			PlayoffType:    firstType,
			NumAlliances:   firstAlliances,
			SeriesLengths:  map[string]int{"EF": 3, "QF": 3, "SF": 3, "F": 3},
			PlayoffFieldId: 1,
		},
		{
			Id:             2,
			Name:           "South",
			ShortName:      "S",
			PlayoffType:    secondType,
			NumAlliances:   secondAlliances,
			SeriesLengths:  map[string]int{"EF": 3, "QF": 3, "SF": 3, "F": 3},
			PlayoffFieldId: 2,
		},
	}
}

// Creates alliances for both conferences with consecutive global IDs.
func createTestConferenceAlliances(t *testing.T, database *model.Database, conferences []model.Conference) {
	id := 1
	for _, conference := range conferences {
		for seed := 1; seed <= conference.NumAlliances; seed++ {
			alliance := model.Alliance{
				Id:           id,
				TeamIds:      []int{100*id + 1, 100*id + 2, 100*id + 3},
				Lineup:       [3]int{100*id + 2, 100*id + 1, 100*id + 3},
				ConferenceId: conference.Id,
				Seed:         seed,
			}
			assert.Nil(t, database.CreateAlliance(&alliance))
			id++
		}
	}
}

// Plays every match of the tournament, letting the lower-numbered alliance win, and returns the number of matches
// played.
func playTournament(t *testing.T, database *model.Database, tournament *PlayoffTournament) int {
	played := 0
	for iterations := 0; iterations < 500 && !tournament.IsComplete(); iterations++ {
		matches, err := database.GetMatchesByType(model.Playoff, false)
		assert.Nil(t, err)
		var next *model.Match
		for i, match := range matches {
			if !match.IsComplete() && match.PlayoffRedAlliance > 0 && match.PlayoffBlueAlliance > 0 {
				next = &matches[i]
				break
			}
		}
		if !assert.NotNil(t, next, "no playable match found before the tournament completed") {
			return played
		}
		if next.PlayoffRedAlliance < next.PlayoffBlueAlliance {
			next.Status = game.RedWonMatch
		} else {
			next.Status = game.BlueWonMatch
		}
		assert.Nil(t, database.UpdateMatch(next))
		assert.Nil(t, tournament.UpdateMatches(database))
		played++
	}
	assert.True(t, tournament.IsComplete())
	return played
}

func TestMultiConferenceTournamentAllCombinations(t *testing.T) {
	type bracketConfig struct {
		playoffType  model.PlayoffType
		numAlliances int
	}
	var bracketConfigs []bracketConfig
	for numAlliances := 2; numAlliances <= 16; numAlliances++ {
		bracketConfigs = append(bracketConfigs, bracketConfig{model.SingleEliminationPlayoff, numAlliances})
	}
	for numAlliances := 4; numAlliances <= 8; numAlliances++ {
		bracketConfigs = append(bracketConfigs, bracketConfig{model.DoubleEliminationPlayoff, numAlliances})
	}
	championshipFormats := []model.ChampionshipFormat{
		model.ChampionsSeriesChampionship,
		model.CrossoverSemisChampionship,
		model.DoubleDeckerChampionship,
		model.NoChampionship,
	}

	for _, first := range bracketConfigs {
		for _, second := range []bracketConfig{
			{model.SingleEliminationPlayoff, 4}, {model.DoubleEliminationPlayoff, 8}, first,
		} {
			for _, format := range championshipFormats {
				name := fmt.Sprintf("%v-%d_%v-%d_%v", first.playoffType, first.numAlliances, second.playoffType,
					second.numAlliances, format)
				conferences := testConferences(
					first.playoffType, first.numAlliances, second.playoffType, second.numAlliances,
				)
				tournament, err := NewMultiConferencePlayoffTournament(
					conferences, MultiConferenceOptions{ChampionshipFormat: format, ChampionshipSeriesLength: 3},
				)
				if !assert.Nil(t, err, name) {
					continue
				}

				// Every match must have a unique order, and names must carry the conference prefix.
				orders := map[int]struct{}{}
				for _, spec := range tournament.matchSpecs {
					_, duplicate := orders[spec.order]
					assert.False(t, duplicate, name)
					orders[spec.order] = struct{}{}
					switch spec.conferenceId {
					case 1:
						assert.True(t, strings.HasPrefix(spec.longName, "North "), spec.longName)
						assert.True(t, strings.HasPrefix(spec.shortName, "N"), spec.shortName)
					case 2:
						assert.True(t, strings.HasPrefix(spec.longName, "South "), spec.longName)
						assert.True(t, strings.HasPrefix(spec.shortName, "S"), spec.shortName)
					default:
						assert.True(t, strings.HasPrefix(spec.longName, "Championship"), spec.longName)
						assert.Greater(t, spec.order, championshipOrderOffset)
					}
				}
				if format == model.NoChampionship {
					assert.Nil(t, tournament.FinalMatchup())
				} else {
					assert.Equal(t, "F", tournament.FinalMatchup().Id())
				}
			}
		}
	}
}

func TestMultiConferenceTournamentPlaysToChampion(t *testing.T) {
	for _, format := range []model.ChampionshipFormat{
		model.ChampionsSeriesChampionship,
		model.CrossoverSemisChampionship,
		model.DoubleDeckerChampionship,
		model.NoChampionship,
	} {
		for _, config := range []struct {
			firstType       model.PlayoffType
			firstAlliances  int
			secondType      model.PlayoffType
			secondAlliances int
		}{
			{model.SingleEliminationPlayoff, 10, model.DoubleEliminationPlayoff, 8},
			{model.DoubleEliminationPlayoff, 4, model.SingleEliminationPlayoff, 2},
			{model.SingleEliminationPlayoff, 16, model.SingleEliminationPlayoff, 5},
		} {
			database := setupTestDb(t)
			conferences := testConferences(
				config.firstType, config.firstAlliances, config.secondType, config.secondAlliances,
			)
			tournament, err := NewMultiConferencePlayoffTournament(
				conferences, MultiConferenceOptions{ChampionshipFormat: format, ChampionshipSeriesLength: 3},
			)
			if !assert.Nil(t, err) {
				continue
			}
			createTestConferenceAlliances(t, database, conferences)
			startTime := time.Unix(1000, 0)
			assert.Nil(t, tournament.CreateMatchesAndBreaks(database, startTime))
			assert.Nil(t, tournament.UpdateMatches(database))

			playTournament(t, database, tournament)
			assert.True(t, tournament.IsConferenceComplete(1))
			assert.True(t, tournament.IsConferenceComplete(2))

			// The lower-numbered alliance always wins, so each conference is won by its first seed.
			assert.Equal(t, 1, tournament.ConferenceFinal(1).WinningAllianceId())
			assert.Equal(t, config.firstAlliances+1, tournament.ConferenceFinal(2).WinningAllianceId())
			if format == model.NoChampionship {
				assert.Equal(t, 0, tournament.WinningAllianceId())
			} else {
				assert.Equal(t, 1, tournament.WinningAllianceId())
				assert.NotEqual(t, 0, tournament.FinalistAllianceId())
			}
		}
	}
}

func TestMultiConferenceAllianceOffsetsAndLabels(t *testing.T) {
	conferences := testConferences(model.DoubleEliminationPlayoff, 8, model.DoubleEliminationPlayoff, 6)
	tournament, err := NewMultiConferencePlayoffTournament(conferences, MultiConferenceOptions{})
	assert.Nil(t, err)

	// The first round of the second conference's six-alliance bracket pits its fourth and fifth seeds (alliances 12 and 13).
	matchup := tournament.MatchGroups()["S-M1"].(*Matchup)
	assert.Equal(t, 8+4, matchup.RedAllianceId)
	assert.Equal(t, 8+5, matchup.BlueAllianceId)
	matchup = tournament.MatchGroups()["N-M1"].(*Matchup)
	assert.Equal(t, 1, matchup.RedAllianceId)
	assert.Equal(t, 8, matchup.BlueAllianceId)

	assert.Equal(t, "N1", tournament.AllianceLabel(1))
	assert.Equal(t, "N8", tournament.AllianceLabel(8))
	assert.Equal(t, "S1", tournament.AllianceLabel(9))
	assert.Equal(t, "S6", tournament.AllianceLabel(14))
	assert.Equal(t, 2, tournament.AllianceConferenceId(14))
	assert.Equal(t, "W N-M1", tournament.MatchGroups()["N-M7"].(*Matchup).RedAllianceSourceDisplayName())
}

func TestMultiConferenceRunnerUpSources(t *testing.T) {
	conferences := testConferences(model.SingleEliminationPlayoff, 2, model.SingleEliminationPlayoff, 2)
	tournament, err := NewMultiConferencePlayoffTournament(
		conferences,
		MultiConferenceOptions{ChampionshipFormat: model.CrossoverSemisChampionship, ChampionshipSeriesLength: 1},
	)
	assert.Nil(t, err)
	semifinal1 := tournament.MatchGroups()["CS1"].(*Matchup)
	semifinal2 := tournament.MatchGroups()["CS2"].(*Matchup)
	assert.Equal(t, "W N-F", semifinal1.RedAllianceSourceDisplayName())
	assert.Equal(t, "L S-F", semifinal1.BlueAllianceSourceDisplayName())
	assert.Equal(t, "W S-F", semifinal2.RedAllianceSourceDisplayName())
	assert.Equal(t, "L N-F", semifinal2.BlueAllianceSourceDisplayName())

	database := setupTestDb(t)
	createTestConferenceAlliances(t, database, conferences)
	assert.Nil(t, tournament.CreateMatchesAndBreaks(database, time.Unix(0, 0)))
	playTournament(t, database, tournament)
	// N1 (1) beats N2 (2); S1 (3) beats S2 (4). CS1: 1 vs 4, CS2: 3 vs 2. Final: 1 vs 2.
	assert.Equal(t, 1, tournament.WinningAllianceId())
	assert.Equal(t, 2, tournament.FinalistAllianceId())
	// The conference runner-up is not eliminated since it plays in a crossover semifinal.
	assert.Equal(t, "Advances to Championship Semifinal 2-1", tournament.ConferenceFinal(1).BlueAllianceDestination())
}

func TestMultiConferenceSeriesLengths(t *testing.T) {
	conferences := testConferences(model.SingleEliminationPlayoff, 8, model.SingleEliminationPlayoff, 8)
	conferences[0].SeriesLengths = map[string]int{"QF": 1, "SF": 3, "F": 5}
	conferences[1].SeriesLengths = map[string]int{"QF": 5, "SF": 1, "F": 1}
	tournament, err := NewMultiConferencePlayoffTournament(
		conferences, MultiConferenceOptions{ChampionshipFormat: model.ChampionsSeriesChampionship,
			ChampionshipSeriesLength: 5},
	)
	assert.Nil(t, err)

	groups := tournament.MatchGroups()
	assert.Equal(t, 1, len(groups["N-QF1"].MatchSpecs()))
	assert.Equal(t, 1, groups["N-QF1"].(*Matchup).NumWinsToAdvance)
	assert.True(t, groups["N-QF1"].MatchSpecs()[0].useTiebreakCriteria)
	assert.Equal(t, 3, len(groups["N-SF1"].MatchSpecs()))
	assert.Equal(t, 5+3, len(groups["N-F"].MatchSpecs()))
	assert.Equal(t, 3, groups["N-F"].(*Matchup).NumWinsToAdvance)
	assert.False(t, groups["N-F"].MatchSpecs()[2].isHidden)
	assert.True(t, groups["N-F"].MatchSpecs()[3].isHidden)

	assert.Equal(t, 5, len(groups["S-QF1"].MatchSpecs()))
	assert.Equal(t, 3, groups["S-QF1"].(*Matchup).NumWinsToAdvance)
	assert.True(t, groups["S-QF1"].MatchSpecs()[4].isHidden)
	assert.Equal(t, 1, len(groups["S-SF1"].MatchSpecs()))
	assert.Equal(t, 1, len(groups["S-F"].MatchSpecs()))
	assert.True(t, groups["S-F"].MatchSpecs()[0].useTiebreakCriteria)

	assert.Equal(t, 5+3, len(groups["F"].MatchSpecs()))
	assert.Equal(t, "Championship 1", groups["F"].MatchSpecs()[0].longName)
	assert.Equal(t, "CO1", groups["F"].MatchSpecs()[5].shortName)

	database := setupTestDb(t)
	createTestConferenceAlliances(t, database, conferences)
	assert.Nil(t, tournament.CreateMatchesAndBreaks(database, time.Unix(0, 0)))
	playTournament(t, database, tournament)
	assert.Equal(t, 1, tournament.WinningAllianceId())
	assert.Equal(t, 9, tournament.FinalistAllianceId())
}

func TestMultiConferenceTimesPerField(t *testing.T) {
	conferences := testConferences(model.SingleEliminationPlayoff, 2, model.SingleEliminationPlayoff, 2)
	conferences[0].SeriesLengths = map[string]int{"F": 1}
	conferences[1].SeriesLengths = map[string]int{"F": 1}
	options := MultiConferenceOptions{
		ChampionshipFormat:       model.ChampionsSeriesChampionship,
		ChampionshipSeriesLength: 1,
		ChampionshipFieldMode:    model.ChampionshipFieldFixed2,
		ChampionshipBreakSec:     600,
		MultiField:               true,
	}
	tournament, err := NewMultiConferencePlayoffTournament(conferences, options)
	assert.Nil(t, err)

	database := setupTestDb(t)
	createTestConferenceAlliances(t, database, conferences)
	firstStart := time.Unix(10000, 0)
	secondStart := time.Unix(12000, 0)
	assert.Nil(
		t,
		tournament.CreateMatchesAndBreaksWithStartTimes(
			database, firstStart, map[int]time.Time{1: firstStart, 2: secondStart},
		),
	)

	matches, err := database.GetMatchesByType(model.Playoff, true)
	assert.Nil(t, err)
	if assert.Equal(t, 3, len(matches)) {
		assert.Equal(t, "North Final 1", matches[0].LongName)
		assert.Equal(t, 1, matches[0].FieldId)
		assert.Equal(t, 1, matches[0].ConferenceId)
		assert.Equal(t, firstStart, matches[0].Time)
		assert.Equal(t, "South Final 1", matches[1].LongName)
		assert.Equal(t, 2, matches[1].FieldId)
		assert.Equal(t, 2, matches[1].ConferenceId)
		assert.Equal(t, secondStart, matches[1].Time)
		// The championship starts after the later conference final ends, plus the showcase break.
		assert.Equal(t, "Championship 1", matches[2].LongName)
		assert.Equal(t, 2, matches[2].FieldId)
		assert.Equal(t, 0, matches[2].ConferenceId)
		assert.Equal(t, secondStart.Add(300*time.Second).Add(600*time.Second), matches[2].Time)
	}
	breaks, err := database.GetScheduledBreaksByMatchType(model.Playoff)
	assert.Nil(t, err)
	if assert.Equal(t, 1, len(breaks)) {
		assert.Equal(t, "Championship Showcase", breaks[0].Description)
		assert.Equal(t, 2, breaks[0].FieldId)
		assert.Equal(t, secondStart.Add(300*time.Second), breaks[0].Time)
	}

	// Rescheduling the second conference moves its matches and the championship.
	laterStart := secondStart.Add(time.Hour)
	assert.Nil(
		t,
		tournament.RescheduleMatchesAndBreaks(database, firstStart, map[int]time.Time{1: firstStart, 2: laterStart}),
	)
	matches, _ = database.GetMatchesByType(model.Playoff, true)
	assert.Equal(t, firstStart, matches[0].Time)
	assert.Equal(t, laterStart, matches[1].Time)
	assert.Equal(t, laterStart.Add(900*time.Second), matches[2].Time)
}

func TestMultiConferenceInterleavedOnSharedField(t *testing.T) {
	conferences := testConferences(model.DoubleEliminationPlayoff, 4, model.DoubleEliminationPlayoff, 4)
	conferences[1].PlayoffFieldId = 1
	tournament, err := NewMultiConferencePlayoffTournament(
		conferences,
		MultiConferenceOptions{
			ChampionshipFormat: model.NoChampionship, MultiField: true, InterleaveOnSharedField: true,
		},
	)
	assert.Nil(t, err)
	database := setupTestDb(t)
	createTestConferenceAlliances(t, database, conferences)
	assert.Nil(t, tournament.CreateMatchesAndBreaks(database, time.Unix(0, 0)))
	matches, err := database.GetMatchesByType(model.Playoff, false)
	assert.Nil(t, err)
	assert.Equal(t, "NM1", matches[0].ShortName)
	assert.Equal(t, "SM1", matches[1].ShortName)
	assert.Equal(t, "NM2", matches[2].ShortName)
	assert.Equal(t, "SM2", matches[3].ShortName)
	for _, match := range matches {
		assert.Equal(t, 1, match.FieldId)
	}
	// Interleaved matches are spaced at half their usual duration.
	assert.Equal(t, int64(270), matches[1].Time.Unix()-matches[0].Time.Unix())
}

func TestMultiConferenceChampionshipAlternateFields(t *testing.T) {
	conferences := testConferences(model.SingleEliminationPlayoff, 2, model.SingleEliminationPlayoff, 2)
	tournament, err := NewMultiConferencePlayoffTournament(
		conferences,
		MultiConferenceOptions{
			ChampionshipFormat:       model.ChampionsSeriesChampionship,
			ChampionshipSeriesLength: 3,
			ChampionshipFieldMode:    model.ChampionshipFieldAlternate,
			MultiField:               true,
		},
	)
	assert.Nil(t, err)
	specs := tournament.MatchGroups()["F"].MatchSpecs()
	assert.Equal(t, 1, specs[0].fieldId)
	assert.Equal(t, 2, specs[1].fieldId)
	assert.Equal(t, 0, specs[2].fieldId)
}

func TestMultiConferenceInvalidConfigurations(t *testing.T) {
	conferences := testConferences(model.DoubleEliminationPlayoff, 9, model.DoubleEliminationPlayoff, 4)
	_, err := NewMultiConferencePlayoffTournament(conferences, MultiConferenceOptions{})
	assert.NotNil(t, err)

	conferences = testConferences(model.SingleEliminationPlayoff, 4, model.SingleEliminationPlayoff, 4)
	conferences[1].ShortName = "N"
	_, err = NewMultiConferencePlayoffTournament(conferences, MultiConferenceOptions{})
	assert.NotNil(t, err)

	_, err = NewMultiConferencePlayoffTournament(conferences[:1], MultiConferenceOptions{})
	assert.NotNil(t, err)
}

func TestRelabel(t *testing.T) {
	final, breakSpecs, err := newDoubleEliminationBracket(4)
	assert.Nil(t, err)
	breaks := relabel(
		final,
		breakSpecs,
		bracketOptions{
			allianceOffset:  10,
			idPrefix:        "X-",
			namePrefix:      "Xylo ",
			shortNamePrefix: "X",
			order:           func(order int) int { return order + 50 },
			tbaSetOffset:    7,
			fieldId:         2,
			conferenceId:    2,
			timeline:        3,
		},
	)
	assert.Equal(t, "X-F", final.Id())
	matchGroups, err := collectMatchGroups(final)
	assert.Nil(t, err)
	assertMatchGroups(t, matchGroups, "X-M1", "X-M2", "X-M3", "X-M4", "X-M5", "X-F")
	m1 := matchGroups["X-M1"].(*Matchup)
	assert.Equal(t, 11, m1.redAllianceSource.AllianceId())
	assert.Equal(t, 14, m1.blueAllianceSource.AllianceId())
	assert.Equal(t, "Xylo Match 1", m1.matchSpecs[0].longName)
	assert.Equal(t, "XM1", m1.matchSpecs[0].shortName)
	assert.Equal(t, 51, m1.matchSpecs[0].order)
	assert.Equal(t, 8, m1.matchSpecs[0].tbaMatchKey.SetNumber)
	assert.Equal(t, 2, m1.matchSpecs[0].fieldId)
	assert.Equal(t, 2, m1.ConferenceId)
	assert.Equal(t, 53, breaks[0].orderBefore)
	assert.Equal(t, "Xylo Field Break", breaks[0].description)
	assert.Equal(t, 3, breaks[0].timeline)

	// Relabeling again is a no-op for matchups that were already relabeled.
	relabel(final, nil, bracketOptions{idPrefix: "Y-"})
	assert.Equal(t, "X-F", final.Id())
}

// Conference short names that overlap the championship's own match names (e.g. "C" for Central) must still work.
func TestMultiConferenceShortNamesThatLookLikeChampionship(t *testing.T) {
	for _, names := range [][2]string{{"C", "D"}, {"CO", "S"}, {"CH", "C"}, {"X", "CC"}} {
		for _, format := range []model.ChampionshipFormat{
			model.ChampionsSeriesChampionship,
			model.CrossoverSemisChampionship,
			model.DoubleDeckerChampionship,
		} {
			for _, playoffType := range []model.PlayoffType{model.DoubleEliminationPlayoff, model.SingleEliminationPlayoff} {
				conferences := testConferences(playoffType, 6, playoffType, 4)
				conferences[0].ShortName, conferences[1].ShortName = names[0], names[1]
				tournament, err := NewMultiConferencePlayoffTournament(
					conferences, MultiConferenceOptions{ChampionshipFormat: format, ChampionshipSeriesLength: 3},
				)
				if assert.Nil(t, err, "%v %v %v", names, format, playoffType) {
					database := setupTestDb(t)
					createTestConferenceAlliances(t, database, conferences)
					assert.Nil(t, tournament.CreateMatchesAndBreaks(database, time.Unix(0, 0)))
					playTournament(t, database, tournament)
				}
			}
		}
	}
}
