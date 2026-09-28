// Copyright 2023 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Represents a scheduled break in the playoff match schedule.

package playoff

type breakSpec struct {
	orderBefore int
	durationSec int
	description string
}

// A break placed into the schedule of a tournament, along with the field and schedule timeline it belongs to.
type tournamentBreak struct {
	breakSpec
	fieldId  int
	timeline int
}

// Wraps breaks from a single-bracket tournament, which are all on the default field and timeline.
func newTournamentBreaks(breakSpecs []breakSpec) []tournamentBreak {
	tournamentBreaks := make([]tournamentBreak, len(breakSpecs))
	for i, spec := range breakSpecs {
		tournamentBreaks[i] = tournamentBreak{breakSpec: spec}
	}
	return tournamentBreaks
}
