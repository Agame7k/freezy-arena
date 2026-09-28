// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for configuring the team list.

package web

import (
	"bytes"
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/dchest/uniuri"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const wpaKeyLength = 8

// Global var to hold the team download progress percentage.
var progressPercentage float64 = 5

// Shows the team list.
func (web *Web) teamsGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	web.renderTeams(w, r, false)
}

// Adds teams to the team list.
func (web *Web) teamsPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canModifyTeamList() {
		web.renderTeams(w, r, true)
		return
	}

	conferences, err := web.arena.Database.EnsureConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	var teamNumbers []int
	teamConferences := make(map[int]int)
	teamNicknames := make(map[int]string)
	var unrecognized []string
	for _, line := range strings.Split(r.PostFormValue("teamNumbers"), "\n") {
		// Each line is a team number, optionally followed by its conference (as "team,conference").
		fields := strings.FieldsFunc(line, func(c rune) bool { return c == ',' || c == '\t' || c == ';' })
		if len(fields) == 0 {
			continue
		}
		teamNumber, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		if len(fields) > 1 {
			value := strings.TrimSpace(fields[1])
			teamConferences[teamNumber] = resolveConferenceId(value, conferences)
			if teamConferences[teamNumber] == 0 && value != "" && value != "0" && value != "-" {
				unrecognized = append(unrecognized, fmt.Sprintf("%d (%q)", teamNumber, value))
			}
		}
		if len(fields) > 2 {
			// An optional third column is the team's nickname, used unless TBA provides one.
			teamNicknames[teamNumber] = strings.TrimSpace(fields[2])
		}
		teamNumbers = append(teamNumbers, teamNumber)
	}

	progressPercentage = 5
	progressIncrement := 95.0 / float64(len(teamNumbers))
	for _, teamNumber := range teamNumbers {
		if existingTeam, err := web.arena.Database.GetTeamById(teamNumber); err == nil && existingTeam != nil {
			// Re-importing an existing team only updates its conference.
			if conferenceId, ok := teamConferences[teamNumber]; ok {
				existingTeam.ConferenceId = conferenceId
				if err = web.arena.Database.UpdateTeam(existingTeam); err != nil {
					handleWebErr(w, err)
					return
				}
			}
			progressPercentage += progressIncrement
			continue
		}
		team := model.Team{Id: teamNumber, ConferenceId: teamConferences[teamNumber]}
		if web.arena.EventSettings.TbaDownloadEnabled {
			if err := web.populateOfficialTeamInfo(&team); err != nil {
				handleWebErr(w, err)
				return
			}
		}
		if team.Nickname == "" {
			team.Nickname = teamNicknames[teamNumber]
		}
		if err := web.arena.Database.CreateTeam(&team); err != nil {
			handleWebErr(w, err)
			return
		}

		progressPercentage += progressIncrement
	}
	progressPercentage = 100
	web.arena.NotifyDataChanged()

	if len(unrecognized) > 0 {
		var names []string
		for _, conference := range conferences {
			names = append(names, conference.ShortName)
		}
		message := fmt.Sprintf(
			"Didn't recognize the conference for %s, so those teams have no conference. Use %s (or the conference "+
				"name or number), then import them again or pick their conference below.",
			strings.Join(unrecognized, ", "), strings.Join(names, " or "),
		)
		http.Redirect(w, r, "/setup/teams?error="+url.QueryEscape(message), 303)
		return
	}
	http.Redirect(w, r, "/setup/teams", 303)
}

// Re-downloads the data for all teams from TBA and overwrites any local edits.
func (web *Web) teamsRefreshHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	progInc := 95.00 / float64(len(teams))

	for _, team := range teams {
		if err = web.populateOfficialTeamInfo(&team); err != nil {
			handleWebErr(w, err)
			return
		}
		if err = web.arena.Database.UpdateTeam(&team); err != nil {
			handleWebErr(w, err)
			return
		}

		progressPercentage += progInc
	}

	progressPercentage = 100
	http.Redirect(w, r, "/setup/teams", 303)
	progressPercentage = 5
}

// Clears the team list.
func (web *Web) teamsClearHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canModifyTeamList() {
		web.renderTeams(w, r, true)
		return
	}

	err := web.arena.Database.TruncateTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	http.Redirect(w, r, "/setup/teams", 303)
}

// Shows the page to edit a team's fields.
func (web *Web) teamEditGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, _ := strconv.Atoi(r.PathValue("id"))
	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, fmt.Sprintf("Error: No such team: %d", teamId), 400)
		return
	}

	template, err := web.parseFiles("templates/edit_team.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		*model.Team
		Conferences []model.Conference
	}{web.arena.EventSettings, team, conferences}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// Updates a team's fields.
func (web *Web) teamEditPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, _ := strconv.Atoi(r.PathValue("id"))
	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, fmt.Sprintf("Error: No such team: %d", teamId), 400)
		return
	}

	team.Name = r.PostFormValue("name")
	team.Nickname = r.PostFormValue("nickname")
	team.City = r.PostFormValue("city")
	team.SchoolName = r.PostFormValue("schoolName")
	team.StateProv = r.PostFormValue("stateProv")
	team.Country = r.PostFormValue("country")
	team.RookieYear, _ = strconv.Atoi(r.PostFormValue("rookieYear"))
	team.RobotName = r.PostFormValue("robotName")
	team.Accomplishments = r.PostFormValue("accomplishments")
	if web.arena.EventSettings.NetworkSecurityEnabled {
		team.WpaKey = r.PostFormValue("wpaKey")
		if len(team.WpaKey) < 8 || len(team.WpaKey) > 63 {
			handleWebErr(w, fmt.Errorf("WPA key must be between 8 and 63 characters."))
			return
		}
	}
	team.HasConnected = r.PostFormValue("hasConnected") == "on"
	if conferenceValue := r.PostFormValue("conferenceId"); conferenceValue != "" {
		team.ConferenceId, _ = strconv.Atoi(conferenceValue)
	}
	err = web.arena.Database.UpdateTeam(team)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	http.Redirect(w, r, "/setup/teams", 303)
}

// Removes a team from the team list.
func (web *Web) teamDeletePostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canModifyTeamList() {
		web.renderTeams(w, r, true)
		return
	}

	teamId, _ := strconv.Atoi(r.PathValue("id"))
	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, fmt.Sprintf("Error: No such team: %d", teamId), 400)
		return
	}
	err = web.arena.Database.DeleteTeam(team.Id)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	http.Redirect(w, r, "/setup/teams", 303)
}

// Generates random WPA keys and saves them to the team models.
func (web *Web) teamsGenerateWpaKeysHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	generateAllKeys := false
	if all, ok := r.URL.Query()["all"]; ok {
		generateAllKeys = all[0] == "true"
	}

	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	for _, team := range teams {
		if len(team.WpaKey) == 0 || generateAllKeys {
			team.WpaKey = uniuri.NewLen(wpaKeyLength)
			if err := web.arena.Database.UpdateTeam(&team); err != nil {
				handleWebErr(w, err)
				return
			}
		}
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

// Returns the current TBA team data download progress.
func (web *Web) teamsUpdateProgressBarHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(fmt.Sprintf("%.0f", progressPercentage))); err != nil {
		handleWebErr(w, err)
		return
	}
}

func (web *Web) renderTeams(w http.ResponseWriter, r *http.Request, showErrorMessage bool) {
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	template, err := web.parseFiles("templates/setup_teams.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferences, err := web.arena.Database.EnsureConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferenceCounts := make(map[int]int)
	for _, team := range teams {
		conferenceCounts[team.ConferenceId]++
	}
	conferenceWarning := ""
	if web.arena.EventSettings.MultiConferenceEnabled && len(teams) > 0 {
		conferenceWarning = checkConferenceTeams(teams, conferences, web.arena.EventSettings).String()
	}
	data := struct {
		*model.EventSettings
		Teams             []model.Team
		ShowErrorMessage  bool
		Conferences       []model.Conference
		ConferenceCounts  map[int]int
		ConferenceWarning string
		ErrorMessage      string
	}{
		web.arena.EventSettings,
		teams,
		showErrorMessage,
		conferences,
		conferenceCounts,
		conferenceWarning,
		r.URL.Query().Get("error"),
	}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// Returns true if it is safe to change the team list (i.e. no matches/results exist yet).
func (web *Web) canModifyTeamList() bool {
	matches, err := web.arena.Database.GetMatchesByType(model.Qualification, true)
	if err != nil || len(matches) > 0 {
		return false
	}
	return true
}

// Returns the data for the given team number.
func (web *Web) populateOfficialTeamInfo(team *model.Team) error {
	tbaTeam, err := web.arena.TbaClient.GetTeam(team.Id)
	if err != nil {
		return err
	}

	// Check if the result is valid. If a team is not found, it will just not have its detail fields filled out.
	if tbaTeam.TeamNumber == 0 {
		return nil
	}

	team.Name = tbaTeam.Name
	team.Nickname = tbaTeam.Nickname
	team.City = tbaTeam.City
	team.StateProv = tbaTeam.StateProv
	team.Country = tbaTeam.Country
	schoolNameRe := regexp.MustCompile("^.*\\S&(\\S.*?$)")
	matches := schoolNameRe.FindStringSubmatch(tbaTeam.Name)
	if len(matches) > 0 {
		team.SchoolName = matches[1]
	}
	team.RookieYear = tbaTeam.RookieYear
	team.RobotName, err = web.arena.TbaClient.GetRobotName(team.Id, time.Now().Year())
	if err != nil {
		return err
	}

	// Generate string of recent awards in reverse chronological order.
	recentAwards, err := web.arena.TbaClient.GetTeamAwards(team.Id)
	if err != nil {
		return err
	}
	var accomplishmentsBuffer bytes.Buffer
	for i := len(recentAwards) - 1; i >= 0; i-- {
		award := recentAwards[i]
		if time.Now().Year()-award.Year <= 1 {
			accomplishmentsBuffer.WriteString(
				fmt.Sprintf("<p>%d %s - %s</p>", award.Year, award.EventName, award.Name),
			)
		}
	}
	team.Accomplishments = accomplishmentsBuffer.String()

	// Download and store the team's avatar; if there isn't one, ignore the error.
	if err = web.arena.TbaClient.DownloadTeamAvatar(team.Id, time.Now().Year()); err != nil {
		return err
	}

	return nil
}

// Updates the conference of one or more teams, either from the per-team dropdowns or in bulk for the selected teams.
func (web *Web) teamsConferencesPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	alliances, err := web.arena.Database.GetAllAlliances()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if len(alliances) > 0 {
		http.Redirect(w, r, "/setup/teams?error=Conferences+can%27t+change+once+alliances+exist.", 303)
		return
	}
	if err = r.ParseForm(); err != nil {
		handleWebErr(w, err)
		return
	}

	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	selected := make(map[string]bool)
	for _, teamId := range r.PostForm["selectedTeams"] {
		selected[teamId] = true
	}
	bulkConferenceId, _ := strconv.Atoi(r.PostFormValue("bulkConferenceId"))
	isBulk := r.PostFormValue("action") == "bulk"
	for _, team := range teams {
		teamIdString := strconv.Itoa(team.Id)
		conferenceId := team.ConferenceId
		if isBulk && selected[teamIdString] {
			conferenceId = bulkConferenceId
		} else if value, ok := r.PostForm["conference_"+teamIdString]; ok {
			// Changes made in the per-team dropdowns are saved whichever button was pressed.
			conferenceId, _ = strconv.Atoi(value[0])
		}
		if conferenceId != team.ConferenceId {
			team.ConferenceId = conferenceId
			if err = web.arena.Database.UpdateTeam(&team); err != nil {
				handleWebErr(w, err)
				return
			}
		}
	}
	web.arena.NotifyDataChanged()
	http.Redirect(w, r, "/setup/teams", 303)
}

// Resolves a conference given by ID, short name or name (case-insensitive) to its ID, or 0 if it doesn't match any.
func resolveConferenceId(value string, conferences []model.Conference) int {
	if id, err := strconv.Atoi(value); err == nil {
		for _, conference := range conferences {
			if conference.Id == id {
				return id
			}
		}
		return 0
	}
	for _, conference := range conferences {
		if strings.EqualFold(conference.ShortName, value) || strings.EqualFold(conference.Name, value) {
			return conference.Id
		}
	}
	return 0
}
