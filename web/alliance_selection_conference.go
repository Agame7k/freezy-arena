// Copyright 2026 Team 254. All Rights Reserved.
//
// Finalizing alliance selection in a multi-conference event, either one conference at a time or both at once.

package web

import (
	"fmt"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"log"
	"net/http"
	"time"
)

// Saves the alliances of the active conference (or of both conferences, depending on the finalize mode) and creates
// or updates the playoff matches. The first finalize creates the whole tournament; the other conference's matches stay
// as placeholders until it is finalized too, at which point they are filled in and rescheduled from its start time.
func (web *Web) finalizeConferenceAllianceSelection(w http.ResponseWriter, r *http.Request, startTime time.Time) {
	activeConference, err := web.activeAllianceSelectionConference()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	// Determine which conferences are being finalized now.
	var toFinalize []model.Conference
	if web.arena.EventSettings.AllianceFinalizeMode == model.FinalizeBothAtOnce {
		toFinalize = conferences
	} else {
		toFinalize = []model.Conference{*activeConference}
	}

	// Check that every alliance of those conferences is complete.
	for _, conference := range toFinalize {
		alliances := web.arena.ConferenceAllianceSelectionAlliances(conference.Id)
		if len(alliances) == 0 {
			web.renderAllianceSelection(
				w, r, fmt.Sprintf("Alliance selection for %s hasn't been started yet.", conference.Name),
			)
			return
		}
		for _, alliance := range alliances {
			for _, allianceTeamId := range alliance.TeamIds {
				if allianceTeamId <= 0 {
					web.renderAllianceSelection(
						w,
						r,
						fmt.Sprintf(
							"Can't finalize alliance selection until all spots have been filled for %s.",
							conference.Name,
						),
					)
					return
				}
			}
		}
		saved, err := web.savedConferenceAlliances(&conference)
		if err != nil {
			handleWebErr(w, err)
			return
		}
		if len(saved) > 0 {
			web.renderAllianceSelection(
				w, r, fmt.Sprintf("Alliance selection for %s has already been finalized.", conference.Name),
			)
			return
		}
	}

	// Save the alliances and each conference's playoff start time.
	for _, conference := range toFinalize {
		for _, alliance := range web.arena.ConferenceAllianceSelectionAlliances(conference.Id) {
			// Populate the initial lineup according to the tournament rules (alliance captain in the middle, first pick
			// on the left, second pick on the right).
			alliance.Lineup[0] = alliance.TeamIds[1]
			alliance.Lineup[1] = alliance.TeamIds[0]
			alliance.Lineup[2] = alliance.TeamIds[2]
			alliance.ConferenceId = conference.Id
			if err = web.arena.Database.CreateAlliance(&alliance); err != nil {
				handleWebErr(w, err)
				return
			}
		}
		conference.PlayoffStartTime = startTime
		if err = web.arena.Database.UpdateConference(&conference); err != nil {
			handleWebErr(w, err)
			return
		}
	}

	// Create the playoff matches on the first finalize, or fill in the placeholders on later ones.
	playoffMatches, err := web.arena.Database.GetMatchesByType(model.Playoff, true)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if len(playoffMatches) == 0 {
		err = web.arena.CreatePlayoffMatches(startTime)
	} else {
		err = web.arena.ReschedulePlayoffMatches(startTime)
	}
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if err = web.arena.UpdatePlayoffTournament(); err != nil {
		handleWebErr(w, err)
		return
	}

	// Reset yellow cards.
	if err = tournament.CalculateTeamCards(web.arena.Database, model.Playoff); err != nil {
		handleWebErr(w, err)
		return
	}

	backupName := "post_alliance_selection"
	if len(toFinalize) == 1 {
		backupName += "_" + toFinalize[0].ShortName
	}
	if err = web.arena.Database.Backup(web.arena.EventSettings.Name, backupName); err != nil {
		handleWebErr(w, err)
		return
	}

	// Signal displays of the bracket to update themselves, and nodes to pick up the new matches.
	web.arena.ScorePostedNotifier.Notify()
	web.arena.NotifyDataChanged()

	// Switch to the conference that still needs to pick, if any.
	for _, conference := range conferences {
		saved, err := web.savedConferenceAlliances(&conference)
		if err == nil && len(saved) == 0 {
			web.arena.SwitchAllianceSelectionConference(conference.Id)
			web.arena.AllianceSelectionNotifier.Notify()
			http.Redirect(w, r, fmt.Sprintf("/alliance_selection?conference=%d", conference.Id), 303)
			return
		}
	}

	if !web.arena.EventSettings.IsHub() {
		// Load the first playoff match that is ready to play on this field.
		if err = web.loadFirstPlayablePlayoffMatch(); err != nil {
			log.Printf("Failed to load the first playoff match: %v", err)
		}
	}
	if web.arena.EventSettings.IsHub() {
		http.Redirect(w, r, "/event_control", 303)
		return
	}
	http.Redirect(w, r, "/match_play", 303)
}

// Loads the first unplayed playoff match that has both alliances filled in.
func (web *Web) loadFirstPlayablePlayoffMatch() error {
	matches, err := web.arena.Database.GetMatchesByType(model.Playoff, false)
	if err != nil {
		return err
	}
	for _, match := range web.arena.FilterMatchesForField(matches) {
		if !match.IsComplete() && match.Red1 > 0 && match.Blue1 > 0 {
			return web.arena.LoadMatch(&match)
		}
	}
	return nil
}
