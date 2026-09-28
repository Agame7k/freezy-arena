// Copyright 2026 Team 254. All Rights Reserved.
//
// Development-only helpers for dry runs of multi-conference and multi-field events without real teams or hardware.
// They are only available when running with -simulate.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/playoff"
	"github.com/Team254/cheesy-arena/tournament"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Seeds an empty event with simulated teams (split evenly into two conferences if requested) and a qualification
// schedule, so that a hub and its nodes can be exercised end to end.
func (web *Web) devSeedEventHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if !web.arena.SimulateHardware {
		http.Error(w, "seeding is only available when running with -simulate", http.StatusConflict)
		return
	}
	if web.arena.EventSettings.IsNode() {
		http.Error(w, "seed the hub; nodes mirror its data", http.StatusConflict)
		return
	}
	numTeams := formIntDefault(r, "teams", 36)
	matchesPerTeam := formIntDefault(r, "matchesPerTeam", 8)
	spacingSec := formIntDefault(r, "spacingSec", 60)
	multiConference := r.FormValue("conferences") != "false"
	firstTeam := formIntDefault(r, "firstTeam", 1001)
	conferenceNames := [2][2]string{
		{formValueDefault(r, "conf1Name", ""), formValueDefault(r, "conf1Short", "")},
		{formValueDefault(r, "conf2Name", ""), formValueDefault(r, "conf2Short", "")},
	}

	existingMatches, err := web.arena.Database.GetMatchesByType(model.Qualification, true)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	existingTeams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if len(existingMatches) > 0 || len(existingTeams) > 0 {
		http.Error(w, "the event already has teams or a schedule; start from an empty database", http.StatusConflict)
		return
	}

	// Build the teams, either from a roster ("number,conference,nickname" per line) or as placeholders, and check them
	// before anything is saved so that a mistake doesn't leave a half-seeded event behind.
	var teams []model.Team
	if strings.TrimSpace(r.FormValue("roster")) != "" {
		// Match the conference column against the given names, or the existing conference names if none were given.
		rosterNames := conferenceNames
		if multiConference {
			existing, err := web.arena.Database.EnsureConferences()
			if err != nil {
				handleWebErr(w, err)
				return
			}
			for i, conference := range existing {
				if i < len(rosterNames) && rosterNames[i][0] == "" && rosterNames[i][1] == "" {
					rosterNames[i] = [2]string{conference.Name, conference.ShortName}
				}
			}
		}
		var err error
		if teams, err = parseSeedRoster(r.FormValue("roster"), multiConference, rosterNames); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		numTeams = len(teams)
	} else {
		for i := 0; i < numTeams; i++ {
			team := model.Team{Id: firstTeam + i, Nickname: fmt.Sprintf("Sim Team %d", firstTeam+i), WpaKey: "simulated"}
			if multiConference {
				team.ConferenceId = 1 + i%2
				if name := conferenceNames[i%2][0]; name != "" {
					team.Nickname = fmt.Sprintf("%s Team %d", name, i/2+1)
				}
			}
			teams = append(teams, team)
		}
	}
	conferenceSizes := make(map[int]int)
	for _, team := range teams {
		conferenceSizes[team.ConferenceId]++
	}
	if multiConference {
		for conferenceId := 1; conferenceId <= model.NumConferences; conferenceId++ {
			if conferenceSizes[conferenceId] < 6 {
				http.Error(
					w,
					fmt.Sprintf(
						"conference %d has %d teams; each conference needs at least 6 for a playoff bracket",
						conferenceId, conferenceSizes[conferenceId],
					),
					http.StatusBadRequest,
				)
				return
			}
		}
	}

	// Generate the qualification schedule.
	settings := web.arena.EventSettings
	fieldAssignment := settings.QualFieldAssignmentMode
	switch r.FormValue("fieldAssignment") {
	case "":
	case "alternate":
		fieldAssignment = model.AlternateFieldAssignment
	case "blocks":
		fieldAssignment = model.BlocksFieldAssignment
	case "dynamic":
		fieldAssignment = model.DynamicFieldAssignment
	default:
		http.Error(w, "fieldAssignment must be alternate, blocks or dynamic", http.StatusBadRequest)
		return
	}
	minTurnaroundSec := settings.MinTurnaroundSec
	if value, err := strconv.Atoi(r.FormValue("minTurnaroundSec")); err == nil && value >= 0 {
		minTurnaroundSec = value
	}
	numMatches := (numTeams*matchesPerTeam + tournament.TeamsPerMatch - 1) / tournament.TeamsPerMatch
	blocks := []model.ScheduleBlock{
		{
			MatchType:       model.Qualification,
			StartTime:       time.Now().Add(time.Minute).Truncate(time.Minute),
			NumMatches:      numMatches,
			MatchSpacingSec: spacingSec,
		},
	}
	matches, report, err := tournament.BuildScheduleWithOptions(
		teams,
		blocks,
		model.Qualification,
		tournament.ScheduleOptions{
			PreferMixedConferences: multiConference,
			SearchIterations:       1000,
			MultiField:             settings.IsHub(),
			AssignmentMode:         fieldAssignment,
		},
	)
	if err != nil {
		http.Error(w, "failed to build the schedule: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Size each conference's bracket to fit its teams, and check the result before saving anything.
	var conferences []model.Conference
	if multiConference {
		// Creating the default conference records is harmless if the rest of the seed fails.
		if conferences, err = web.arena.Database.EnsureConferences(); err != nil {
			handleWebErr(w, err)
			return
		}
		for i := range conferences {
			conference := &conferences[i]
			conferenceId := conference.Id
			if names := conferenceNames[conferenceId-1]; names[0] != "" {
				conference.Name = names[0]
				conference.ShortName = names[1]
				if conference.ShortName == "" {
					conference.ShortName = strings.TrimRight(names[0][:min(3, len(names[0]))], "0123456789")
				}
			}
			perConference := conferenceSizes[conferenceId]
			conference.PlayoffType = model.DoubleEliminationPlayoff
			conference.NumAlliances = min(8, perConference/3)
			if conference.NumAlliances < 4 {
				conference.PlayoffType = model.SingleEliminationPlayoff
				conference.NumAlliances = max(2, perConference/3)
			}
		}
		_, err = playoff.NewMultiConferencePlayoffTournament(
			conferences, playoff.MultiConferenceOptionsFromSettings(settings),
		)
		if err != nil {
			http.Error(w, "invalid conferences: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Everything checks out; save it.
	for _, team := range teams {
		if err = web.arena.Database.CreateTeam(&team); err != nil {
			handleWebErr(w, err)
			return
		}
	}
	updatedSettings := *settings
	updatedSettings.TbaDownloadEnabled = false
	updatedSettings.QualFieldAssignmentMode = fieldAssignment
	updatedSettings.MinTurnaroundSec = minTurnaroundSec
	if multiConference {
		for i := range conferences {
			if err = web.arena.Database.UpdateConference(&conferences[i]); err != nil {
				handleWebErr(w, err)
				return
			}
		}
		updatedSettings.MultiConferenceEnabled = true
		updatedSettings.PreferMixedConferenceAlliances = true
	}
	if err = web.arena.Database.UpdateEventSettings(&updatedSettings); err != nil {
		handleWebErr(w, err)
		return
	}
	if err = web.arena.LoadSettings(); err != nil {
		handleWebErr(w, err)
		return
	}
	for _, block := range blocks {
		if err = web.arena.Database.CreateScheduleBlock(&block); err != nil {
			handleWebErr(w, err)
			return
		}
	}
	for _, match := range matches {
		if err = web.arena.Database.CreateMatch(&match); err != nil {
			handleWebErr(w, err)
			return
		}
	}
	web.arena.NotifyDataChanged()
	web.arena.MatchListNotifier.Notify()
	writeJsonResponse(
		w,
		map[string]any{
			"Teams":                numTeams,
			"Matches":              len(matches),
			"MixedAlliancePercent": report.MixedAlliancePercent,
			"Seed":                 report.Seed,
		},
	)
}

// Parses a roster of "number,conference,nickname" lines. The conference column may hold either conference's name or
// short name (case-insensitive); blank lines and lines starting with a non-number (e.g. a header) are skipped.
func parseSeedRoster(roster string, multiConference bool, conferenceNames [2][2]string) ([]model.Team, error) {
	var teams []model.Team
	seen := make(map[int]bool)
	for lineNumber, line := range strings.Split(roster, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		teamId, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		if teamId <= 0 {
			return nil, fmt.Errorf("line %d: invalid team number %d", lineNumber+1, teamId)
		}
		if seen[teamId] {
			return nil, fmt.Errorf("line %d: team %d is listed more than once", lineNumber+1, teamId)
		}
		seen[teamId] = true
		team := model.Team{Id: teamId, WpaKey: "simulated"}
		if multiConference {
			conference := ""
			if len(fields) > 1 {
				conference = strings.TrimSpace(fields[1])
			}
			for i, names := range conferenceNames {
				for _, name := range names {
					if name != "" && strings.EqualFold(conference, name) {
						team.ConferenceId = i + 1
					}
				}
			}
			if team.ConferenceId == 0 {
				return nil, fmt.Errorf(
					"line %d: team %d has conference %q, which is neither %s nor %s", lineNumber+1, teamId,
					conference, describeSeedConference(conferenceNames[0]), describeSeedConference(conferenceNames[1]),
				)
			}
		}
		if len(fields) > 2 {
			team.Nickname = strings.TrimSpace(strings.Join(fields[2:], ","))
		}
		teams = append(teams, team)
	}
	if len(teams) == 0 {
		return nil, fmt.Errorf("the roster doesn't contain any teams")
	}
	return teams, nil
}

func describeSeedConference(names [2]string) string {
	return fmt.Sprintf("%q/%q", names[0], names[1])
}

// Runs alliance selection automatically from the rankings (captains in rank order, then the best remaining teams in
// serpentine order) and finalizes it, for each conference in a multi-conference event.
func (web *Web) devAutoAllianceSelectionHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if !web.arena.SimulateHardware {
		http.Error(w, "automatic alliance selection is only available when running with -simulate", http.StatusConflict)
		return
	}
	alliances, err := web.arena.Database.GetAllAlliances()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if len(alliances) > 0 {
		http.Error(w, "alliances already exist", http.StatusConflict)
		return
	}
	rankings, err := web.arena.Database.GetAllRankings()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	type group struct {
		conferenceId int
		numAlliances int
	}
	groups := []group{{0, web.arena.EventSettings.NumPlayoffAlliances}}
	if web.arena.EventSettings.MultiConferenceEnabled {
		conferences, err := web.arena.Database.GetAllConferences()
		if err != nil {
			handleWebErr(w, err)
			return
		}
		groups = nil
		for _, conference := range conferences {
			groups = append(groups, group{conference.Id, conference.NumAlliances})
		}
	}

	startTime := time.Now().Add(time.Minute).Truncate(time.Minute)
	nextId := 1
	for _, group := range groups {
		groupRankings := rankings
		if group.conferenceId > 0 {
			groupRankings = tournament.FilterRankingsByConference(rankings, teams, group.conferenceId)
		}
		if len(groupRankings) < group.numAlliances*3 {
			http.Error(
				w,
				fmt.Sprintf("only %d ranked teams for %d alliances", len(groupRankings), group.numAlliances),
				http.StatusConflict,
			)
			return
		}
		picks := make([][]int, group.numAlliances)
		next := 0
		for round := 0; round < 3; round++ {
			for i := 0; i < group.numAlliances; i++ {
				index := i
				if round == 2 {
					index = group.numAlliances - 1 - i
				}
				picks[index] = append(picks[index], groupRankings[next].TeamId)
				next++
			}
		}
		for seed, teamIds := range picks {
			alliance := model.Alliance{
				Id:           nextId,
				TeamIds:      teamIds,
				Lineup:       [3]int{teamIds[1], teamIds[0], teamIds[2]},
				ConferenceId: group.conferenceId,
				Seed:         seed + 1,
			}
			if err = web.arena.Database.CreateAlliance(&alliance); err != nil {
				handleWebErr(w, err)
				return
			}
			nextId++
		}
		if group.conferenceId > 0 {
			conference, err := web.arena.Database.GetConferenceById(group.conferenceId)
			if err != nil {
				handleWebErr(w, err)
				return
			}
			conference.PlayoffStartTime = startTime
			if err = web.arena.Database.UpdateConference(conference); err != nil {
				handleWebErr(w, err)
				return
			}
		}
	}

	if err = web.arena.CreatePlayoffMatches(startTime); err != nil {
		handleWebErr(w, err)
		return
	}
	if err = web.arena.UpdatePlayoffTournament(); err != nil {
		handleWebErr(w, err)
		return
	}
	web.arena.ScorePostedNotifier.Notify()
	web.arena.NotifyDataChanged()
	matches, _ := web.arena.Database.GetMatchesByType(model.Playoff, true)
	writeJsonResponse(w, map[string]any{"Alliances": nextId - 1, "PlayoffMatches": len(matches)})
}

func formIntDefault(r *http.Request, name string, defaultValue int) int {
	value, err := strconv.Atoi(r.FormValue(name))
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func formValueDefault(r *http.Request, name, defaultValue string) string {
	if value := r.FormValue(name); value != "" {
		return value
	}
	return defaultValue
}
