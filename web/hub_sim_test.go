// Copyright 2026 Team 254. All Rights Reserved.

package web

import (
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/led"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestHubSim(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/hub_sim?alliance=red")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Red Hub - Hub Lighting Sim")
	assert.Contains(t, recorder.Body.String(), `"Alliance":"red"`)
	assert.Contains(t, recorder.Body.String(), `"CanControl":false`)

	recorder = web.getHttpResponse("/hub_sim?alliance=green")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Red &amp; Blue Hubs")
	assert.Contains(t, recorder.Body.String(), `"Alliance":"both"`)

	web.arena.SimControlsEnabled = true
	recorder = web.getHttpResponse("/hub_sim?alliance=blue")
	assert.Contains(t, recorder.Body.String(), "Blue Hub - Hub Lighting Sim")
	assert.Contains(t, recorder.Body.String(), `"CanControl":true`)

	// Controls need an admin login when a password is set.
	web.arena.EventSettings.AdminPassword = "password"
	recorder = web.getHttpResponse("/hub_sim?alliance=blue")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"CanControl":false`)
}

func TestHubSimWebsocket(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/hub_sim/websocket?alliance=red", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	timelines, ok := readWebsocketType(t, ws, "hubSimTimeline").(map[string]any)
	if assert.True(t, ok) {
		assert.Contains(t, timelines, "RedWonAuto")
		assert.Contains(t, timelines, "BlueWonAuto")
		assert.Contains(t, timelines, "Swatches")
	}
	frame, ok := readWebsocketTypeEventually(t, ws, "hubSimFrame", 5).(map[string]any)
	if assert.True(t, ok) {
		assert.Len(t, frame["Red"], 64*6)
		assert.Len(t, frame["Blue"], 64*6)
	}

	// Controls are refused unless the server was started with -hubsim.
	ws.Write("startMatch", nil)
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "-hubsim")
	assert.Equal(t, field.PreMatch, web.arena.MatchState)
}

func TestHubSimWebsocketControls(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.SimControlsEnabled = true

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/hub_sim/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)
	readWebsocketTypes(t, ws, 5, "hubSimTimeline", "hubSimFrame")

	ws.Write("setLedMode", map[string]any{"Alliance": "red", "Mode": led.RainbowMode})
	ws.Write("setLedMode", map[string]any{"Alliance": "blue", "Mode": led.Side2TestMode})
	assertModes := func(redMode, blueMode led.Mode) {
		for i := 0; i < 50; i++ {
			frame := readWebsocketTypeEventually(t, ws, "hubSimFrame", 5).(map[string]any)
			if int(frame["RedMode"].(float64)) == int(redMode) && int(frame["BlueMode"].(float64)) == int(blueMode) {
				return
			}
		}
		assert.Fail(t, "LED modes never reached the expected values", "%d/%d", redMode, blueMode)
	}
	assertModes(led.RainbowMode, led.Side2TestMode)
	ws.Write("setLedMode", map[string]any{"Alliance": "both", "Mode": led.PurpleMode})
	assertModes(led.PurpleMode, led.PurpleMode)

	ws.Write("setLedMode", map[string]any{"Alliance": "green", "Mode": led.PurpleMode})
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "invalid alliance")
	ws.Write("setAutoWinner", "purple")
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "invalid auto winner")
	ws.Write("skipAhead", "lots")
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "failed to parse")

	ws.Write("setAutoWinner", "red")
	ws.Write("startMatch", nil)
	for i := 0; i < 50 && web.arena.MatchState == field.PreMatch; i++ {
		readWebsocketTypeEventually(t, ws, "hubSimFrame", 5)
	}
	assert.Equal(t, field.StartMatch, web.arena.MatchState)
	assert.Equal(t, model.Test, web.arena.CurrentMatch.Type)
	assert.True(t, web.arena.AllianceStations["B2"].Bypass)

	// LED previews aren't allowed during a match.
	ws.Write("setLedMode", map[string]any{"Alliance": "red", "Mode": led.RainbowMode})
	assert.Equal(t, fieldTestingLedModeDisabledMessage, readWebsocketTypeEventually(t, ws, "error", 50))
}

func TestFieldSim(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/field_sim")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Field Simulator")
	assert.Contains(t, recorder.Body.String(), `"SideNames":["Driver Station","Audience","Center","Scoring Table"]`)
	assert.Contains(t, recorder.Body.String(), `"CoilNames":["heartbeat"`)
	assert.Contains(t, recorder.Body.String(), `"CanControl":false`)
}

func TestFieldSimWebsocketControls(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EnableSimulation()

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/field_sim/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)
	messages := readWebsocketTypes(t, ws, 10, "hubSimTimeline", "matchTiming", "fieldSimStatus")
	status, ok := messages["fieldSimStatus"].(map[string]any)
	if assert.True(t, ok) {
		assert.Equal(t, true, status["SimulatedPlc"])
	}

	// Stop buttons and Fuel go to the simulated PLC.
	ws.Write("setStationStop", map[string]any{"Station": "R3", "AStop": false, "Pressed": true})
	ws.Write("setFieldEStop", true)
	ws.Write("addFuel", map[string]any{"Alliance": "blue", "Count": 5})
	for i := 0; i < 100; i++ {
		redEStops, _ := web.arena.Plc.GetTeamEStops()
		_, blueCount := web.arena.Plc.GetHubCounts()
		if redEStops[2] && web.arena.Plc.GetFieldEStop() && blueCount == 5 {
			break
		}
		readWebsocketTypeEventually(t, ws, "hubSimFrame", 5)
	}
	redEStops, _ := web.arena.Plc.GetTeamEStops()
	_, blueCount := web.arena.Plc.GetHubCounts()
	assert.True(t, redEStops[2])
	assert.True(t, web.arena.Plc.GetFieldEStop())
	assert.Equal(t, 5, blueCount)

	ws.Write("addFuel", map[string]any{"Alliance": "green", "Count": 1})
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "invalid alliance")
	ws.Write("setFieldEStop", "yes")
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "failed to parse")
}

func TestFieldSimWebsocketRobotCommands(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EnableSimulation()

	recorder := web.getHttpResponse("/field_sim")
	assert.Contains(t, recorder.Body.String(), `"RobotStates":{"0":"Off"`)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/field_sim/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)
	readWebsocketTypes(t, ws, 10, "hubSimTimeline", "fieldSimStatus")

	ws.Write("setRobotState", map[string]any{"Station": "R9", "State": 4})
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "invalid alliance station")
	ws.Write("useEventTeams", nil)
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "no teams")
	ws.Write("addFuel", map[string]any{"Alliance": "red", "Count": 1000})
	assert.Contains(t, readWebsocketTypeEventually(t, ws, "error", 50), "between 1 and 100")

	// Hidden pages still get occasional updates.
	ws.Write("setVisible", false)
	frames := 0
	for i := 0; i < 20 && frames < 2; i++ {
		messageType, _, err := ws.ReadWithTimeout(3 * time.Second)
		if !assert.Nil(t, err) {
			break
		}
		if messageType == "hubSimFrame" {
			frames++
		}
	}
	assert.Equal(t, 2, frames)
}
