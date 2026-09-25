// Copyright 2026 Team 254. All Rights Reserved.

package web

import (
	"encoding/json"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestFtaConsole(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/fta")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "FTA - Untitled Event - Cheesy Arena")
	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.Contains(t, body, `id="station`+station+`"`)
	}
	for _, tag := range model.FtaNoteTags {
		assert.Contains(t, body, `data-tag="`+tag+`"`)
	}
}

func TestFtaConsoleRequiresAdmin(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.AdminPassword = "secret"

	for _, path := range []string{
		"/fta", "/api/fta/events/current", "/api/fta/matches", "/api/fta/matches/1/timeline", "/api/fta/teams/254",
		"/api/fta/watchlist",
	} {
		recorder := web.getHttpResponse(path)
		assert.Equal(t, 307, recorder.Code, path)
		assert.Contains(t, recorder.Header().Get("Location"), "/login", path)
	}
}

func TestFtaTeamHistoryApi(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, Nickname: "The Cheesy Poofs", FtaNotes: "Spare radio"}))
	assert.Nil(t, web.arena.Database.CreateFtaNote(&model.FtaNote{TeamId: 254, Tag: "radio", Text: "Rebooted radio"}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{MatchId: 1, TeamId: 254}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{MatchId: 2, TeamId: 254}))
	assert.Nil(t, web.arena.Database.CreateFtaEvent(&model.FtaEvent{MatchId: 1, TeamId: 254, Type: model.FtaEventEStop}))
	assert.Nil(t, web.arena.Database.CreateFtaEvent(&model.FtaEvent{MatchId: 2, TeamId: 254, Type: model.FtaEventBypass}))
	assert.Nil(t, web.arena.Database.CreateFtaEvent(&model.FtaEvent{MatchId: 2, TeamId: 1114}))

	recorder := web.getHttpResponse("/api/fta/teams/254")
	assert.Equal(t, 200, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	var history ftaTeamHistory
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &history))
	assert.Equal(t, "The Cheesy Poofs", history.Team.Nickname)
	assert.Equal(t, "Spare radio", history.Team.FtaNotes)
	assert.Len(t, history.Notes, 1)
	if assert.Len(t, history.Stats, 2) {
		// Newest first.
		assert.Equal(t, 2, history.Stats[0].MatchId)
	}
	if assert.Len(t, history.Events, 2) {
		assert.Equal(t, model.FtaEventBypass, history.Events[0].Type)
	}

	// A team that isn't in the database still returns an empty history rather than an error.
	recorder = web.getHttpResponse("/api/fta/teams/9999")
	assert.Equal(t, 200, recorder.Code)
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &history))
	assert.Equal(t, 9999, history.Team.Id)
	assert.Empty(t, history.Notes)
	assert.Empty(t, history.Events)
}

func TestFtaCurrentEventsApi(t *testing.T) {
	web := setupTestWeb(t)
	matchId := web.arena.CurrentMatch.Id
	loadTime := web.arena.CurrentMatchLoadTime
	events := []model.FtaEvent{
		{MatchId: matchId, Time: loadTime.Add(-time.Minute), Message: "before load"},
		{MatchId: matchId, Time: loadTime.Add(time.Second), Message: "after load"},
		{MatchId: matchId + 100, Time: loadTime.Add(time.Second), Message: "other match"},
	}
	for i := range events {
		assert.Nil(t, web.arena.Database.CreateFtaEvent(&events[i]))
	}

	recorder := web.getHttpResponse("/api/fta/events/current")
	assert.Equal(t, 200, recorder.Code)
	var currentEvents []model.FtaEvent
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &currentEvents))
	if assert.Len(t, currentEvents, 1) {
		assert.Equal(t, "after load", currentEvents[0].Message)
	}
}

func TestFtaMatchesAndTimelineApi(t *testing.T) {
	web := setupTestWeb(t)
	startedAt := time.Unix(1000, 0)
	for i, shortName := range []string{"Q1", "Q2", "Q3"} {
		match := model.Match{
			Type: model.Qualification, TypeOrder: i + 1, ShortName: shortName, LongName: "Qualification " + shortName,
			Red1: 254, Blue2: 1114,
		}
		if shortName != "Q3" {
			match.StartedAt = startedAt.Add(time.Duration(i) * time.Minute)
		}
		assert.Nil(t, web.arena.Database.CreateMatch(&match))
	}
	assert.Nil(t, web.arena.Database.CreateFtaEvent(&model.FtaEvent{
		MatchId: 1, TeamId: 254, Station: "R1", MatchTimeSec: 42, Severity: model.FtaSeverityBad,
	}))
	assert.Nil(t, web.arena.Database.CreateFtaEvent(&model.FtaEvent{MatchId: 1, Severity: model.FtaSeverityGood}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{
		MatchId: 1, TeamId: 254, Station: "R1", MinBatteryVoltage: 7.9,
	}))

	recorder := web.getHttpResponse("/api/fta/matches")
	assert.Equal(t, 200, recorder.Code)
	var matches []ftaMatchListItem
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &matches))
	if assert.Len(t, matches, 2) {
		// Only started matches, most recent first, with only non-good events counted as faults.
		assert.Equal(t, "Q2", matches[0].ShortName)
		assert.Equal(t, "Q1", matches[1].ShortName)
		assert.Equal(t, 1, matches[1].FaultCount)
		assert.Equal(t, []int{254, 0, 0}, matches[1].RedTeams)
	}

	recorder = web.getHttpResponse("/api/fta/matches/1/timeline")
	assert.Equal(t, 200, recorder.Code)
	var timeline ftaTimeline
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &timeline))
	assert.Equal(t, "Q1", timeline.ShortName)
	assert.Greater(t, timeline.DurationSec, timeline.AutoEndSec)
	assert.Len(t, timeline.Events, 2)
	if assert.Len(t, timeline.Stations, 6) {
		assert.Equal(t, "R1", timeline.Stations[0].Station)
		assert.Equal(t, 254, timeline.Stations[0].TeamId)
		assert.Equal(t, "/match_logs/1/R1/log", timeline.Stations[0].LogUrl)
		if assert.NotNil(t, timeline.Stations[0].Stats) {
			assert.Equal(t, 7.9, timeline.Stations[0].Stats.MinBatteryVoltage)
		}
		assert.Equal(t, 0, timeline.Stations[1].TeamId)
		assert.Equal(t, "", timeline.Stations[1].LogUrl)
		assert.Equal(t, 1114, timeline.Stations[4].TeamId)
	}

	recorder = web.getHttpResponse("/api/fta/matches/999/timeline")
	assert.Equal(t, 404, recorder.Code)
}

func TestTimelineSamples(t *testing.T) {
	rows := []MatchLogRow{
		{MatchTimeSec: 0.5},
		{MatchTimeSec: 1, DsLinked: true},
		{MatchTimeSec: 1.5, DsLinked: true, RadioLinked: true},
		{MatchTimeSec: 2, DsLinked: true, RadioLinked: true, RioLinked: true},
		{MatchTimeSec: 2.5, DsLinked: true, RadioLinked: true, RioLinked: true, RobotLinked: true, Enabled: true,
			BatteryVoltage: 12.1, DsRobotTripTimeMs: 4},
	}
	samples := timelineSamples(rows)
	levels := []int{}
	for _, sample := range samples {
		levels = append(levels, sample.Level)
	}
	assert.Equal(t, []int{0, 1, 2, 3, 4}, levels)
	assert.Equal(t, ftaTimelineSample{T: 2.5, Level: 4, Enabled: true, Battery: 12.1, TripMs: 4}, samples[4])
}

func TestFtaWebsocket(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))
	assert.Nil(t, web.arena.SubstituteTeams(0, 0, 0, 254, 0, 0))

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/fta/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	messages := readWebsocketTypes(t, ws, 10, "matchTiming", "arenaStatus", "eventStatus", "matchTime", "matchLoad")
	arenaStatus := messages["arenaStatus"].(map[string]any)
	assert.Contains(t, arenaStatus, "StartMatchBlockers")
	ftaStations := arenaStatus["FtaStations"].(map[string]any)
	b1 := ftaStations["B1"].(map[string]any)
	assert.Equal(t, false, b1["Ready"])

	// Standing notes are saved to the team and to the copy loaded into the match.
	ws.Write("updateTeamNotes", map[string]any{"teamId": 254, "notes": "Spare radio in pit"})
	readWebsocketTypeEventually(t, ws, "ftaNote", 5)
	team, _ := web.arena.Database.GetTeamById(254)
	assert.Equal(t, "Spare radio in pit", team.FtaNotes)
	assert.Equal(t, "Spare radio in pit", web.arena.AllianceStations["B1"].Team.FtaNotes)

	// Tagged notes are attributed to the current match since the team is in it.
	ws.Write("addNote", map[string]any{"teamId": 254, "tag": "radio", "text": "  Rebooted radio  "})
	assert.Equal(t, float64(254), readWebsocketTypeEventually(t, ws, "ftaNote", 5))
	notes, _ := web.arena.Database.GetFtaNotesByTeamId(254)
	if assert.Len(t, notes, 1) {
		assert.Equal(t, "Rebooted radio", notes[0].Text)
		assert.Equal(t, web.arena.CurrentMatch.ShortName, notes[0].MatchShortName)
	}

	ws.Write("deleteNote", map[string]any{"id": notes[0].Id})
	readWebsocketTypeEventually(t, ws, "ftaNote", 5)
	notes, _ = web.arena.Database.GetFtaNotesByTeamId(254)
	assert.Empty(t, notes)

	// Stations can be bypassed before the match but not during it.
	ws.Write("toggleBypass", map[string]any{"station": "B1"})
	readWebsocketTypeEventually(t, ws, "arenaStatus", 5)
	assert.True(t, web.arena.AllianceStations["B1"].Bypass)
	web.arena.MatchState = field.AutoPeriod
	ws.Write("toggleBypass", map[string]any{"station": "B1"})
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "only be bypassed before the match")
	assert.True(t, web.arena.AllianceStations["B1"].Bypass)
	web.arena.MatchState = field.PreMatch
	ws.Write("toggleBypass", map[string]any{"station": "Z9"})
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "Invalid alliance station")

	// The FTA ready switch toggles back and forth.
	ws.Write("toggleFtaReady", nil)
	readWebsocketTypeEventually(t, ws, "arenaStatus", 5)
	assert.True(t, web.arena.Plc.IsFtaReady())
	ws.Write("toggleFtaReady", nil)
	readWebsocketTypeEventually(t, ws, "arenaStatus", 5)
	assert.False(t, web.arena.Plc.IsFtaReady())

	// Check error scenarios.
	ws.Write("addNote", map[string]any{"teamId": 254, "tag": "vibes", "text": "x"})
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "invalid note tag")
	ws.Write("updateTeamNotes", map[string]any{"teamId": 9999, "notes": "x"})
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "team 9999 is not present")
	ws.Write("deleteNote", map[string]any{"id": 12345})
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "note 12345 does not exist")
	ws.Write("bogus", nil)
	assert.Contains(t, readWebsocketErrorEventually(t, ws), "invalid command")
}

// Reads past any status broadcasts to the next error message.
func readWebsocketErrorEventually(t *testing.T, ws *websocket.Websocket) string {
	message, _ := readWebsocketTypeEventually(t, ws, "error", 10).(string)
	return message
}

func TestFtaCsvReport(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, HasConnected: true, FtaNotes: `Uses "spare" radio`}))
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 1114}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{
		TeamId: 254, MinBatteryVoltage: 8.2, FaultCount: 2, RobotDownSec: 3.4,
	}))
	assert.Nil(t, web.arena.Database.CreateFtaTeamMatchStats(&model.FtaTeamMatchStats{
		TeamId: 254, MinBatteryVoltage: 7.1, FaultCount: 1,
	}))
	assert.Nil(t, web.arena.Database.CreateFtaNote(&model.FtaNote{
		TeamId: 254, MatchShortName: "Q1", Tag: "radio", Text: "Rebooted radio",
	}))
	assert.Nil(t, web.arena.Database.CreateFtaNote(&model.FtaNote{TeamId: 254, Tag: "can", Text: "Loose CAN"}))

	recorder := web.getHttpResponse("/reports/csv/fta")
	assert.Equal(t, 200, recorder.Code)
	expectedBody := "Number,HasConnected,FtaNotes,MatchesPlayed,FaultCount,LowestBatteryVoltage,RobotDownSec,NoteLog\n" +
		"254,true,\"Uses \"\"spare\"\" radio\",2,3,7.1,3,\"[Q1 radio] Rebooted radio | [can] Loose CAN\"\n" +
		"1114,false,\"\",0,0,,0,\"\"\n"
	assert.Equal(t, expectedBody, recorder.Body.String())
}
