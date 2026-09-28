// Copyright 2026 Team 254. All Rights Reserved.
//
// Per-field summary shown on the hub's Event Control page and dual-field displays.

package hub

import (
	"github.com/Team254/cheesy-arena/model"
	"sort"
	"strconv"
	"time"
)

const numUpcomingMatches = 4

// FieldOverview summarizes one field for the hub dashboard.
type FieldOverview struct {
	FieldId          int
	FieldName        string
	Connected        bool
	CurrentMatchName string
	MatchStateName   string
	MatchTimeSec     int
	EarlyLateMessage string
	TeamIds          []int
	RedScore         int
	BlueScore        int
	Upcoming         []UpcomingMatch
}

// UpcomingMatch is an unplayed match assigned to a field.
type UpcomingMatch struct {
	Id        int
	ShortName string
	LongName  string
	Time      time.Time
	TeamIds   []int
}

// FieldOverviews returns a summary of each field that has a node registered or matches assigned to it.
func (hub *Hub) FieldOverviews() []FieldOverview {
	overviews := make(map[int]*FieldOverview)
	for _, status := range hub.NodeStatuses() {
		overviews[status.FieldId] = &FieldOverview{
			FieldId:          status.FieldId,
			FieldName:        status.FieldName,
			Connected:        status.Connected,
			CurrentMatchName: status.Report.CurrentMatchName,
			MatchStateName:   status.Report.MatchStateName,
			MatchTimeSec:     status.Report.MatchTimeSec,
			EarlyLateMessage: status.Report.EarlyLateMessage,
			TeamIds:          status.Report.TeamIds,
			RedScore:         status.Report.RedScore,
			BlueScore:        status.Report.BlueScore,
		}
	}

	var matches []model.Match
	for _, matchType := range []model.MatchType{model.Practice, model.Qualification, model.Playoff} {
		typeMatches, err := hub.arena.Database.GetMatchesByType(matchType, false)
		if err == nil {
			matches = append(matches, typeMatches...)
		}
	}
	for _, match := range matches {
		if match.FieldId == 0 || match.IsComplete() {
			continue
		}
		overview, ok := overviews[match.FieldId]
		if !ok {
			overview = &FieldOverview{FieldId: match.FieldId}
			overviews[match.FieldId] = overview
		}
		if len(overview.Upcoming) < numUpcomingMatches {
			overview.Upcoming = append(
				overview.Upcoming,
				UpcomingMatch{
					Id:        match.Id,
					ShortName: match.ShortName,
					LongName:  match.LongName,
					Time:      match.Time,
					TeamIds:   []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3},
				},
			)
		}
	}

	var result []FieldOverview
	for _, overview := range overviews {
		if overview.FieldName == "" {
			overview.FieldName = "Field " + strconv.Itoa(overview.FieldId)
		}
		result = append(result, *overview)
	}
	sort.Slice(
		result,
		func(i, j int) bool {
			return result[i].FieldId < result[j].FieldId
		},
	)
	return result
}

// AssignableMatchGroup is a set of unplayed matches, all on the same field (or on none), that the hub admin can move.
type AssignableMatchGroup struct {
	Label   string
	FieldId int
	Matches []model.Match
}

// AssignableMatches returns the unplayed, visible matches grouped by the field that they are assigned to, starting with
// the ones that have no field yet. Groups without any matches are left out.
func (hub *Hub) AssignableMatches() []AssignableMatchGroup {
	groups := []AssignableMatchGroup{{Label: "No field yet"}}
	for fieldId := 1; fieldId <= model.MaxFieldId; fieldId++ {
		groups = append(groups, AssignableMatchGroup{Label: "On Field " + strconv.Itoa(fieldId), FieldId: fieldId})
	}
	for _, matchType := range []model.MatchType{model.Practice, model.Qualification, model.Playoff} {
		matches, err := hub.arena.Database.GetMatchesByType(matchType, false)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if !match.IsComplete() && match.FieldId >= 0 && match.FieldId < len(groups) {
				groups[match.FieldId].Matches = append(groups[match.FieldId].Matches, match)
			}
		}
	}
	var nonEmpty []AssignableMatchGroup
	for _, group := range groups {
		if len(group.Matches) > 0 {
			nonEmpty = append(nonEmpty, group)
		}
	}
	return nonEmpty
}
