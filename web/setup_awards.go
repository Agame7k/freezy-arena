// Copyright 2019 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for managing awards.

package web

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"net/http"
	"strconv"
)

// Shows the awards configuration page.
func (web *Web) awardsGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/setup_awards.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	awards, err := web.arena.Database.GetAllAwards()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	// Append a blank award to the end that can be used to add a new one.
	awards = append(awards, model.Award{})

	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		*model.EventSettings
		Awards      []model.Award
		Teams       []model.Team
		Conferences []model.Conference
	}{web.arena.EventSettings, awards, teams, conferences}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// Saves the new or modified awards to the database.
func (web *Web) awardsPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	awardId, _ := strconv.Atoi(r.PostFormValue("id"))
	if r.PostFormValue("action") == "delete" {
		if err := tournament.DeleteAward(web.arena.Database, awardId); err != nil {
			handleWebErr(w, err)
			return
		}
	} else {
		teamId, _ := strconv.Atoi(r.PostFormValue("teamId"))
		conferenceId, _ := strconv.Atoi(r.PostFormValue("conferenceId"))
		award := model.Award{
			Id:           awardId,
			Type:         model.JudgedAward,
			AwardName:    r.PostFormValue("awardName"),
			TeamId:       teamId,
			PersonName:   r.PostFormValue("personName"),
			ConferenceId: conferenceId,
		}
		if existing, err := web.arena.Database.GetAwardById(awardId); err == nil && existing != nil {
			// Keep the type of generated awards (e.g. conference winners) when they are edited.
			award.Type = existing.Type
		}
		if err := tournament.CreateOrUpdateAward(web.arena.Database, &award, true); err != nil {
			handleWebErr(w, err)
			return
		}
	}

	http.Redirect(w, r, "/setup/awards", 303)
}
