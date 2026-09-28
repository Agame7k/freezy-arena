// Copyright 2023 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Models and logic encapsulating the common aspects of all supported playoff tournament formats.

package playoff

import (
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"math"
	"sort"
	"strconv"
	"time"
)

type PlayoffTournament struct {
	matchGroups map[string]MatchGroup
	matchSpecs  []*matchSpec
	breaks      []tournamentBreak
	// The root of the tournament; nil for a multi-conference tournament without an event championship.
	finalMatchup *Matchup
	// The matchups from which the whole tournament can be traversed.
	roots []*Matchup

	// Multi-conference state; empty for a single-conference tournament.
	conferences         []model.Conference
	conferenceFinals    map[int]*Matchup
	conferenceTimelines map[int]int
	allianceConferences map[int]int
	allianceSeeds       map[int]int
	championshipFormat  model.ChampionshipFormat
}

// NewPlayoffTournament creates a new playoff tournament of the given type and number of alliances, or returns an error
// if the number of alliances is invalid for the given tournament type.
func NewPlayoffTournament(playoffType model.PlayoffType, numPlayoffAlliances int) (*PlayoffTournament, error) {
	var finalMatchup *Matchup
	var breakSpecs []breakSpec
	var err error
	switch playoffType {
	case model.DoubleEliminationPlayoff:
		finalMatchup, breakSpecs, err = newDoubleEliminationBracket(numPlayoffAlliances)
	case model.SingleEliminationPlayoff:
		finalMatchup, breakSpecs, err = newSingleEliminationBracket(numPlayoffAlliances)
	default:
		err = fmt.Errorf("invalid playoff type: %v", playoffType)
	}
	if err != nil {
		return nil, err
	}

	tournament := &PlayoffTournament{finalMatchup: finalMatchup, roots: []*Matchup{finalMatchup}}
	if err = tournament.initialize(newTournamentBreaks(breakSpecs)); err != nil {
		return nil, err
	}
	return tournament, nil
}

// initialize collects the match groups and specs from the tournament roots, links the tree and populates the initial
// alliances.
func (tournament *PlayoffTournament) initialize(breaks []tournamentBreak) error {
	matchGroups, err := collectMatchGroupsFromTraversal(tournament.traverseAll)
	if err != nil {
		return err
	}
	matchSpecs, err := collectMatchSpecsFromTraversal(tournament.traverseAll)
	if err != nil {
		return err
	}

	sort.SliceStable(
		breaks,
		func(i, j int) bool {
			return breaks[i].orderBefore < breaks[j].orderBefore
		},
	)

	// Doubly link the match group tree in order to populate alliance destinations.
	for _, root := range tournament.roots {
		root.setSourceDestinations()
	}

	// Trigger an initial update to populate the alliances.
	for _, root := range tournament.roots {
		root.update(map[int]playoffMatchResult{})
	}

	tournament.matchGroups = matchGroups
	tournament.matchSpecs = matchSpecs
	tournament.breaks = breaks
	return nil
}

// MatchGroups returns a map of all match groups in the tournament keyed by ID.
func (tournament *PlayoffTournament) MatchGroups() map[string]MatchGroup {
	return tournament.matchGroups
}

// FinalMatchup returns the matchup representing the tournament's final round, or nil if the tournament has no single
// final (a multi-conference tournament without an event championship).
func (tournament *PlayoffTournament) FinalMatchup() *Matchup {
	return tournament.finalMatchup
}

// IsComplete returns true if the tournament has been won and false if it is still in progress.
func (tournament *PlayoffTournament) IsComplete() bool {
	if tournament.finalMatchup == nil {
		for _, root := range tournament.roots {
			if !root.IsComplete() {
				return false
			}
		}
		return len(tournament.roots) > 0
	}
	return tournament.finalMatchup.IsComplete()
}

// WinningAllianceId returns the number of the alliance that won the tournament, or 0 if the tournament is not yet
// complete or has no single winner.
func (tournament *PlayoffTournament) WinningAllianceId() int {
	if tournament.finalMatchup == nil {
		return 0
	}
	return tournament.finalMatchup.WinningAllianceId()
}

// FinalistAllianceId returns the number of the alliance that were tournament finalists, or 0 if the tournament is not
// yet complete or has no single winner.
func (tournament *PlayoffTournament) FinalistAllianceId() int {
	if tournament.finalMatchup == nil {
		return 0
	}
	return tournament.finalMatchup.LosingAllianceId()
}

// Traverse calls the given function on each match group in the tournament, in reverse round order of play.
func (tournament *PlayoffTournament) Traverse(visitFunction func(MatchGroup) error) error {
	return tournament.traverseAll(visitFunction)
}

func (tournament *PlayoffTournament) traverseAll(visitFunction func(MatchGroup) error) error {
	for _, root := range tournament.roots {
		if err := root.traverse(visitFunction); err != nil {
			return err
		}
	}
	return nil
}

// IsMultiConference returns true if the tournament is made up of separate conference brackets.
func (tournament *PlayoffTournament) IsMultiConference() bool {
	return len(tournament.conferences) > 0
}

// Conferences returns the conferences that the tournament was built for, if it is a multi-conference tournament.
func (tournament *PlayoffTournament) Conferences() []model.Conference {
	return tournament.conferences
}

// ChampionshipFormat returns the format used to join the conference brackets into an event championship.
func (tournament *PlayoffTournament) ChampionshipFormat() model.ChampionshipFormat {
	return tournament.championshipFormat
}

// ConferenceFinal returns the final matchup of the given conference's bracket, or nil if there is none.
func (tournament *PlayoffTournament) ConferenceFinal(conferenceId int) *Matchup {
	return tournament.conferenceFinals[conferenceId]
}

// IsConferenceComplete returns true if the given conference's bracket has produced a winner.
func (tournament *PlayoffTournament) IsConferenceComplete(conferenceId int) bool {
	final := tournament.ConferenceFinal(conferenceId)
	return final != nil && final.IsComplete()
}

// AllianceConferenceId returns the conference of the given alliance, or 0 in a single-conference tournament.
func (tournament *PlayoffTournament) AllianceConferenceId(allianceId int) int {
	return tournament.allianceConferences[allianceId]
}

// AllianceLabel returns the display label of the given alliance, e.g. "3" in a single-conference tournament or "N3"
// in a multi-conference tournament.
func (tournament *PlayoffTournament) AllianceLabel(allianceId int) string {
	if allianceId == 0 {
		return ""
	}
	conferenceId, ok := tournament.allianceConferences[allianceId]
	if !ok {
		return strconv.Itoa(allianceId)
	}
	for _, conference := range tournament.conferences {
		if conference.Id == conferenceId {
			return conference.ShortName + strconv.Itoa(tournament.allianceSeeds[allianceId])
		}
	}
	return strconv.Itoa(allianceId)
}

// CreateMatchesAndBreaks creates all the playoff matches and scheduled breaks in the database, as a one-time action at
// the beginning of the playoff tournament. Every bracket starts at the given time.
func (tournament *PlayoffTournament) CreateMatchesAndBreaks(database *model.Database, startTime time.Time) error {
	return tournament.CreateMatchesAndBreaksWithStartTimes(database, startTime, nil)
}

// CreateMatchesAndBreaksWithStartTimes creates all the playoff matches and scheduled breaks in the database, starting
// each conference's bracket at its entry in the given map (keyed by conference ID) or at the default start time if it
// has none.
func (tournament *PlayoffTournament) CreateMatchesAndBreaksWithStartTimes(
	database *model.Database, defaultStartTime time.Time, conferenceStartTimes map[int]time.Time,
) error {
	matches, err := database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		return fmt.Errorf("cannot create playoff matches; %d matches already exist", len(matches))
	}
	scheduledBreaks, err := database.GetScheduledBreaksByMatchType(model.Playoff)
	if err != nil {
		return err
	}
	if len(scheduledBreaks) > 0 {
		return fmt.Errorf("cannot create playoff breaks; %d breaks already exist", len(scheduledBreaks))
	}

	alliances, err := getAlliancesById(database)
	if err != nil {
		return err
	}

	matchTimes, breakTimes := tournament.computeSchedule(defaultStartTime, conferenceStartTimes)
	for _, tournamentBreak := range tournament.scheduledBreaks() {
		scheduledBreak := model.ScheduledBreak{
			MatchType:       model.Playoff,
			TypeOrderBefore: tournamentBreak.orderBefore,
			Time:            breakTimes[tournamentBreak.orderBefore],
			DurationSec:     tournamentBreak.durationSec,
			Description:     tournamentBreak.description,
			FieldId:         tournamentBreak.fieldId,
		}
		if err := database.CreateScheduledBreak(&scheduledBreak); err != nil {
			return err
		}
	}

	for _, matchSpec := range tournament.matchSpecs {
		match := model.Match{
			Type:                model.Playoff,
			TypeOrder:           matchSpec.order,
			Time:                matchTimes[matchSpec.order],
			LongName:            matchSpec.longName,
			ShortName:           matchSpec.shortName,
			NameDetail:          matchSpec.nameDetail,
			PlayoffMatchGroupId: matchSpec.matchGroupId,
			PlayoffRedAlliance:  matchSpec.redAllianceId,
			PlayoffBlueAlliance: matchSpec.blueAllianceId,
			UseTiebreakCriteria: matchSpec.useTiebreakCriteria,
			TbaMatchKey:         matchSpec.tbaMatchKey,
			FieldId:             matchSpec.fieldId,
			ConferenceId:        matchSpec.conferenceId,
		}
		if alliance, ok := alliances[match.PlayoffRedAlliance]; ok {
			positionRedTeams(&match, &alliance)
		}
		if alliance, ok := alliances[match.PlayoffBlueAlliance]; ok {
			positionBlueTeams(&match, &alliance)
		}
		if matchSpec.isHidden {
			match.Status = game.MatchHidden
		} else {
			match.Status = game.MatchScheduled
		}

		if err := database.CreateMatch(&match); err != nil {
			return err
		}
	}

	return nil
}

// RescheduleMatchesAndBreaks recomputes the scheduled times of the existing unplayed playoff matches and the playoff
// breaks, e.g. after a conference's alliance selection is finalized with its own start time.
func (tournament *PlayoffTournament) RescheduleMatchesAndBreaks(
	database *model.Database, defaultStartTime time.Time, conferenceStartTimes map[int]time.Time,
) error {
	matchTimes, breakTimes := tournament.computeSchedule(defaultStartTime, conferenceStartTimes)

	matches, err := database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		return err
	}
	for _, match := range matches {
		newTime, ok := matchTimes[match.TypeOrder]
		if !ok || match.IsComplete() || !match.StartedAt.IsZero() || match.Time.Equal(newTime) {
			continue
		}
		match.Time = newTime
		if err = database.UpdateMatch(&match); err != nil {
			return err
		}
	}

	scheduledBreaks, err := database.GetScheduledBreaksByMatchType(model.Playoff)
	if err != nil {
		return err
	}
	for _, scheduledBreak := range scheduledBreaks {
		newTime, ok := breakTimes[scheduledBreak.TypeOrderBefore]
		if !ok || scheduledBreak.Time.Equal(newTime) {
			continue
		}
		scheduledBreak.Time = newTime
		if err = database.UpdateScheduledBreak(&scheduledBreak); err != nil {
			return err
		}
	}
	return nil
}

// scheduledBreaks returns the breaks that precede an existing match, in order.
func (tournament *PlayoffTournament) scheduledBreaks() []tournamentBreak {
	existingOrders := make(map[int]struct{}, len(tournament.matchSpecs))
	for _, spec := range tournament.matchSpecs {
		existingOrders[spec.order] = struct{}{}
	}
	var breaks []tournamentBreak
	for _, tournamentBreak := range tournament.breaks {
		if _, ok := existingOrders[tournamentBreak.orderBefore]; ok {
			breaks = append(breaks, tournamentBreak)
		}
	}
	return breaks
}

// computeSchedule walks the matches in order of play and determines the start time of each match and break, keyed by
// match order. Matches on the same timeline are played back-to-back; each conference timeline starts at that
// conference's start time, and the championship timeline starts once every other timeline has finished.
func (tournament *PlayoffTournament) computeSchedule(
	defaultStartTime time.Time, conferenceStartTimes map[int]time.Time,
) (map[int]time.Time, map[int]time.Time) {
	breaksByOrder := make(map[int]tournamentBreak)
	for _, tournamentBreak := range tournament.scheduledBreaks() {
		breaksByOrder[tournamentBreak.orderBefore] = tournamentBreak
	}

	timelineTimes := make(map[int]time.Time)
	timelineStart := func(spec *matchSpec) time.Time {
		if spec.timeline == championshipTimeline {
			// The championship starts after every conference bracket has finished.
			var latest time.Time
			for timeline, timelineTime := range timelineTimes {
				if timeline != championshipTimeline && timelineTime.After(latest) {
					latest = timelineTime
				}
			}
			if latest.IsZero() {
				latest = defaultStartTime
			}
			return latest
		}
		// Use the earliest start time of any conference sharing this timeline.
		var start time.Time
		for conferenceId, timeline := range tournament.conferenceTimelines {
			if timeline != spec.timeline {
				continue
			}
			if conferenceStart, ok := conferenceStartTimes[conferenceId]; ok && !conferenceStart.IsZero() &&
				(start.IsZero() || conferenceStart.Before(start)) {
				start = conferenceStart
			}
		}
		if start.IsZero() {
			start = defaultStartTime
		}
		return start
	}

	matchTimes := make(map[int]time.Time)
	breakTimes := make(map[int]time.Time)
	for _, spec := range tournament.matchSpecs {
		nextEventTime, ok := timelineTimes[spec.timeline]
		if !ok {
			nextEventTime = timelineStart(spec)
		}

		if tournamentBreak, ok := breaksByOrder[spec.order]; ok {
			// Create the break that is scheduled before the next match.
			breakTimes[spec.order] = nextEventTime
			nextEventTime = nextEventTime.Add(time.Duration(tournamentBreak.durationSec) * time.Second)
		}

		matchTimes[spec.order] = nextEventTime
		durationSec := float64(spec.durationSec)
		if spec.durationScale > 0 {
			durationSec = math.Round(durationSec * spec.durationScale)
		}
		timelineTimes[spec.timeline] = nextEventTime.Add(time.Duration(durationSec) * time.Second)
	}
	return matchTimes, breakTimes
}

// UpdateMatches updates the playoff matches in the database to assign teams based on the results of the playoff
// tournament so far.
func (tournament *PlayoffTournament) UpdateMatches(database *model.Database) error {
	matches, err := database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("cannot update playoff matches; no matches exist")
	}

	playoffMatchResults := make(map[int]playoffMatchResult)
	for _, match := range matches {
		switch match.Status {
		case game.RedWonMatch, game.BlueWonMatch, game.TieMatch:
			playoffMatchResults[match.TypeOrder] = playoffMatchResult{status: match.Status}
		}
	}

	for _, root := range tournament.roots {
		root.update(playoffMatchResults)
	}

	// Update all unplayed matches to assign any alliances that have been newly populated into or removed from matches.
	matchesByTypeOrder := make(map[int]*model.Match)
	for i, match := range matches {
		matchesByTypeOrder[match.TypeOrder] = &matches[i]
	}
	alliances, err := getAlliancesById(database)
	if err != nil {
		return err
	}

	for _, spec := range tournament.matchSpecs {
		match, ok := matchesByTypeOrder[spec.order]
		if !ok {
			return fmt.Errorf("cannot update playoff matches; match with order %d does not exist", spec.order)
		}
		if match.IsComplete() {
			continue
		}

		if spec.isHidden {
			match.Status = game.MatchHidden
		} else {
			match.Status = game.MatchScheduled
		}
		match.PlayoffRedAlliance = spec.redAllianceId
		match.PlayoffBlueAlliance = spec.blueAllianceId
		if alliance, ok := alliances[match.PlayoffRedAlliance]; ok && match.Status == game.MatchScheduled {
			positionRedTeams(match, &alliance)
		} else {
			// Zero out the teams.
			positionRedTeams(match, &model.Alliance{})
		}
		if alliance, ok := alliances[match.PlayoffBlueAlliance]; ok && match.Status == game.MatchScheduled {
			positionBlueTeams(match, &alliance)
		} else {
			// Zero out the teams.
			positionBlueTeams(match, &model.Alliance{})
		}
		if err = database.UpdateMatch(match); err != nil {
			return err
		}
	}

	return nil
}

// Returns all alliances in the database keyed by ID.
func getAlliancesById(database *model.Database) (map[int]model.Alliance, error) {
	alliances, err := database.GetAllAlliances()
	if err != nil {
		return nil, err
	}
	alliancesById := make(map[int]model.Alliance, len(alliances))
	for _, alliance := range alliances {
		alliancesById[alliance.Id] = alliance
	}
	return alliancesById, nil
}

// Assigns the lineup from the alliance into the red team slots for the match.
func positionRedTeams(match *model.Match, alliance *model.Alliance) {
	match.Red1 = alliance.Lineup[0]
	match.Red2 = alliance.Lineup[1]
	match.Red3 = alliance.Lineup[2]
}

// Assigns the lineup from the alliance into the blue team slots for the match.
func positionBlueTeams(match *model.Match, alliance *model.Alliance) {
	match.Blue1 = alliance.Lineup[0]
	match.Blue2 = alliance.Lineup[1]
	match.Blue3 = alliance.Lineup[2]
}

// Refresh updates the in-memory state of the tournament from the match results in the database, without modifying
// the database (used by field nodes, whose playoff matches are mirrored from the hub).
func (tournament *PlayoffTournament) Refresh(database *model.Database) error {
	matches, err := database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		return err
	}
	playoffMatchResults := make(map[int]playoffMatchResult)
	for _, match := range matches {
		switch match.Status {
		case game.RedWonMatch, game.BlueWonMatch, game.TieMatch:
			playoffMatchResults[match.TypeOrder] = playoffMatchResult{status: match.Status}
		}
	}
	for _, root := range tournament.roots {
		root.update(playoffMatchResults)
	}
	return nil
}
