// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package web

import (
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
	"time"
)

func TestRefereePanel(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/panels/referee")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Referee Panel - Untitled Event - Cheesy Arena")
	assert.Contains(t, recorder.Body.String(), "Auto Tower")
	assert.Contains(t, recorder.Body.String(), "Endgame Tower")
	assert.Contains(t, recorder.Body.String(), "id=\"ftaReadyButton\"")
	assert.Contains(t, recorder.Body.String(), "id=\"postMatchChecklist\"")
	assert.Contains(t, recorder.Body.String(), "id=\"undoToast\"")
	assert.NotContains(t, recorder.Body.String(), "Leave")
	assert.NotContains(t, recorder.Body.String(), "Coral")
	assert.NotContains(t, recorder.Body.String(), "Algae")
}

// Reads the status messages that are sent to a referee panel right after it connects.
func readRefereePanelInitialMessages(t *testing.T, ws *websocket.Websocket) {
	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")
	readWebsocketType(t, ws, "realtimeScore")
	readWebsocketType(t, ws, "scoringStatus")
	readWebsocketType(t, ws, "arenaStatus")
	readWebsocketType(t, ws, "allianceStationDisplayMode")
}

// Connects a referee panel websocket with the given query string and consumes the initial status messages.
func connectRefereePanel(t *testing.T, wsUrl string, query string) (*gorillawebsocket.Conn, *websocket.Websocket) {
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/panels/referee/websocket"+query, nil)
	assert.Nil(t, err)
	ws := websocket.NewTestWebsocket(conn)
	readRefereePanelInitialMessages(t, ws)
	return conn, ws
}

func TestRefereePanelFtaReadyToggle(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, ws := connectRefereePanel(t, wsUrl, "")
	defer conn.Close()

	assert.False(t, web.arena.Plc.IsFtaReady())
	ws.Write("toggleFtaReady", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.True(t, web.arena.Plc.IsFtaReady())

	ws.Write("toggleFtaReady", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.False(t, web.arena.Plc.IsFtaReady())
}

func TestRefereePanelWebsocket(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, ws := connectRefereePanel(t, wsUrl, "")
	defer conn.Close()

	numHead, numRef := web.arena.RefereePanels.GetNumPanels()
	assert.Equal(t, 1, numHead)
	assert.Equal(t, 0, numRef)

	// Test foul addition.
	addFoulData := struct {
		Alliance string
		IsMajor  bool
	}{"red", true}
	ws.Write("addFoul", addFoulData)
	addFoulData.IsMajor = false
	ws.Write("addFoul", addFoulData)
	addFoulData.Alliance = "blue"
	ws.Write("addFoul", addFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	readWebsocketType(t, ws, "realtimeScore")
	readWebsocketType(t, ws, "realtimeScore")
	if assert.Equal(t, 2, len(web.arena.RedRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, true, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].IsMajor)
		assert.Equal(t, 0, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].TeamId)
		assert.Equal(t, 0, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].RuleId)
		assert.Equal(t, "Head Ref", web.arena.RedRealtimeScore.CurrentScore.Fouls[0].Source)
		assert.Equal(t, false, web.arena.RedRealtimeScore.CurrentScore.Fouls[1].IsMajor)
		assert.Equal(t, 0, web.arena.RedRealtimeScore.CurrentScore.Fouls[1].TeamId)
		assert.Equal(t, 0, web.arena.RedRealtimeScore.CurrentScore.Fouls[1].RuleId)
	}
	if assert.Equal(t, 1, len(web.arena.BlueRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, false, web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].IsMajor)
		assert.Equal(t, 0, web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].TeamId)
		assert.Equal(t, 0, web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].RuleId)
	}
	assert.False(t, web.arena.RedRealtimeScore.FoulsCommitted)
	assert.False(t, web.arena.BlueRealtimeScore.FoulsCommitted)

	// Test foul mutation. Fouls are referenced by ID; red has fouls 1 and 2 and blue has foul 3.
	modifyFoulData := struct {
		Alliance string
		FoulId   int
		TeamId   int
		RuleId   int
	}{}
	modifyFoulData.Alliance = "red"
	modifyFoulData.FoulId = 2
	ws.Write("toggleFoulType", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	assert.Equal(t, true, web.arena.RedRealtimeScore.CurrentScore.Fouls[1].IsMajor)
	modifyFoulData.FoulId = 1
	modifyFoulData.TeamId = 256
	ws.Write("updateFoulTeam", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	assert.Equal(t, 256, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].TeamId)
	modifyFoulData.Alliance = "blue"
	modifyFoulData.FoulId = 3
	modifyFoulData.RuleId = 3
	ws.Write("updateFoulRule", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	assert.Equal(t, 3, web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].RuleId)

	// Test foul deletion.
	ws.Write("deleteFoul", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	assert.Equal(t, 0, len(web.arena.BlueRealtimeScore.CurrentScore.Fouls))
	modifyFoulData.Alliance = "red"
	modifyFoulData.FoulId = 3 // Foul belongs to the other alliance.
	ws.Write("deleteFoul", modifyFoulData)
	modifyFoulData.FoulId = 99 // Nonexistent foul.
	ws.Write("deleteFoul", modifyFoulData)
	modifyFoulData.FoulId = 1
	ws.Write("deleteFoul", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	if assert.Equal(t, 1, len(web.arena.RedRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, 2, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].FoulId)
	}

	// Test restoring deleted fouls into their original positions.
	ws.Write("restoreFoul", map[string]int{"FoulId": 1})
	readWebsocketType(t, ws, "realtimeScore")
	if assert.Equal(t, 2, len(web.arena.RedRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, 1, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].FoulId)
		assert.Equal(t, 256, web.arena.RedRealtimeScore.CurrentScore.Fouls[0].TeamId)
		assert.Equal(t, 2, web.arena.RedRealtimeScore.CurrentScore.Fouls[1].FoulId)
	}
	ws.Write("restoreFoul", map[string]int{"FoulId": 1}) // Already restored.
	ws.Write("restoreFoul", map[string]int{"FoulId": 3})
	readWebsocketType(t, ws, "realtimeScore")
	if assert.Equal(t, 1, len(web.arena.BlueRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, 3, web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].RuleId)
	}
	assert.Equal(t, 2, len(web.arena.RedRealtimeScore.CurrentScore.Fouls))
	modifyFoulData.Alliance = "blue"
	modifyFoulData.FoulId = 3
	ws.Write("deleteFoul", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	modifyFoulData.Alliance = "red"
	modifyFoulData.FoulId = 1
	ws.Write("deleteFoul", modifyFoulData)
	readWebsocketType(t, ws, "realtimeScore")
	assert.Equal(t, 1, len(web.arena.RedRealtimeScore.CurrentScore.Fouls))

	// Test card setting.
	cardData := struct {
		Alliance string
		TeamId   int
		Card     string
	}{"red", 256, "yellow"}
	ws.Write("card", cardData)
	readWebsocketType(t, ws, "realtimeScore")
	cardData.Alliance = "blue"
	cardData.TeamId = 1680
	cardData.Card = "red"
	ws.Write("card", cardData)
	readWebsocketType(t, ws, "realtimeScore")
	time.Sleep(time.Millisecond * 10) // Allow some time for the command to be processed.
	if assert.Equal(t, 1, len(web.arena.RedRealtimeScore.Cards)) {
		assert.Equal(t, "yellow", web.arena.RedRealtimeScore.Cards["256"])
	}
	if assert.Equal(t, 1, len(web.arena.BlueRealtimeScore.Cards)) {
		assert.Equal(t, "red", web.arena.BlueRealtimeScore.Cards["1680"])
	}

	// Test card setting in a playoff match.
	web.arena.CurrentMatch.Type = model.Playoff
	web.arena.CurrentMatch.Red1 = 256
	web.arena.CurrentMatch.Red2 = 257
	web.arena.CurrentMatch.Red3 = 258
	web.arena.CurrentMatch.Blue1 = 1679
	web.arena.CurrentMatch.Blue2 = 1680
	web.arena.CurrentMatch.Blue3 = 1681
	cardData.Card = "yellow"
	ws.Write("card", cardData)
	readWebsocketType(t, ws, "realtimeScore")
	cardData.Alliance = "red"
	cardData.TeamId = 258
	cardData.Card = "red"
	ws.Write("card", cardData)
	readWebsocketType(t, ws, "realtimeScore")
	time.Sleep(time.Millisecond * 10) // Allow some time for the command to be processed.
	if assert.Equal(t, 3, len(web.arena.RedRealtimeScore.Cards)) {
		assert.Equal(t, "red", web.arena.RedRealtimeScore.Cards["256"])
		assert.Equal(t, "red", web.arena.RedRealtimeScore.Cards["257"])
		assert.Equal(t, "red", web.arena.RedRealtimeScore.Cards["258"])
	}
	if assert.Equal(t, 3, len(web.arena.BlueRealtimeScore.Cards)) {
		assert.Equal(t, "yellow", web.arena.BlueRealtimeScore.Cards["1679"])
		assert.Equal(t, "yellow", web.arena.BlueRealtimeScore.Cards["1680"])
		assert.Equal(t, "yellow", web.arena.BlueRealtimeScore.Cards["1681"])
	}

	// Test field reset and match committing.
	web.arena.CurrentMatch.Type = model.Test
	web.arena.MatchState = field.PostMatch
	ws.Write("signalReset", nil)
	assert.Equal(t, "fieldReset", readWebsocketType(t, ws, "allianceStationDisplayMode"))
	assert.Equal(t, "fieldReset", web.arena.AllianceStationDisplayMode)
	assert.False(t, web.arena.RedRealtimeScore.FoulsCommitted)
	assert.False(t, web.arena.BlueRealtimeScore.FoulsCommitted)
	web.arena.AllianceStationDisplayMode = "logo"
	ws.Write("commitAndPost", nil)
	readWebsocketType(t, ws, "scoringStatus")
	messages := readWebsocketTypes(t, ws, 10, "realtimeScore", "scoringStatus", "matchLoad")
	assert.NotNil(t, messages["realtimeScore"])
	assert.NotNil(t, messages["scoringStatus"])
	assert.NotNil(t, messages["matchLoad"])
	assert.Equal(t, "score", web.arena.AudienceDisplayMode)
}

func TestRefereePanelRestoreFoulAfterMatchLoad(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, ws := connectRefereePanel(t, wsUrl, "")
	defer conn.Close()

	ws.Write("addFoul", map[string]any{"Alliance": "red", "IsMajor": false})
	readWebsocketType(t, ws, "realtimeScore")
	ws.Write("deleteFoul", map[string]any{"Alliance": "red", "FoulId": 1})
	readWebsocketType(t, ws, "realtimeScore")

	// A foul deleted in one match must not be restorable into the next one.
	assert.Nil(t, web.arena.LoadTestMatch())
	readWebsocketTypeEventually(t, ws, "matchLoad", 10)
	ws.Write("restoreFoul", map[string]int{"FoulId": 1})
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, 0, len(web.arena.RedRealtimeScore.CurrentScore.Fouls))
}

func TestRefereePanelNonHeadReferee(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, ws := connectRefereePanel(t, wsUrl, "?hr=false")
	defer conn.Close()

	numHead, numRef := web.arena.RefereePanels.GetNumPanels()
	assert.Equal(t, 0, numHead)
	assert.Equal(t, 1, numRef)

	// Regular referees can add fouls, which are tagged with their source.
	ws.Write("addFoul", map[string]any{"Alliance": "blue", "IsMajor": true})
	readWebsocketType(t, ws, "realtimeScore")
	if assert.Equal(t, 1, len(web.arena.BlueRealtimeScore.CurrentScore.Fouls)) {
		assert.Equal(t, "Ref", web.arena.BlueRealtimeScore.CurrentScore.Fouls[0].Source)
	}

	// Head-referee-only commands are rejected.
	web.arena.MatchState = field.PostMatch
	for _, messageType := range []string{
		"card", "toggleBypass", "signalVolunteers", "signalReset", "toggleFtaReady", "commitAndPost",
	} {
		ws.Write(messageType, nil)
		assert.Contains(t, readWebsocketError(t, ws), "Only the head referee")
	}
	assert.False(t, web.arena.Plc.IsFtaReady())
	assert.False(t, web.arena.RedRealtimeScore.FoulsCommitted)
	assert.Equal(t, "match", web.arena.AllianceStationDisplayMode)
}

func TestRefereePanelFoulList(t *testing.T) {
	web := setupTestWeb(t)

	web.arena.CurrentMatch.Red1 = 254
	web.arena.CurrentMatch.Blue1 = 1678
	web.arena.RedRealtimeScore.CurrentScore.Fouls = []game.Foul{
		{FoulId: 1, IsMajor: false, Source: "Head Ref"},
		{FoulId: 3, IsMajor: true, TeamId: 254, Source: "Red Scorer"},
	}
	web.arena.BlueRealtimeScore.CurrentScore.Fouls = []game.Foul{{FoulId: 2, IsMajor: false, Source: "Ref"}}

	fouls := web.combinedFoulList()
	if assert.Equal(t, 3, len(fouls)) {
		// Newest fouls come first, numbered within their own alliance.
		assert.Equal(t, 3, fouls[0].Foul.FoulId)
		assert.Equal(t, "red", fouls[0].Alliance)
		assert.Equal(t, 2, fouls[0].Number)
		assert.Equal(t, [3]int{254, 0, 0}, fouls[0].TeamIds)
		assert.Equal(t, 2, fouls[1].Foul.FoulId)
		assert.Equal(t, "blue", fouls[1].Alliance)
		assert.Equal(t, 1, fouls[1].Number)
		assert.Equal(t, [3]int{1678, 0, 0}, fouls[1].TeamIds)
		assert.Equal(t, 1, fouls[2].Foul.FoulId)
	}

	recorder := web.getHttpResponse("/panels/referee/foul_list")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "data-foul-id=\"3\"")
	assert.Contains(t, body, "Red Scorer")
	assert.Less(t, strings.Index(body, "data-foul-id=\"3\""), strings.Index(body, "data-foul-id=\"1\""))
}

func TestRefereePanelFoulListRequiresAdmin(t *testing.T) {
	web := setupTestWeb(t)

	web.arena.EventSettings.AdminPassword = "password"
	recorder := web.getHttpResponse("/panels/referee/foul_list")
	assert.Equal(t, 307, recorder.Code)
}
