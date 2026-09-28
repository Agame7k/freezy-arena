// Copyright 2022 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Defines the tournament structure for a single-elimination bracket with a configurable series length per round.

package playoff

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"strings"
)

// Definition of the pre-final rounds of a single-elimination bracket, in order of play.
var singleEliminationRounds = []struct {
	key       string
	longName  string
	shortName string
	numSets   int
}{
	{"EF", "Eighthfinal", "EF", 8},
	{"QF", "Quarterfinal", "QF", 4},
	{"SF", "Semifinal", "SF", 2},
}

// Seeding of the eighthfinal round, in set order.
var singleEliminationSeeds = [8][2]int{{1, 16}, {8, 9}, {4, 13}, {5, 12}, {2, 15}, {7, 10}, {3, 14}, {6, 11}}

// singleEliminationOptions configures the series length of each round and optional round labeling.
type singleEliminationOptions struct {
	// Series length (1, 3 or 5) keyed by round ("EF", "QF", "SF", "F"). Missing rounds default to best-of-three.
	seriesLengths map[string]int
	// Whether to label a partially-filled first round of a 9-15 alliance bracket as a play-in round.
	labelPlayIn bool
}

func (options singleEliminationOptions) seriesLength(round string) int {
	if length, ok := options.seriesLengths[round]; ok && (length == 1 || length == 3 || length == 5) {
		return length
	}
	return 3
}

// Creates a single-elimination bracket containing only the required matchups for the given number of alliances, and
// returns the root matchup comprising the tournament finals along with scheduled breaks.
func newSingleEliminationBracket(numAlliances int) (*Matchup, []breakSpec, error) {
	return newSingleEliminationBracketWithOptions(numAlliances, singleEliminationOptions{})
}

// Creates a single-elimination bracket with the given series lengths per round.
func newSingleEliminationBracketWithOptions(
	numAlliances int, options singleEliminationOptions,
) (*Matchup, []breakSpec, error) {
	if numAlliances < 2 {
		return nil, nil, fmt.Errorf("single-elimination bracket must have at least 2 alliances")
	}
	if numAlliances > 16 {
		return nil, nil, fmt.Errorf("single-elimination bracket must have at most 16 alliances")
	}

	order := 0
	var previousRound []*Matchup
	for roundIndex, round := range singleEliminationRounds {
		seriesLength := options.seriesLength(round.key)
		longName, shortName := round.longName, round.shortName
		if roundIndex == 0 && options.labelPlayIn && numAlliances > 8 && numAlliances < 16 {
			longName, shortName = "Play-In", "PI"
		}

		matchups := make([]*Matchup, round.numSets)
		for set := 1; set <= round.numSets; set++ {
			matchup := &Matchup{
				id:               fmt.Sprintf("%s%d", round.shortName, set),
				NumWinsToAdvance: seriesLength/2 + 1,
			}
			if roundIndex == 0 {
				matchup.redAllianceSource = allianceSelectionSource{singleEliminationSeeds[set-1][0]}
				matchup.blueAllianceSource = allianceSelectionSource{singleEliminationSeeds[set-1][1]}
			} else {
				matchup.redAllianceSource = newSingleEliminationAllianceSource(previousRound[2*set-2], numAlliances)
				matchup.blueAllianceSource = newSingleEliminationAllianceSource(previousRound[2*set-1], numAlliances)
			}
			for game := 1; game <= seriesLength; game++ {
				match := newSingleEliminationRoundMatch(
					longName, shortName, round.shortName, set, game, order+(game-1)*round.numSets+set,
				)
				// Games that are only needed in a long series are hidden until the series requires them.
				match.isHidden = seriesLength >= 5 && game > matchup.NumWinsToAdvance
				matchup.matchSpecs = append(matchup.matchSpecs, match)
			}
			matchups[set-1] = matchup
		}
		order += seriesLength * round.numSets
		previousRound = matchups
	}

	// Define final matches.
	finalSeriesLength := options.seriesLength("F")
	final := Matchup{
		id:                 "F",
		NumWinsToAdvance:   finalSeriesLength/2 + 1,
		redAllianceSource:  newSingleEliminationAllianceSource(previousRound[0], numAlliances),
		blueAllianceSource: newSingleEliminationAllianceSource(previousRound[1], numAlliances),
		matchSpecs:         newFinalMatchesWithLength(order+1, finalSeriesLength),
	}

	// Define scheduled breaks.
	var breakSpecs []breakSpec
	if numAlliances > 2 {
		// Only create a break before the first finals match if there were preceding matches.
		breakSpecs = append(breakSpecs, breakSpec{order + 1, 480, "Field Break"})
	}
	for game := 2; game <= finalSeriesLength; game++ {
		breakSpecs = append(breakSpecs, breakSpec{order + game, 480, "Field Break"})
	}

	return &final, breakSpecs, nil
}

// Helper method to create an allianceSource while pruning any unnecessary matchups due to the number of alliances.
func newSingleEliminationAllianceSource(matchup *Matchup, numAlliances int) allianceSource {
	redAllianceId := matchup.redAllianceSource.AllianceId()
	blueAllianceId := matchup.blueAllianceSource.AllianceId()

	if blueAllianceId > redAllianceId && blueAllianceId > numAlliances {
		return matchup.redAllianceSource
	}
	if redAllianceId > blueAllianceId && redAllianceId > numAlliances {
		return matchup.blueAllianceSource
	}
	return matchupSource{matchup: matchup, useWinner: true}
}

// Helper method to create a match spec for a pre-final single-elimination matchup.
func newSingleEliminationMatch(longRoundName, shortRoundName string, setNumber, matchNumber, order int) *matchSpec {
	return newSingleEliminationRoundMatch(longRoundName, shortRoundName, shortRoundName, setNumber, matchNumber, order)
}

// Helper method to create a match spec for a pre-final single-elimination matchup whose display names may differ from
// the round name used for its TBA match key.
func newSingleEliminationRoundMatch(
	longRoundName, shortRoundName, tbaRoundName string, setNumber, matchNumber, order int,
) *matchSpec {
	return &matchSpec{
		longName:            fmt.Sprintf("%s %d-%d", longRoundName, setNumber, matchNumber),
		shortName:           fmt.Sprintf("%s%d-%d", shortRoundName, setNumber, matchNumber),
		order:               order,
		durationSec:         600,
		useTiebreakCriteria: true,
		tbaMatchKey:         model.TbaMatchKey{strings.ToLower(tbaRoundName), setNumber, matchNumber},
	}
}

// Helper method to create the best-of-three final matches for any tournament type.
func newFinalMatches(startingOrder int) []*matchSpec {
	return newFinalMatchesWithLength(startingOrder, 3)
}

// Helper method to create the final matches for a series of the given length (1, 3 or 5), followed by up to three
// hidden overtime matches used to break a tied series.
func newFinalMatchesWithLength(startingOrder, seriesLength int) []*matchSpec {
	return newFinalSeriesMatches("Final", "F", "Overtime", "O", "f", 1, startingOrder, seriesLength)
}

// Helper method to create the matches for a final-style series with the given names.
func newFinalSeriesMatches(
	longName, shortName, overtimeLongName, overtimeShortName, tbaCompLevel string,
	tbaSetNumber, startingOrder, seriesLength int,
) []*matchSpec {
	if seriesLength == 1 {
		// A single-match final is decided using the tiebreak criteria, so no overtime matches are needed.
		return []*matchSpec{
			{
				longName:            fmt.Sprintf("%s 1", longName),
				shortName:           fmt.Sprintf("%s1", shortName),
				order:               startingOrder,
				durationSec:         300,
				useTiebreakCriteria: true,
				tbaMatchKey:         model.TbaMatchKey{tbaCompLevel, tbaSetNumber, 1},
			},
		}
	}

	numWinsToAdvance := seriesLength/2 + 1
	var matchSpecs []*matchSpec
	for game := 1; game <= seriesLength; game++ {
		matchSpecs = append(
			matchSpecs,
			&matchSpec{
				longName:            fmt.Sprintf("%s %d", longName, game),
				shortName:           fmt.Sprintf("%s%d", shortName, game),
				order:               startingOrder + game - 1,
				durationSec:         300,
				useTiebreakCriteria: false,
				isHidden:            seriesLength >= 5 && game > numWinsToAdvance,
				tbaMatchKey:         model.TbaMatchKey{tbaCompLevel, tbaSetNumber, game},
			},
		)
	}
	for overtime := 1; overtime <= 3; overtime++ {
		matchSpecs = append(
			matchSpecs,
			&matchSpec{
				longName:            fmt.Sprintf("%s %d", overtimeLongName, overtime),
				shortName:           fmt.Sprintf("%s%d", overtimeShortName, overtime),
				order:               startingOrder + seriesLength + overtime - 1,
				durationSec:         600,
				useTiebreakCriteria: true,
				isHidden:            true,
				tbaMatchKey:         model.TbaMatchKey{tbaCompLevel, tbaSetNumber, seriesLength + overtime},
			},
		)
	}
	return matchSpecs
}
