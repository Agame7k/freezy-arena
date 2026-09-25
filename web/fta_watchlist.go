// Copyright 2026 Team 254. All Rights Reserved.
//
// Web handler for the FTA watchlist: which teams are coming up in the next few matches and which have had trouble
// at the event, so that the FTA can visit their pits before they hold up the field.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/model"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	ftaUpcomingMatchCount = 3
	// Only a team's most recent matches count toward whether it is having trouble, so that a fixed problem ages out.
	ftaRecentMatchCount = 3
	// A robot down for less than this in a match was likely just a slow reconnect, not worth a pit visit.
	ftaRobotDownWatchSec = 5
)

type ftaRiskReason struct {
	Severity model.FtaSeverity
	Message  string
}

// Everything the FTA needs at a glance to decide whether a team is worth checking on before its next match.
type ftaTeamSummary struct {
	TeamId            int
	Nickname          string
	HasConnected      bool
	FtaNotes          string
	MatchesPlayed     int
	FaultCount        int
	BrownoutCount     int
	RobotDownSec      float64
	MinBatteryVoltage float64
	LastNote          *model.FtaNote
	// The worst severity among the reasons, or "good" if there are none.
	Risk    model.FtaSeverity
	Reasons []ftaRiskReason
	score   int
}

type ftaUpcomingStation struct {
	Station string
	TeamId  int
}

type ftaUpcomingMatch struct {
	Id        int
	ShortName string
	Time      time.Time
	Stations  []ftaUpcomingStation
}

type ftaWatchlist struct {
	Upcoming []ftaUpcomingMatch
	// Every team at the event, most worrying first.
	Teams []ftaTeamSummary
}

// Returns the upcoming matches and a risk summary of every team at the event.
func (web *Web) ftaWatchlistApiHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	watchlist, err := web.getFtaWatchlist()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	writeJson(w, watchlist)
}

func (web *Web) getFtaWatchlist() (*ftaWatchlist, error) {
	upcoming, err := web.getFtaUpcomingMatches()
	if err != nil {
		return nil, err
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		return nil, err
	}
	allStats, err := web.arena.Database.GetAllFtaTeamMatchStats()
	if err != nil {
		return nil, err
	}
	allNotes, err := web.arena.Database.GetAllFtaNotes()
	if err != nil {
		return nil, err
	}

	statsByTeam := make(map[int][]model.FtaTeamMatchStats)
	for _, stats := range allStats {
		statsByTeam[stats.TeamId] = append(statsByTeam[stats.TeamId], stats)
	}
	notesByTeam := make(map[int][]model.FtaNote)
	for _, note := range allNotes {
		notesByTeam[note.TeamId] = append(notesByTeam[note.TeamId], note)
	}

	summaries := make([]ftaTeamSummary, 0, len(teams))
	for _, team := range teams {
		summaries = append(summaries, summarizeFtaTeam(team, statsByTeam[team.Id], notesByTeam[team.Id]))
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].score != summaries[j].score {
			return summaries[i].score > summaries[j].score
		}
		return summaries[i].TeamId < summaries[j].TeamId
	})
	return &ftaWatchlist{Upcoming: upcoming, Teams: summaries}, nil
}

// Returns the next few unplayed matches after the one currently loaded. From a test match, that's the next unplayed
// matches of the earliest phase of the event that still has any.
func (web *Web) getFtaUpcomingMatches() ([]ftaUpcomingMatch, error) {
	currentMatch := web.arena.CurrentMatch
	matchTypes := []model.MatchType{currentMatch.Type}
	if currentMatch.Type == model.Test {
		matchTypes = []model.MatchType{model.Practice, model.Qualification, model.Playoff}
	}

	upcoming := []ftaUpcomingMatch{}
	for _, matchType := range matchTypes {
		matches, err := web.arena.Database.GetMatchesByType(matchType, false)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if match.IsComplete() || (match.Type == currentMatch.Type && match.TypeOrder <= currentMatch.TypeOrder) {
				continue
			}
			teamIds := []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3}
			upcomingMatch := ftaUpcomingMatch{Id: match.Id, ShortName: match.ShortName, Time: match.Time}
			for i, station := range ftaStations {
				upcomingMatch.Stations = append(
					upcomingMatch.Stations, ftaUpcomingStation{Station: station, TeamId: teamIds[i]},
				)
			}
			upcoming = append(upcoming, upcomingMatch)
			if len(upcoming) == ftaUpcomingMatchCount {
				return upcoming, nil
			}
		}
		if len(upcoming) > 0 {
			break
		}
	}
	return upcoming, nil
}

// Summarizes a team's history into a list of reasons it might have trouble in its next match. Stats must be oldest
// first and notes newest first, as returned by the database.
func summarizeFtaTeam(team model.Team, stats []model.FtaTeamMatchStats, notes []model.FtaNote) ftaTeamSummary {
	summary := ftaTeamSummary{
		TeamId:       team.Id,
		Nickname:     team.Nickname,
		HasConnected: team.HasConnected,
		FtaNotes:     team.FtaNotes,
		Risk:         model.FtaSeverityGood,
		Reasons:      []ftaRiskReason{},
	}
	if len(notes) > 0 {
		summary.LastNote = &notes[0]
	}
	addReason := func(severity model.FtaSeverity, format string, args ...any) {
		summary.Reasons = append(summary.Reasons, ftaRiskReason{severity, fmt.Sprintf(format, args...)})
		if severity == model.FtaSeverityBad {
			summary.score += 3
			summary.Risk = model.FtaSeverityBad
		} else {
			summary.score++
			if summary.Risk != model.FtaSeverityBad {
				summary.Risk = model.FtaSeverityWarn
			}
		}
	}

	if !team.HasConnected {
		addReason(model.FtaSeverityBad, "Hasn't connected to the field yet")
	}

	// Test matches are left out since they're often used to deliberately pull cables and power.
	var played []model.FtaTeamMatchStats
	for _, matchStats := range stats {
		if matchStats.MatchType == model.Test {
			continue
		}
		played = append(played, matchStats)
		summary.FaultCount += matchStats.FaultCount
		summary.BrownoutCount += matchStats.BrownoutCount
		summary.RobotDownSec += matchStats.RobotDownSec
		if matchStats.MinBatteryVoltage > 0 &&
			(summary.MinBatteryVoltage == 0 || matchStats.MinBatteryVoltage < summary.MinBatteryVoltage) {
			summary.MinBatteryVoltage = matchStats.MinBatteryVoltage
		}
	}
	summary.MatchesPlayed = len(played)

	// Look at the most recent matches, newest first.
	recent := played[max(0, len(played)-ftaRecentMatchCount):]
	var downMatches, brownoutMatches []model.FtaTeamMatchStats
	var lowestBattery, highestTrip *model.FtaTeamMatchStats
	for i := len(recent) - 1; i >= 0; i-- {
		matchStats := &recent[i]
		if matchStats.RobotDownSec >= ftaRobotDownWatchSec {
			downMatches = append(downMatches, *matchStats)
		}
		if matchStats.BrownoutCount > 0 {
			brownoutMatches = append(brownoutMatches, *matchStats)
		}
		if matchStats.MinBatteryVoltage > 0 &&
			(lowestBattery == nil || matchStats.MinBatteryVoltage < lowestBattery.MinBatteryVoltage) {
			lowestBattery = matchStats
		}
		if highestTrip == nil || matchStats.MaxTripTimeMs > highestTrip.MaxTripTimeMs {
			highestTrip = matchStats
		}
	}

	if len(downMatches) == 1 {
		addReason(
			model.FtaSeverityBad,
			"Robot down %.0fs in %s",
			downMatches[0].RobotDownSec,
			ftaMatchName(downMatches[0]),
		)
	} else if len(downMatches) > 1 {
		addReason(
			model.FtaSeverityBad,
			"Robot down in %d of last %d matches (%.0fs in %s)",
			len(downMatches),
			len(recent),
			downMatches[0].RobotDownSec,
			ftaMatchName(downMatches[0]),
		)
	}
	if len(brownoutMatches) == 1 {
		addReason(model.FtaSeverityWarn, "Browned out in %s", ftaMatchName(brownoutMatches[0]))
	} else if len(brownoutMatches) > 1 {
		addReason(model.FtaSeverityBad, "Browned out in %d of last %d matches", len(brownoutMatches), len(recent))
	} else if lowestBattery != nil && lowestBattery.MinBatteryVoltage < field.FtaLowBatteryVolts {
		addReason(
			model.FtaSeverityWarn,
			"Battery sagged to %.1fV in %s",
			lowestBattery.MinBatteryVoltage,
			ftaMatchName(*lowestBattery),
		)
	}
	if highestTrip != nil && highestTrip.MaxTripTimeMs >= field.FtaHighTripTimeMs {
		addReason(
			model.FtaSeverityWarn,
			"Trip time hit %dms in %s",
			highestTrip.MaxTripTimeMs,
			ftaMatchName(*highestTrip),
		)
	}
	return summary
}

func ftaMatchName(stats model.FtaTeamMatchStats) string {
	if strings.TrimSpace(stats.MatchShortName) == "" {
		return fmt.Sprintf("match %d", stats.MatchId)
	}
	return stats.MatchShortName
}
