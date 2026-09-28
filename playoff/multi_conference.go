// Copyright 2026 Team 254. All Rights Reserved.
//
// Builds a playoff tournament made of one bracket per conference, optionally joined by an event championship.

package playoff

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"sort"
	"strings"
)

const (
	// Match order offset of the second conference's bracket when the brackets are not interleaved.
	secondConferenceOrderOffset = 200
	// Match order offset of the championship matches, keeping them after every conference match.
	championshipOrderOffset = 1000
	// Schedule timeline used by the championship matches.
	championshipTimeline = 100
	// TBA set number offset per conference, keeping TBA match keys unique across the brackets.
	conferenceTbaSetOffset = 100
	// Default length of the break before the championship, if not configured.
	defaultChampionshipBreakSec = 900
	// Length of the break between games of a championship series.
	championshipGameBreakSec = 480
)

// MultiConferenceOptions holds the event-wide settings that shape a multi-conference tournament.
type MultiConferenceOptions struct {
	ChampionshipFormat       model.ChampionshipFormat
	ChampionshipSeriesLength int
	ChampionshipFieldMode    model.ChampionshipFieldMode
	ChampionshipBreakSec     int
	// Whether the event runs on more than one field (hub and nodes); if not, every match is played on one field.
	MultiField bool
	// Whether to interleave the two conferences' matches when both brackets are played on the same field.
	InterleaveOnSharedField bool
}

// MultiConferenceOptionsFromSettings extracts the multi-conference tournament options from the event settings.
func MultiConferenceOptionsFromSettings(settings *model.EventSettings) MultiConferenceOptions {
	return MultiConferenceOptions{
		ChampionshipFormat:       settings.ChampionshipFormat,
		ChampionshipSeriesLength: settings.EffectiveChampionshipSeriesLength(),
		ChampionshipFieldMode:    settings.ChampionshipFieldMode,
		ChampionshipBreakSec:     settings.ChampionshipBreakSec,
		MultiField:               settings.IsMultiField(),
		InterleaveOnSharedField:  settings.SinglePlayoffFieldInterleave,
	}
}

// NewMultiConferencePlayoffTournament creates a tournament containing a bracket for each of the two given conferences,
// joined by an event championship according to the given options.
func NewMultiConferencePlayoffTournament(
	conferences []model.Conference, options MultiConferenceOptions,
) (*PlayoffTournament, error) {
	if len(conferences) != model.NumConferences {
		return nil, fmt.Errorf("multi-conference tournament requires exactly %d conferences", model.NumConferences)
	}
	conferences = append([]model.Conference(nil), conferences...)
	sort.Slice(
		conferences,
		func(i, j int) bool {
			return conferences[i].Id < conferences[j].Id
		},
	)
	if strings.EqualFold(conferences[0].ShortName, conferences[1].ShortName) ||
		strings.EqualFold(strings.TrimSpace(conferences[0].Name), strings.TrimSpace(conferences[1].Name)) {
		return nil, fmt.Errorf("conferences must have distinct names and short names")
	}
	for _, conference := range conferences {
		if err := conference.Validate(); err != nil {
			return nil, err
		}
	}

	sameField := !options.MultiField || conferences[0].PlayoffFieldId == conferences[1].PlayoffFieldId
	interleave := sameField && options.InterleaveOnSharedField

	tournament := &PlayoffTournament{
		conferences:         conferences,
		conferenceFinals:    make(map[int]*Matchup),
		conferenceTimelines: make(map[int]int),
		allianceConferences: make(map[int]int),
		allianceSeeds:       make(map[int]int),
		championshipFormat:  options.ChampionshipFormat,
	}

	var breaks []tournamentBreak
	allianceOffset := 0
	for i, conference := range conferences {
		final, conferenceBreaks, err := newConferenceBracket(conference)
		if err != nil {
			return nil, err
		}

		index := i
		orderFunc := func(order int) int { return order + index*secondConferenceOrderOffset }
		if interleave {
			orderFunc = func(order int) int { return 2*order - 1 + index }
		}
		timeline := 1 + i
		if sameField {
			timeline = 1
		}
		durationScale := 1.0
		if interleave {
			durationScale = 0.5
		}
		fieldId := 0
		if options.MultiField {
			fieldId = conference.PlayoffFieldId
		}

		breaks = append(
			breaks,
			relabel(
				final,
				conferenceBreaks,
				bracketOptions{
					allianceOffset:  allianceOffset,
					idPrefix:        conference.ShortName + "-",
					namePrefix:      conference.Name + " ",
					shortNamePrefix: conference.ShortName,
					order:           orderFunc,
					tbaSetOffset:    (i + 1) * conferenceTbaSetOffset,
					fieldId:         fieldId,
					conferenceId:    conference.Id,
					timeline:        timeline,
					durationScale:   durationScale,
				},
			)...,
		)
		final.isConferenceFinal = true
		tournament.conferenceFinals[conference.Id] = final
		tournament.conferenceTimelines[conference.Id] = timeline
		for seed := 1; seed <= conference.NumAlliances; seed++ {
			tournament.allianceConferences[allianceOffset+seed] = conference.Id
			tournament.allianceSeeds[allianceOffset+seed] = seed
		}
		allianceOffset += conference.NumAlliances
	}

	first := tournament.conferenceFinals[conferences[0].Id]
	second := tournament.conferenceFinals[conferences[1].Id]
	championship, championshipBreaks, err := newChampionship(
		first, second, options, championshipShortPrefix(conferences, tournamentShortNames(first, second)),
	)
	if err != nil {
		return nil, err
	}
	if championship == nil {
		tournament.roots = []*Matchup{first, second}
	} else {
		tournament.finalMatchup = championship
		tournament.roots = []*Matchup{championship}
		breaks = append(breaks, championshipBreaks...)
	}

	if err = tournament.initialize(breaks); err != nil {
		return nil, err
	}
	return tournament, nil
}

// Builds the bracket for a single conference using the standard bracket constructors.
func newConferenceBracket(conference model.Conference) (*Matchup, []breakSpec, error) {
	switch conference.PlayoffType {
	case model.SingleEliminationPlayoff:
		return newSingleEliminationBracketWithOptions(
			conference.NumAlliances,
			singleEliminationOptions{seriesLengths: conference.SeriesLengths, labelPlayIn: true},
		)
	case model.DoubleEliminationPlayoff:
		final, breakSpecs, err := newDoubleEliminationBracket(conference.NumAlliances)
		if err != nil {
			return nil, nil, err
		}
		if finalLength := conference.SeriesLength("F"); finalLength != 3 {
			final.matchSpecs = newFinalMatchesWithLength(final.matchSpecs[0].order, finalLength)
			final.NumWinsToAdvance = finalLength/2 + 1
		}
		return final, breakSpecs, nil
	}
	return nil, nil, fmt.Errorf("invalid playoff type for %s: %v", conference.Name, conference.PlayoffType)
}

// Builds the championship that joins the two conference finals, returning nil if the format has no championship.
func newChampionship(
	first, second *Matchup, options MultiConferenceOptions, prefix string,
) (*Matchup, []tournamentBreak, error) {
	seriesLength := options.ChampionshipSeriesLength
	if seriesLength != 1 && seriesLength != 5 {
		seriesLength = 3
	}

	var final *Matchup
	var breakSpecs []breakSpec
	switch options.ChampionshipFormat {
	case model.NoChampionship:
		return nil, nil, nil
	case model.ChampionsSeriesChampionship:
		final = &Matchup{
			id:                 "F",
			NumWinsToAdvance:   seriesLength/2 + 1,
			redAllianceSource:  matchupSource{matchup: first, useWinner: true},
			blueAllianceSource: matchupSource{matchup: second, useWinner: true},
			matchSpecs: newFinalSeriesMatches(
				"Championship", prefix, "Championship Overtime", prefix+"O", "f", 1, 1, seriesLength,
			),
		}
		for game := 2; game <= seriesLength; game++ {
			breakSpecs = append(breakSpecs, breakSpec{game, championshipGameBreakSec, "Field Break"})
		}
	case model.CrossoverSemisChampionship:
		semifinals := []*Matchup{
			{
				id:                 "CS1",
				NumWinsToAdvance:   seriesLength/2 + 1,
				redAllianceSource:  matchupSource{matchup: first, useWinner: true},
				blueAllianceSource: matchupSource{matchup: second, useWinner: false},
			},
			{
				id:                 "CS2",
				NumWinsToAdvance:   seriesLength/2 + 1,
				redAllianceSource:  matchupSource{matchup: second, useWinner: true},
				blueAllianceSource: matchupSource{matchup: first, useWinner: false},
			},
		}
		for set, semifinal := range semifinals {
			for game := 1; game <= seriesLength; game++ {
				semifinal.matchSpecs = append(
					semifinal.matchSpecs,
					newSingleEliminationRoundMatch(
						"Championship Semifinal", prefix+"S", "sf", set+1, game, (game-1)*2+set+1,
					),
				)
				semifinal.matchSpecs[game-1].isHidden = seriesLength >= 5 && game > semifinal.NumWinsToAdvance
			}
		}
		finalOrder := 2*seriesLength + 1
		final = &Matchup{
			id:                 "F",
			NumWinsToAdvance:   seriesLength/2 + 1,
			redAllianceSource:  matchupSource{matchup: semifinals[0], useWinner: true},
			blueAllianceSource: matchupSource{matchup: semifinals[1], useWinner: true},
			matchSpecs: newFinalSeriesMatches(
				"Championship", prefix, "Championship Overtime", prefix+"O", "f", 1, finalOrder, seriesLength,
			),
		}
		for game := 2; game <= seriesLength; game++ {
			breakSpecs = append(breakSpecs, breakSpec{finalOrder + game - 1, championshipGameBreakSec, "Field Break"})
		}
		breakSpecs = append(breakSpecs, breakSpec{finalOrder, championshipGameBreakSec, "Field Break"})
	case model.DoubleDeckerChampionship:
		// Seeds: 1 = first conference champion, 2 = second conference champion, 3 = first conference runner-up and
		// 4 = second conference runner-up, so that round one pits each champion against the other conference's
		// runner-up.
		var err error
		final, breakSpecs, err = newFourAllianceDoubleEliminationBracketFromSources(
			[4]allianceSource{
				matchupSource{matchup: first, useWinner: true},
				matchupSource{matchup: second, useWinner: true},
				matchupSource{matchup: first, useWinner: false},
				matchupSource{matchup: second, useWinner: false},
			},
		)
		if err != nil {
			return nil, nil, err
		}
		if seriesLength != 3 {
			final.matchSpecs = newFinalMatchesWithLength(final.matchSpecs[0].order, seriesLength)
			final.NumWinsToAdvance = seriesLength/2 + 1
		}
	default:
		return nil, nil, fmt.Errorf("invalid championship format: %v", options.ChampionshipFormat)
	}

	breakSec := options.ChampionshipBreakSec
	if breakSec <= 0 {
		breakSec = defaultChampionshipBreakSec
	}
	idPrefix := ""
	namePrefix := ""
	shortNamePrefix := ""
	if options.ChampionshipFormat == model.DoubleDeckerChampionship {
		idPrefix = championshipIdPrefix
		namePrefix = "Championship "
		shortNamePrefix = prefix
	}
	orderFunc := func(order int) int { return order + championshipOrderOffset }
	breaks := relabel(
		final,
		breakSpecs,
		bracketOptions{
			idPrefix:        idPrefix,
			namePrefix:      namePrefix,
			shortNamePrefix: shortNamePrefix,
			order:           orderFunc,
			timeline:        championshipTimeline,
		},
	)
	// The root of the championship is always the tournament final.
	final.id = "F"

	// Replace any break before the first championship match with the championship showcase break.
	var championshipSpecs []*matchSpec
	collectSpecs := func(matchGroup MatchGroup) error {
		if matchup, ok := matchGroup.(*Matchup); ok && matchup.ConferenceId == 0 {
			championshipSpecs = append(championshipSpecs, matchup.matchSpecs...)
		}
		return nil
	}
	if err := final.traverse(collectSpecs); err != nil {
		return nil, nil, err
	}
	sort.Slice(
		championshipSpecs,
		func(i, j int) bool {
			return championshipSpecs[i].order < championshipSpecs[j].order
		},
	)
	if len(championshipSpecs) == 0 {
		return nil, nil, fmt.Errorf("championship has no matches")
	}
	firstOrder := championshipSpecs[0].order
	var filteredBreaks []tournamentBreak
	for _, tournamentBreak := range breaks {
		if tournamentBreak.orderBefore != firstOrder {
			filteredBreaks = append(filteredBreaks, tournamentBreak)
		}
	}
	filteredBreaks = append(
		filteredBreaks,
		tournamentBreak{
			breakSpec: breakSpec{orderBefore: firstOrder, durationSec: breakSec, description: "Championship Showcase"},
			timeline:  championshipTimeline,
		},
	)

	// Assign the championship matches to fields.
	if options.MultiField {
		for i, spec := range championshipSpecs {
			switch options.ChampionshipFieldMode {
			case model.ChampionshipFieldFixed1:
				spec.fieldId = 1
			case model.ChampionshipFieldFixed2:
				spec.fieldId = 2
			case model.ChampionshipFieldAlternate:
				// Alternate fields, leaving any deciding games beyond the first two of the final series for the hub to
				// assign.
				if spec.isInMatchup(final) && spec.gameIndex(final) > 2 {
					spec.fieldId = 0
				} else {
					spec.fieldId = 1 + i%2
				}
			case model.ChampionshipFieldChooseAtLoad:
				spec.fieldId = 0
			}
		}
		for i := range filteredBreaks {
			if len(championshipSpecs) > 0 {
				filteredBreaks[i].fieldId = fieldForOrder(championshipSpecs, filteredBreaks[i].orderBefore)
			}
		}
	}

	return final, filteredBreaks, nil
}

// isInMatchup returns true if the spec belongs to the given matchup.
func (spec *matchSpec) isInMatchup(matchup *Matchup) bool {
	for _, matchupSpec := range matchup.matchSpecs {
		if matchupSpec == spec {
			return true
		}
	}
	return false
}

// gameIndex returns the one-based position of the spec within the given matchup's series.
func (spec *matchSpec) gameIndex(matchup *Matchup) int {
	for i, matchupSpec := range matchup.matchSpecs {
		if matchupSpec == spec {
			return i + 1
		}
	}
	return 0
}

// Returns the field of the match with the given order, or zero if there is none.
func fieldForOrder(specs []*matchSpec, order int) int {
	for _, spec := range specs {
		if spec.order == order {
			return spec.fieldId
		}
	}
	return 0
}

// Prefix of the IDs of double-decker championship matchups. Conference matchup IDs are the conference's short name
// (at most three characters) followed by a dash, so they can never start with this.
const championshipIdPrefix = "CHAMP-"

// Candidate prefixes for championship match short names, in order of preference.
var championshipShortPrefixes = []string{"C", "CH", "CC", "X", "Z"}

// Returns the short names of every match in the brackets rooted at the given matchups.
func tournamentShortNames(roots ...*Matchup) map[string]bool {
	names := make(map[string]bool)
	for _, root := range roots {
		_ = root.traverse(
			func(matchGroup MatchGroup) error {
				for _, spec := range matchGroup.MatchSpecs() {
					names[spec.shortName] = true
				}
				return nil
			},
		)
	}
	return names
}

// Picks a prefix for the championship match short names that can't be confused with any conference match (e.g. "C1"
// normally, but not when a conference's own short name starts with "C").
func championshipShortPrefix(conferences []model.Conference, taken map[string]bool) string {
	suffixes := []string{"1", "2", "3", "4", "5", "O1", "O2", "O3", "S1-1", "S2-1", "M1", "M5", "F1", "F2", "F3", "O4"}
	for _, prefix := range championshipShortPrefixes {
		clashes := false
		for _, conference := range conferences {
			if strings.HasPrefix(prefix, conference.ShortName) || strings.HasPrefix(conference.ShortName, prefix) {
				clashes = true
			}
		}
		for _, suffix := range suffixes {
			if taken[prefix+suffix] {
				clashes = true
			}
		}
		if !clashes {
			return prefix
		}
	}
	return "CHAMP"
}
