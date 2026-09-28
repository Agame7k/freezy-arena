// Copyright 2026 Team 254. All Rights Reserved.
//
// Multi-conference and multi-field variants of the CSV and PDF reports.

package web

import (
	"bytes"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"net/http"
	"strconv"
)

// Returns the conference name for the given ID, or an empty string, for use in report templates.
func conferenceNameHelper(conferences map[int]model.Conference, conferenceId int) string {
	if conference, ok := conferences[conferenceId]; ok {
		return conference.Name
	}
	return ""
}

// Returns the conference selected by the "conference" query parameter, or nil if none (or multi-conference mode is
// off).
func (web *Web) reportConference(r *http.Request) (*model.Conference, error) {
	if !web.arena.EventSettings.MultiConferenceEnabled {
		return nil, nil
	}
	conferenceId, _ := strconv.Atoi(r.URL.Query().Get("conference"))
	if conferenceId == 0 {
		return nil, nil
	}
	return web.arena.Database.GetConferenceById(conferenceId)
}

// Filters the rankings to the conference selected in the request (renumbered within the conference), returning a
// title prefix naming the conference.
func (web *Web) filterRankingsForReport(r *http.Request, rankings game.Rankings) (game.Rankings, string, error) {
	conference, err := web.reportConference(r)
	if err != nil || conference == nil {
		return rankings, "", err
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		return nil, "", err
	}
	return tournament.FilterRankingsByConference(rankings, teams, conference.Id), conference.Name + " ", nil
}

// Filters the matches to the field selected by the "field" query parameter, returning a title suffix naming the field.
func filterMatchesForReport(r *http.Request, matches []model.Match) ([]model.Match, string) {
	fieldId, _ := strconv.Atoi(r.URL.Query().Get("field"))
	if fieldId == 0 {
		return matches, ""
	}
	var filtered []model.Match
	for _, match := range matches {
		if match.FieldId == fieldId {
			filtered = append(filtered, match)
		}
	}
	return filtered, " - Field " + strconv.Itoa(fieldId)
}

// Writes the given CSV template with the given data, as the other CSV reports do.
func (web *Web) writeCsvReport(w http.ResponseWriter, templateName string, data any) {
	// Don't set the content type as "text/csv", as that will trigger an automatic download in the browser.
	w.Header().Set("Content-Type", "text/plain")
	template, err := web.parseFiles("templates/" + templateName)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	var buf bytes.Buffer
	if err = template.ExecuteTemplate(&buf, templateName, data); err != nil {
		handleWebErr(w, err)
		return
	}

	// Strip out carriage returns to ensure consistent behavior across platforms.
	cleaned := bytes.ReplaceAll(buf.Bytes(), []byte("\r"), []byte(""))
	if _, err = w.Write(cleaned); err != nil {
		handleWebErr(w, err)
	}
}

func (web *Web) conferenceMap() (map[int]model.Conference, error) {
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		return nil, err
	}
	return model.ConferenceMap(conferences), nil
}

// Generates the rankings CSV with each team's conference and conference rank, optionally for one conference.
func (web *Web) conferenceRankingsCsvReport(w http.ResponseWriter, r *http.Request) {
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
	conferences, err := web.conferenceMap()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferenceId, _ := strconv.Atoi(r.URL.Query().Get("conference"))
	web.writeCsvReport(
		w,
		"rankings_conference.csv",
		struct {
			Rankings    []tournament.ConferenceRanking
			Conferences map[int]model.Conference
		}{tournament.ConferenceRankings(rankings, teams, conferenceId), conferences},
	)
}

// Generates the team list CSV with each team's conference.
func (web *Web) conferenceTeamsCsvReport(w http.ResponseWriter) {
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	conferences, err := web.conferenceMap()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	web.writeCsvReport(
		w,
		"teams_conference.csv",
		struct {
			Teams       []model.Team
			Conferences map[int]model.Conference
		}{teams, conferences},
	)
}

// Generates the schedule CSV with the field and conference of each match, optionally for one field.
func (web *Web) multiScheduleCsvReport(w http.ResponseWriter, r *http.Request, matches []model.Match) {
	conferences, err := web.conferenceMap()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	matches, _ = filterMatchesForReport(r, matches)
	web.writeCsvReport(
		w,
		"schedule_multi.csv",
		struct {
			Matches     []model.Match
			Conferences map[int]model.Conference
		}{matches, conferences},
	)
}

type rankingsReportSection struct {
	titlePrefix string
	rankings    game.Rankings
}

// Splits the rankings report into one section per conference in a multi-conference event (unless the request asks
// for one conference), or a single section otherwise.
func (web *Web) rankingsReportSections(r *http.Request, rankings game.Rankings) ([]rankingsReportSection, error) {
	if !web.arena.EventSettings.MultiConferenceEnabled || r.URL.Query().Get("conference") != "" {
		filtered, titlePrefix, err := web.filterRankingsForReport(r, rankings)
		if err != nil {
			return nil, err
		}
		return []rankingsReportSection{{titlePrefix, filtered}}, nil
	}
	conferences, err := web.arena.Database.GetAllConferences()
	if err != nil {
		return nil, err
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		return nil, err
	}
	var sections []rankingsReportSection
	for _, conference := range conferences {
		sections = append(
			sections,
			rankingsReportSection{
				conference.Name + " ", tournament.FilterRankingsByConference(rankings, teams, conference.Id),
			},
		)
	}
	return sections, nil
}
