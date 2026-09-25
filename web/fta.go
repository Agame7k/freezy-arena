// Copyright 2026 Team 254. All Rights Reserved.
//
// Web handlers for the FTA console: live station health, the fault log, per-team history and notes, and per-match
// connection timelines.

package web

import (
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"github.com/mitchellh/mapstructure"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ftaTeamHistoryMaxEvents = 100
	ftaMatchListMaxMatches  = 60
)

var ftaStations = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

type ftaTeamHistory struct {
	Team    *model.Team
	Station string
	Notes   []model.FtaNote
	Stats   []model.FtaTeamMatchStats
	Events  []model.FtaEvent
}

type ftaMatchListItem struct {
	Id         int
	ShortName  string
	LongName   string
	StartedAt  time.Time
	FaultCount int
	RedTeams   []int
	BlueTeams  []int
}

type ftaTimelineSample struct {
	T float64
	// How far down the connection chain the link reaches: 0 none, 1 DS, 2 radio, 3 roboRIO, 4 robot code.
	Level   int
	Enabled bool
	Battery float64
	TripMs  int
}

type ftaTimelineStation struct {
	Station string
	TeamId  int
	Samples []ftaTimelineSample
	Stats   *model.FtaTeamMatchStats
	LogUrl  string
}

type ftaTimeline struct {
	MatchId     int
	ShortName   string
	LongName    string
	DurationSec float64
	AutoEndSec  float64
	TeleopStart float64
	Stations    []ftaTimelineStation
	Events      []model.FtaEvent
}

// Renders the FTA console.
func (web *Web) ftaHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/fta.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		NoteTags []string
	}{web.arena.EventSettings, model.FtaNoteTags}
	err = template.ExecuteTemplate(w, "fta.html", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the FTA console to receive live updates and save notes.
func (web *Web) ftaWebsocketHandler(w http.ResponseWriter, r *http.Request) {
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
	go ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.ArenaStatusNotifier,
		web.arena.EventStatusNotifier,
		web.arena.MatchTimeNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.FtaEventNotifier,
		web.arena.FtaNoteNotifier,
		web.arena.ReloadDisplaysNotifier,
	)

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		command, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}

		switch command {
		case "updateTeamNotes":
			args := struct {
				TeamId int
				Notes  string
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.updateFtaTeamNotes(args.TeamId, args.Notes)
			}
		case "addNote":
			args := struct {
				TeamId int
				Tag    string
				Text   string
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.addFtaNote(args.TeamId, args.Tag, args.Text)
			}
		case "toggleBypass":
			args := struct {
				Station string
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.toggleFtaBypass(args.Station)
			}
		case "toggleFtaReady":
			// Same switch as the head referee panel's, so both always agree.
			web.arena.Plc.SetFtaReady(!web.arena.Plc.IsFtaReady())
			web.arena.ArenaStatusNotifier.Notify()
		case "deleteNote":
			args := struct {
				Id int
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.deleteFtaNote(args.Id)
			}
		default:
			err = fmt.Errorf("invalid command %q", command)
		}
		if err != nil {
			writeWebsocketError(ws, err.Error())
		}
	}
}

// Replaces the team's general FTA notes, which are shown on the field monitor as well as the FTA console.
func (web *Web) updateFtaTeamNotes(teamId int, notes string) error {
	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		return err
	}
	if team == nil {
		return fmt.Errorf("team %d is not present at the event", teamId)
	}
	team.FtaNotes = notes
	if err = web.arena.Database.UpdateTeam(team); err != nil {
		return err
	}

	// Keep the copy of the team loaded into the current match in sync so that the displays show the new notes.
	for _, allianceStation := range web.arena.AllianceStations {
		if allianceStation.Team != nil && allianceStation.Team.Id == teamId {
			allianceStation.Team.FtaNotes = notes
		}
	}
	web.arena.ArenaStatusNotifier.Notify()
	web.arena.FtaNoteNotifier.NotifyWithMessage(teamId)
	return nil
}

// Adds a timestamped, tagged note to the team's history, attributed to the current match if the team is in it.
func (web *Web) addFtaNote(teamId int, tag, text string) error {
	note := model.FtaNote{TeamId: teamId, Time: time.Now(), Tag: tag, Text: strings.TrimSpace(text)}
	if web.ftaCurrentStation(teamId) != "" && web.arena.CurrentMatch != nil {
		note.MatchId = web.arena.CurrentMatch.Id
		note.MatchShortName = web.arena.CurrentMatch.ShortName
	}
	if err := web.arena.Database.CreateFtaNote(&note); err != nil {
		return err
	}
	web.arena.FtaNoteNotifier.NotifyWithMessage(teamId)
	return nil
}

func (web *Web) deleteFtaNote(id int) error {
	note, err := web.arena.Database.GetFtaNoteById(id)
	if err != nil {
		return err
	}
	if note == nil {
		return fmt.Errorf("note %d does not exist", id)
	}
	if err = web.arena.Database.DeleteFtaNote(id); err != nil {
		return err
	}
	web.arena.FtaNoteNotifier.NotifyWithMessage(note.TeamId)
	return nil
}

// Bypasses or un-bypasses a station. Only allowed before the match, so that a stray tap can't disable a robot mid-match.
func (web *Web) toggleFtaBypass(station string) error {
	if web.arena.MatchState != field.PreMatch {
		return fmt.Errorf("stations can only be bypassed before the match starts")
	}
	return web.arena.ToggleBypass(station)
}

// Returns the station the team is in for the current match, or the empty string if it isn't in the match.
func (web *Web) ftaCurrentStation(teamId int) string {
	for station, allianceStation := range web.arena.AllianceStations {
		if allianceStation.Team != nil && allianceStation.Team.Id == teamId {
			return station
		}
	}
	return ""
}

// Returns everything the FTA knows about a team: notes, per-match connection stats, and recent faults.
func (web *Web) ftaTeamHistoryApiHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("teamId"))
	if err != nil {
		handleWebErr(w, err)
		return
	}
	history, err := web.getFtaTeamHistory(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	writeJson(w, history)
}

func (web *Web) getFtaTeamHistory(teamId int) (*ftaTeamHistory, error) {
	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		return nil, err
	}
	if team == nil {
		team = &model.Team{Id: teamId}
	}
	notes, err := web.arena.Database.GetFtaNotesByTeamId(teamId)
	if err != nil {
		return nil, err
	}
	stats, err := web.arena.Database.GetFtaTeamMatchStatsByTeamId(teamId)
	if err != nil {
		return nil, err
	}
	events, err := web.arena.Database.GetFtaEventsByTeamId(teamId)
	if err != nil {
		return nil, err
	}

	// Show the newest first, since recent history is what matters when a team walks up.
	reverse(stats)
	reverse(events)
	if len(events) > ftaTeamHistoryMaxEvents {
		events = events[:ftaTeamHistoryMaxEvents]
	}
	if notes == nil {
		notes = []model.FtaNote{}
	}
	if stats == nil {
		stats = []model.FtaTeamMatchStats{}
	}
	if events == nil {
		events = []model.FtaEvent{}
	}
	return &ftaTeamHistory{
		Team: team, Station: web.ftaCurrentStation(teamId), Notes: notes, Stats: stats, Events: events,
	}, nil
}

// Returns the faults logged since the current match was loaded.
func (web *Web) ftaCurrentEventsApiHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	events, err := web.getFtaCurrentMatchEvents()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	writeJson(w, events)
}

func (web *Web) getFtaCurrentMatchEvents() ([]model.FtaEvent, error) {
	events, err := web.arena.Database.GetFtaEventsByMatchId(web.arena.CurrentMatch.Id)
	if err != nil {
		return nil, err
	}
	// Only include events since the match was loaded, so that a replay (or the ever-reused test match) starts clean.
	currentEvents := []model.FtaEvent{}
	for _, event := range events {
		if !event.Time.Before(web.arena.CurrentMatchLoadTime) {
			currentEvents = append(currentEvents, event)
		}
	}
	return currentEvents, nil
}

// Returns the most recently played matches, for choosing which timeline to view.
func (web *Web) ftaMatchesApiHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	var matches []model.Match
	for _, matchType := range []model.MatchType{model.Practice, model.Qualification, model.Playoff} {
		matchesOfType, err := web.arena.Database.GetMatchesByType(matchType, true)
		if err != nil {
			handleWebErr(w, err)
			return
		}
		for _, match := range matchesOfType {
			if !match.StartedAt.IsZero() {
				matches = append(matches, match)
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].StartedAt.After(matches[j].StartedAt)
	})
	if len(matches) > ftaMatchListMaxMatches {
		matches = matches[:ftaMatchListMaxMatches]
	}

	events, err := web.arena.Database.GetAllFtaEvents()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	faultCounts := make(map[int]int)
	for _, event := range events {
		if event.Severity != model.FtaSeverityGood {
			faultCounts[event.MatchId]++
		}
	}

	items := []ftaMatchListItem{}
	for _, match := range matches {
		items = append(
			items,
			ftaMatchListItem{
				Id:         match.Id,
				ShortName:  match.ShortName,
				LongName:   match.LongName,
				StartedAt:  match.StartedAt,
				FaultCount: faultCounts[match.Id],
				RedTeams:   []int{match.Red1, match.Red2, match.Red3},
				BlueTeams:  []int{match.Blue1, match.Blue2, match.Blue3},
			},
		)
	}
	writeJson(w, items)
}

// Returns each station's connection level over the course of the given match, along with the faults logged.
func (web *Web) ftaTimelineApiHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	matchId, err := strconv.Atoi(r.PathValue("matchId"))
	if err != nil {
		handleWebErr(w, err)
		return
	}
	timeline, err := web.getFtaTimeline(matchId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if timeline == nil {
		http.Error(w, fmt.Sprintf("Match %d does not exist", matchId), 404)
		return
	}
	writeJson(w, timeline)
}

func (web *Web) getFtaTimeline(matchId int) (*ftaTimeline, error) {
	var match *model.Match
	var events []model.FtaEvent
	var err error
	if web.arena.CurrentMatch != nil && web.arena.CurrentMatch.Id == matchId {
		// The test match is never saved, so use the live one; this also covers the match being played right now.
		match = web.arena.CurrentMatch
		events, err = web.getFtaCurrentMatchEvents()
	} else {
		match, err = web.arena.Database.GetMatchById(matchId)
		if err == nil && match != nil {
			events, err = web.arena.Database.GetFtaEventsByMatchId(matchId)
		}
	}
	if err != nil || match == nil {
		return nil, err
	}
	allStats, err := web.arena.Database.GetFtaTeamMatchStatsByMatchId(matchId)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []model.FtaEvent{}
	}

	timeline := ftaTimeline{
		MatchId:     match.Id,
		ShortName:   match.ShortName,
		LongName:    match.LongName,
		DurationSec: game.GetDurationToTeleopEnd().Seconds(),
		AutoEndSec:  game.GetDurationToAutoEnd().Seconds(),
		TeleopStart: game.GetDurationToTeleopStart().Seconds(),
		Events:      events,
	}
	teamIds := []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3}
	for i, station := range ftaStations {
		timelineStation := ftaTimelineStation{Station: station, TeamId: teamIds[i], Samples: []ftaTimelineSample{}}
		if teamIds[i] != 0 {
			timelineStation.LogUrl = fmt.Sprintf("/match_logs/%d/%s/log", match.Id, station)
			matchLogs, err := loadMatchLogs(match.ShortName, teamIds[i])
			if err != nil {
				return nil, err
			}
			if len(matchLogs) > 0 {
				// Files are named by start time, so the last one is the most recent run of the match.
				timelineStation.Samples = timelineSamples(matchLogs[len(matchLogs)-1].Rows)
			}
			// Likewise, use the stats from the most recent run.
			for j := range allStats {
				if allStats[j].TeamId == teamIds[i] {
					timelineStation.Stats = &allStats[j]
				}
			}
		}
		timeline.Stations = append(timeline.Stations, timelineStation)
	}
	return &timeline, nil
}

func timelineSamples(rows []MatchLogRow) []ftaTimelineSample {
	samples := make([]ftaTimelineSample, 0, len(rows))
	for _, row := range rows {
		level := 0
		switch {
		case row.RobotLinked:
			level = 4
		case row.RioLinked:
			level = 3
		case row.RadioLinked:
			level = 2
		case row.DsLinked:
			level = 1
		}
		samples = append(
			samples,
			ftaTimelineSample{
				T:       row.MatchTimeSec,
				Level:   level,
				Enabled: row.Enabled,
				Battery: row.BatteryVoltage,
				TripMs:  row.DsRobotTripTimeMs,
			},
		)
	}
	return samples
}

func reverse[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func writeJson(w http.ResponseWriter, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err = w.Write(jsonData); err != nil {
		log.Println(err)
	}
}

type ftaReportRow struct {
	Team model.Team
	// Free-text fields are pre-escaped for use inside a quoted CSV field.
	FtaNotes             string
	MatchesPlayed        int
	FaultCount           int
	LowestBatteryVoltage string
	RobotDownSec         string
	NoteLog              string
}

// Summarizes every team's FTA history into one row per team for the CSV report.
func (web *Web) getFtaReportRows() ([]ftaReportRow, error) {
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

	rows := make([]ftaReportRow, 0, len(teams))
	for _, team := range teams {
		row := ftaReportRow{Team: team, FtaNotes: escapeCsvQuotes(team.FtaNotes)}
		lowestVoltage := 0.0
		downSec := 0.0
		for _, stats := range allStats {
			if stats.TeamId != team.Id {
				continue
			}
			row.MatchesPlayed++
			row.FaultCount += stats.FaultCount
			downSec += stats.RobotDownSec
			if stats.MinBatteryVoltage > 0 && (lowestVoltage == 0 || stats.MinBatteryVoltage < lowestVoltage) {
				lowestVoltage = stats.MinBatteryVoltage
			}
		}
		if lowestVoltage > 0 {
			row.LowestBatteryVoltage = fmt.Sprintf("%.1f", lowestVoltage)
		}
		row.RobotDownSec = fmt.Sprintf("%.0f", downSec)

		// List notes oldest first so the log reads chronologically.
		var noteLines []string
		for i := len(allNotes) - 1; i >= 0; i-- {
			note := allNotes[i]
			if note.TeamId != team.Id {
				continue
			}
			prefix := note.Tag
			if note.MatchShortName != "" {
				prefix = note.MatchShortName + " " + prefix
			}
			noteLines = append(noteLines, fmt.Sprintf("[%s] %s", prefix, note.Text))
		}
		row.NoteLog = escapeCsvQuotes(strings.Join(noteLines, " | "))
		rows = append(rows, row)
	}
	return rows, nil
}

// Doubles any quotes so that the text can be placed inside a quoted CSV field.
func escapeCsvQuotes(text string) string {
	return strings.ReplaceAll(text, `"`, `""`)
}
