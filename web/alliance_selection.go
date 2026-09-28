// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for conducting the alliance selection process.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"github.com/Team254/cheesy-arena/websocket"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Global var to hold configurable time limit for selections. A value of zero disables the timer.
var allianceSelectionTimeLimitSec = 45

// Global var to hold the time limit that the current timer was started with
var currentAllianceSelectionTimeLimitSec = 0

// The time limit for the break between rounds
const allianceSelectionBreakDurationSec = 120

// Global var to hold a ticker used for the alliance selection timer.
var allianceSelectionTicker *time.Ticker

// Shows the alliance selection page.
func (web *Web) allianceSelectionGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if web.arena.EventSettings.MultiConferenceEnabled {
		// Switch the active conference if requested, or default to the first one.
		conferenceId, _ := strconv.Atoi(r.URL.Query().Get("conference"))
		if conferenceId == 0 && web.arena.AllianceSelectionConferenceId == 0 {
			conferenceId = 1
		}
		if conferenceId > 0 && conferenceId != web.arena.AllianceSelectionConferenceId {
			conference, err := web.arena.Database.GetConferenceById(conferenceId)
			if err != nil {
				handleWebErr(w, err)
				return
			}
			if conference != nil {
				web.arena.SwitchAllianceSelectionConference(conferenceId)
				web.arena.AllianceSelectionNotifier.Notify()
			}
		}
	}

	web.renderAllianceSelection(w, r, "")
}

// Returns the active conference of a multi-conference alliance selection, or nil in a single-conference event.
func (web *Web) activeAllianceSelectionConference() (*model.Conference, error) {
	if !web.arena.EventSettings.MultiConferenceEnabled {
		return nil, nil
	}
	if web.arena.AllianceSelectionConferenceId == 0 {
		web.arena.SwitchAllianceSelectionConference(1)
	}
	conference, err := web.arena.Database.GetConferenceById(web.arena.AllianceSelectionConferenceId)
	if err != nil {
		return nil, err
	}
	if conference == nil {
		return nil, fmt.Errorf("conference %d does not exist", web.arena.AllianceSelectionConferenceId)
	}
	return conference, nil
}

// Returns the ID of the first alliance of the given conference; alliance IDs are consecutive across conferences.
func (web *Web) conferenceAllianceOffset(conference *model.Conference) (int, error) {
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		return 0, err
	}
	offset := 0
	for _, other := range conferences {
		if other.Id < conference.Id {
			offset += other.NumAlliances
		}
	}
	return offset, nil
}

// Returns the saved alliances belonging to the given conference (or all alliances if conference is nil).
func (web *Web) savedConferenceAlliances(conference *model.Conference) ([]model.Alliance, error) {
	alliances, err := web.arena.Database.GetAllAlliances()
	if err != nil || conference == nil {
		return alliances, err
	}
	conferenceAlliances := []model.Alliance{}
	for _, alliance := range alliances {
		if alliance.ConferenceId == conference.Id {
			conferenceAlliances = append(conferenceAlliances, alliance)
		}
	}
	return conferenceAlliances, nil
}

// Updates the cache with the latest input from the client.
func (web *Web) allianceSelectionPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canModifyAllianceSelection() {
		web.renderAllianceSelection(w, r, "Alliance selection has already been finalized.")
		return
	}

	// Reset picked state for each team in preparation for reconstructing it.
	for i := range web.arena.AllianceSelectionRankedTeams {
		web.arena.AllianceSelectionRankedTeams[i].Picked = false
	}

	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teamConferences := make(map[int]int, len(teams))
	for _, team := range teams {
		teamConferences[team.Id] = team.ConferenceId
	}

	// Iterate through all selections and update the alliances.
	for i, alliance := range web.arena.AllianceSelectionAlliances {
		for j := range alliance.TeamIds {
			teamString := r.PostFormValue(fmt.Sprintf("selection%d_%d", i, j))
			if teamString == "" {
				web.arena.AllianceSelectionAlliances[i].TeamIds[j] = 0
			} else {
				teamId, err := strconv.Atoi(teamString)
				if err != nil {
					web.renderAllianceSelection(w, r, fmt.Sprintf("Invalid team number value '%s'.", teamString))
					return
				}
				found := false
				for k, team := range web.arena.AllianceSelectionRankedTeams {
					if team.TeamId == teamId {
						if team.Picked {
							web.renderAllianceSelection(
								w, r, fmt.Sprintf("Team %d is already part of an alliance.", teamId),
							)
							return
						}
						found = true
						web.arena.AllianceSelectionRankedTeams[k].Picked = true
						web.arena.AllianceSelectionAlliances[i].TeamIds[j] = teamId
						break
					}
				}
				if !found && web.arena.EventSettings.MultiConferenceEnabled &&
					teamConferences[teamId] != web.arena.AllianceSelectionConferenceId {
					// Server-side check that every pick belongs to the conference currently selecting.
					web.renderAllianceSelection(
						w, r, fmt.Sprintf("Team %d is not in the conference that is currently selecting.", teamId),
					)
					return
				}
				if !found {
					web.renderAllianceSelection(
						w,
						r,
						fmt.Sprintf(
							"Team %d has not played any matches at this event and is ineligible for selection.", teamId,
						),
					)
					return
				}
			}
		}
	}

	web.arena.AllianceSelectionNotifier.Notify()
	http.Redirect(w, r, "/alliance_selection", 303)
}

// Sets up the empty alliances and populates the ranked team list.
func (web *Web) allianceSelectionStartHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if len(web.arena.AllianceSelectionAlliances) != 0 {
		web.renderAllianceSelection(w, r, "Can't start alliance selection when it is already in progress.")
		return
	}
	if !web.canModifyAllianceSelection() {
		web.renderAllianceSelection(w, r, "Alliance selection has already been finalized.")
		return
	}

	conference, err := web.activeAllianceSelectionConference()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	// Create a blank alliance set matching the event (or conference) configuration.
	numAlliances := web.arena.EventSettings.NumPlayoffAlliances
	allianceOffset := 0
	if conference != nil {
		numAlliances = conference.NumAlliances
		if allianceOffset, err = web.conferenceAllianceOffset(conference); err != nil {
			handleWebErr(w, err)
			return
		}
	}
	web.arena.AllianceSelectionAlliances = make([]model.Alliance, numAlliances)
	teamsPerAlliance := 3
	if web.arena.EventSettings.SelectionRound3Order != "" {
		teamsPerAlliance = 4
	}
	for i := 0; i < numAlliances; i++ {
		web.arena.AllianceSelectionAlliances[i].Id = allianceOffset + i + 1
		web.arena.AllianceSelectionAlliances[i].Seed = i + 1
		web.arena.AllianceSelectionAlliances[i].TeamIds = make([]int, teamsPerAlliance)
		if conference != nil {
			web.arena.AllianceSelectionAlliances[i].ConferenceId = conference.Id
		}
	}

	// Populate the ranked list of teams (only the active conference's teams, by conference rank).
	rankings, err := web.arena.Database.GetAllRankings()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if conference != nil {
		teams, err := web.arena.Database.GetAllTeams()
		if err != nil {
			handleWebErr(w, err)
			return
		}
		rankings = tournament.FilterRankingsByConference(rankings, teams, conference.Id)
		if len(rankings) < numAlliances*teamsPerAlliance {
			web.arena.AllianceSelectionAlliances = []model.Alliance{}
			message := fmt.Sprintf(
				"%s has only %d ranked teams, but %d alliances of %d need %d.", conference.Name, len(rankings),
				numAlliances, teamsPerAlliance, numAlliances*teamsPerAlliance,
			)
			savedAlliances, err := web.arena.Database.GetAllAlliances()
			if err != nil {
				handleWebErr(w, err)
				return
			}
			if len(savedAlliances) == 0 {
				message += fmt.Sprintf(
					" Lower %s's number of alliances to %d or fewer on Settings → Multi-Conference, then start again.",
					conference.Name, len(rankings)/teamsPerAlliance,
				)
			} else {
				message += fmt.Sprintf(
					" The playoffs were already set up when the other conference finalized, so to change %s's size, "+
						"first use Clear Playoff/Alliance Data on the Settings page (this also clears the other "+
						"conference's alliances).", conference.Name,
				)
			}
			web.renderAllianceSelection(w, r, message)
			return
		}
	}
	web.arena.AllianceSelectionRankedTeams = make([]model.AllianceSelectionRankedTeam, len(rankings))
	for i, ranking := range rankings {
		web.arena.AllianceSelectionRankedTeams[i] = model.AllianceSelectionRankedTeam{
			Rank:   i + 1,
			TeamId: ranking.TeamId,
			Picked: false,
		}
	}

	web.arena.AllianceSelectionNotifier.Notify()
	http.Redirect(w, r, "/alliance_selection", 303)
}

// Resets the alliance selection process back to the starting point.
func (web *Web) allianceSelectionResetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canResetAllianceSelection() {
		web.renderAllianceSelection(w, r, "Cannot reset alliance selection; playoff matches have already started.")
		return
	}

	// Delete any playoff matches that were already created (but not played since they would fail the above check).
	err := web.deleteMatchDataForType(model.Playoff)
	if err != nil {
		handleWebErr(w, err)
		return
	}

	// Delete the saved alliances.
	if err = web.arena.Database.TruncateAlliances(); err != nil {
		handleWebErr(w, err)
		return
	}

	web.arena.ResetAllianceSelectionStates()
	if err = web.clearConferencePlayoffStartTimes(); err != nil {
		handleWebErr(w, err)
		return
	}
	web.arena.AllianceSelectionNotifier.Notify()
	web.arena.NotifyDataChanged()
	http.Redirect(w, r, "/alliance_selection", 303)
}

// Saves the selected alliances to the database and generates the first round of playoff matches.
func (web *Web) allianceSelectionFinalizeHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if !web.canModifyAllianceSelection() {
		web.renderAllianceSelection(w, r, "Alliance selection has already been finalized.")
		return
	}

	location, _ := time.LoadLocation("Local")
	startTime, err := time.ParseInLocation("2006-01-02 03:04:05 PM", r.PostFormValue("startTime"), location)
	if err != nil {
		web.renderAllianceSelection(w, r, "Must specify a valid start time for the playoff rounds.")
		return
	}

	if web.arena.EventSettings.MultiConferenceEnabled {
		web.finalizeConferenceAllianceSelection(w, r, startTime)
		return
	}

	// Check that all spots are filled.
	for _, alliance := range web.arena.AllianceSelectionAlliances {
		for _, allianceTeamId := range alliance.TeamIds {
			if allianceTeamId <= 0 {
				web.renderAllianceSelection(w, r, "Can't finalize alliance selection until all spots have been filled.")
				return
			}
		}
	}

	// Save alliances to the database.
	for _, alliance := range web.arena.AllianceSelectionAlliances {
		// Populate the initial lineup according to the tournament rules (alliance captain in the middle, first pick on
		// the left, second pick on the right).
		alliance.Lineup[0] = alliance.TeamIds[1]
		alliance.Lineup[1] = alliance.TeamIds[0]
		alliance.Lineup[2] = alliance.TeamIds[2]

		err := web.arena.Database.CreateAlliance(&alliance)
		if err != nil {
			handleWebErr(w, err)
			return
		}
	}

	// Generate the first round of playoff matches.
	if err = web.arena.CreatePlayoffMatches(startTime); err != nil {
		handleWebErr(w, err)
		return
	}

	// Reset yellow cards.
	err = tournament.CalculateTeamCards(web.arena.Database, model.Playoff)
	if err != nil {
		handleWebErr(w, err)
		return
	}

	// Back up the database.
	err = web.arena.Database.Backup(web.arena.EventSettings.Name, "post_alliance_selection")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	if web.arena.EventSettings.TbaPublishingEnabled {
		// Publish alliances and schedule to The Blue Alliance.
		err = web.arena.TbaClient.PublishAlliances(web.arena.Database)
		if err != nil {
			web.renderAllianceSelection(w, r, fmt.Sprintf("Failed to publish alliances: %s", err.Error()))
			return
		}
		err = web.arena.TbaClient.PublishMatches(web.arena.Database)
		if err != nil {
			web.renderAllianceSelection(w, r, fmt.Sprintf("Failed to publish matches: %s", err.Error()))
			return
		}
	}

	// Signal displays of the bracket to update themselves.
	web.arena.ScorePostedNotifier.Notify()

	// Load the first playoff match.
	matches, err := web.arena.Database.GetMatchesByType(model.Playoff, false)
	if err != nil {
		web.renderAllianceSelection(w, r, fmt.Sprintf("Failed to load playoff matches: %s", err.Error()))
		return
	}
	if len(matches) > 0 {
		if err = web.arena.LoadMatch(&matches[0]); err != nil {
			web.renderAllianceSelection(w, r, fmt.Sprintf("Failed to load playoff match: %s", err.Error()))
			return
		}
	}

	http.Redirect(w, r, "/match_play", 303)
}

// The websocket endpoint for the alliance selection client to send control commands and receive status updates.
func (web *Web) allianceSelectionWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer closeWebsocket(ws)

	// Subscribe the websocket to the notifiers whose messages will be passed on to the client, in a separate goroutine.
	go ws.HandleNotifiers(web.arena.AllianceSelectionNotifier, web.arena.AudienceDisplayModeNotifier)

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}

		switch messageType {
		case "setTimer":
			if timeLimitSec, ok := data.(float64); ok {
				allianceSelectionTimeLimitSec = int(timeLimitSec)
			} else {
				writeWebsocketError(ws, "Invalid time limit value.")
			}
		case "startTimer":
			if allianceSelectionTicker != nil {
				allianceSelectionTicker.Stop()
			}
			if web.arena.AllianceSelectionTimeRemainingSec == 0 {
				web.arena.AllianceSelectionTimeRemainingSec = allianceSelectionTimeLimitSec
				currentAllianceSelectionTimeLimitSec = allianceSelectionTimeLimitSec
			}
			web.arena.AllianceSelectionShowTimer = true
			web.arena.AllianceSelectionNotifier.Notify()
			allianceSelectionTicker = time.NewTicker(time.Second)
			go func() {
				for range allianceSelectionTicker.C {
					web.arena.AllianceSelectionTimeRemainingSec--
					web.arena.AllianceSelectionNotifier.Notify()

					if web.arena.AllianceSelectionTimeRemainingSec <= 0 {
						allianceSelectionTicker.Stop()
					}

					// Only play sounds if we are not in a break between rounds
					if currentAllianceSelectionTimeLimitSec != allianceSelectionBreakDurationSec {
						if web.arena.AllianceSelectionTimeRemainingSec == 5 {
							web.arena.PlaySound("pick_clock")
						} else if web.arena.AllianceSelectionTimeRemainingSec == 0 {
							web.arena.PlaySound("pick_clock_expired")
						}
					}
				}
			}()
		case "stopTimer":
			if allianceSelectionTicker != nil {
				allianceSelectionTicker.Stop()
			}
			web.arena.AllianceSelectionNotifier.Notify()
		case "restartTimer":
			web.arena.AllianceSelectionShowTimer = true
			web.arena.AllianceSelectionTimeRemainingSec = allianceSelectionTimeLimitSec
			currentAllianceSelectionTimeLimitSec = allianceSelectionTimeLimitSec
			web.arena.AllianceSelectionNotifier.Notify()
		case "hideTimer":
			if allianceSelectionTicker != nil {
				allianceSelectionTicker.Stop()
			}
			web.arena.AllianceSelectionShowTimer = false
			web.arena.AllianceSelectionTimeRemainingSec = 0
			web.arena.AllianceSelectionNotifier.Notify()
		case "setAudienceDisplay":
			mode, ok := data.(string)
			if !ok {
				writeWebsocketError(ws, fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			web.arena.SetAudienceDisplayMode(mode)
		default:
			writeWebsocketError(ws, fmt.Sprintf("Invalid message type '%s'.", messageType))
		}
	}
}

func (web *Web) renderAllianceSelection(w http.ResponseWriter, r *http.Request, errorMessage string) {
	if web.arena.EventSettings.IsNode() {
		errorMessage = "Alliance selection is run on the hub; this field node only mirrors its results."
	}
	conference, err := web.activeAllianceSelectionConference()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if len(web.arena.AllianceSelectionAlliances) == 0 {
		// The application may have been restarted since the alliance selection was conducted; try reloading the
		// alliances from the DB.
		web.arena.AllianceSelectionAlliances, err = web.savedConferenceAlliances(conference)
		if err != nil {
			handleWebErr(w, err)
			return
		}
	}
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if !web.arena.EventSettings.MultiConferenceEnabled {
		conferences = nil
	}

	template, err := web.parseFiles(
		"templates/alliance_selection.html", "templates/audience_display_radio_buttons.html", "templates/base.html",
	)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	nextRow, nextCol := web.determineNextCell()
	data := struct {
		*model.EventSettings
		Alliances    []model.Alliance
		RankedTeams  []model.AllianceSelectionRankedTeam
		NextRow      int
		NextCol      int
		ErrorMessage string
		TimeLimitSec int
		Conference   *model.Conference
		Conferences  []model.Conference
	}{
		web.arena.EventSettings,
		web.arena.AllianceSelectionAlliances,
		web.arena.AllianceSelectionRankedTeams,
		nextRow,
		nextCol,
		errorMessage,
		allianceSelectionTimeLimitSec,
		conference,
		conferences,
	}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// Returns true if it is safe to change the alliance selection (i.e. no playoff matches exist yet, or in a
// multi-conference event, the active conference has not been finalized yet).
func (web *Web) canModifyAllianceSelection() bool {
	if web.arena.EventSettings.IsNode() {
		// Nodes only mirror the alliances chosen on the hub.
		return false
	}
	if web.arena.EventSettings.MultiConferenceEnabled {
		conference, err := web.activeAllianceSelectionConference()
		if err != nil {
			return false
		}
		saved, err := web.savedConferenceAlliances(conference)
		return err == nil && len(saved) == 0
	}
	matches, err := web.arena.Database.GetMatchesByType(model.Playoff, true)
	if err != nil || len(matches) > 0 {
		return false
	}
	return true
}

// Returns true if it is safe to reset the alliance selection (i.e. no playoff matches have been played yet).
func (web *Web) canResetAllianceSelection() bool {
	if web.arena.EventSettings.IsNode() {
		return false
	}
	matches, err := web.arena.Database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		return false
	}
	for _, match := range matches {
		if match.IsComplete() {
			return false
		}
	}
	return true
}

// Returns the row and column of the next alliance selection spot that should have keyboard autofocus.
func (web *Web) determineNextCell() (int, int) {
	// Check the first two columns.
	for i, alliance := range web.arena.AllianceSelectionAlliances {
		if alliance.TeamIds[0] == 0 {
			return i, 0
		}
		if alliance.TeamIds[1] == 0 {
			return i, 1
		}
	}

	// Check the third column.
	if web.arena.EventSettings.SelectionRound2Order == "F" {
		for i, alliance := range web.arena.AllianceSelectionAlliances {
			if alliance.TeamIds[2] == 0 {
				return i, 2
			}
		}
	} else {
		for i := len(web.arena.AllianceSelectionAlliances) - 1; i >= 0; i-- {
			if web.arena.AllianceSelectionAlliances[i].TeamIds[2] == 0 {
				return i, 2
			}
		}
	}

	// Check the fourth column.
	if web.arena.EventSettings.SelectionRound3Order == "F" {
		for i, alliance := range web.arena.AllianceSelectionAlliances {
			if alliance.TeamIds[3] == 0 {
				return i, 3
			}
		}
	} else if web.arena.EventSettings.SelectionRound3Order == "L" {
		for i := len(web.arena.AllianceSelectionAlliances) - 1; i >= 0; i-- {
			if web.arena.AllianceSelectionAlliances[i].TeamIds[3] == 0 {
				return i, 3
			}
		}
	}
	return -1, -1
}

// Clears the playoff start time of every conference, e.g. when the playoffs are reset.
func (web *Web) clearConferencePlayoffStartTimes() error {
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		return err
	}
	for _, conference := range conferences {
		if !conference.PlayoffStartTime.IsZero() {
			conference.PlayoffStartTime = time.Time{}
			if err = web.arena.Database.UpdateConference(&conference); err != nil {
				return err
			}
		}
	}
	return nil
}
