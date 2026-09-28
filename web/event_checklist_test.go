// Copyright 2026 Team 254. All Rights Reserved.

package web

import (
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"net/http/httptest"
	"testing"
)

func setupTestHubWeb(t *testing.T) *Web {
	web := setupTestWeb(t)
	web.arena.EventSettings.MultiFieldRole = model.HubRole
	eventHub := hub.New(web.arena)
	t.Cleanup(eventHub.Stop)
	web.SetHub(eventHub)
	return web
}

func findStep(steps []checklistStep, title string) *checklistStep {
	for i := range steps {
		if steps[i].Title == title {
			return &steps[i]
		}
	}
	return nil
}

func TestEventChecklistGuidesANewHub(t *testing.T) {
	web := setupTestHubWeb(t)

	steps, err := web.eventChecklist()
	assert.Nil(t, err)
	secret := findStep(steps, "Set a shared secret")
	if assert.NotNil(t, secret) {
		assert.Equal(t, stepProblem, secret.State)
		assert.True(t, secret.Current)
		assert.Equal(t, "secret", secret.Action)
	}
	assert.NotNil(t, findStep(steps, "Connect Field 1"))
	assert.NotNil(t, findStep(steps, "Add teams"))

	// Setting the secret from the page moves the checklist on to connecting the fields.
	recorder := web.postHttpResponse("/event_control/secret", "hubSharedSecret=ab")
	assert.Contains(t, recorder.Body.String(), "at least 4 characters")
	assert.Equal(t, "", web.arena.EventSettings.HubSharedSecret)
	recorder = web.postHttpResponse("/event_control/secret", "hubSharedSecret=+dev-secret+")
	assert.Equal(t, 303, recorder.Code)
	assert.Equal(t, "/event_control?saved=secret", recorder.Header().Get("Location"))
	assert.Equal(t, "dev-secret", web.arena.EventSettings.HubSharedSecret)
	steps, _ = web.eventChecklist()
	assert.Equal(t, stepDone, findStep(steps, "Shared secret set").State)
	assert.True(t, findStep(steps, "Connect Field 1").Current)

	// A node asking to join gets an Approve button, and the operator can see which laptop is asking.
	assert.Nil(t, web.arena.Database.CreateNodeRegistration(
		&model.NodeRegistration{Id: 2, FieldName: "Field 2", RemoteAddr: "10.0.0.7:51234"},
	))
	steps, _ = web.eventChecklist()
	connect := findStep(steps, "Connect Field 2")
	if assert.NotNil(t, connect) {
		assert.Equal(t, "approve", connect.Action)
		assert.Contains(t, connect.Detail, "asking to join from 10.0.0.7")
	}

	// The fragment and the full page both render.
	recorder = web.getHttpResponse("/event_control/checklist")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Approve Field 2")
	recorder = web.getHttpResponse("/event_control?saved=secret")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Shared secret saved")
	assert.Contains(t, recorder.Body.String(), "How to connect a field")
	assert.Contains(t, recorder.Body.String(), "http://localhost:8080")
}

func TestEventChecklistTracksProgress(t *testing.T) {
	web := setupTestHubWeb(t)
	web.arena.EventSettings.HubSharedSecret = "dev-secret"
	for i := 1; i <= 4; i++ {
		match := model.Match{
			Type: model.Qualification, TypeOrder: i, ShortName: "Q", FieldId: 1 + (i+1)%2, Red1: i, Blue1: i + 10,
		}
		if i <= 3 {
			match.Status = game.RedWonMatch
		}
		assert.Nil(t, web.arena.Database.CreateMatch(&match))
	}
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))

	steps, err := web.eventChecklist()
	assert.Nil(t, err)
	assert.Equal(t, stepDone, findStep(steps, "1 teams").State)
	assert.Equal(t, stepDone, findStep(steps, "Qualification schedule: 4 matches").State)
	quals := findStep(steps, "Qualifications: 3 of 4 played")
	if assert.NotNil(t, quals) {
		assert.Equal(t, stepTodo, quals.State)
		assert.Equal(t, "Field 1: 2/2, Field 2: 1/2", quals.Detail)
	}
}

func TestEventChecklistFlagsConferencesTooSmallForTheirAlliances(t *testing.T) {
	web := setupTestHubWeb(t)
	web.arena.EventSettings.HubSharedSecret = "dev-secret"
	web.arena.EventSettings.MultiConferenceEnabled = true
	conferences, err := web.arena.Database.EnsureConferences()
	assert.Nil(t, err)
	// North has 8 double-elimination alliances by default, which needs 24 teams.
	for i := 1; i <= 15; i++ {
		assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: i, ConferenceId: conferences[0].Id}))
	}

	steps, err := web.eventChecklist()
	assert.Nil(t, err)
	teams := findStep(steps, "15 teams")
	if assert.NotNil(t, teams) {
		assert.Equal(t, stepProblem, teams.State)
		assert.Contains(t, teams.Detail, "North has 15 teams but 8 alliances of 3 need 24")
		assert.Contains(t, teams.Detail, "lower its number of alliances to 5 or fewer")
		// South has no teams, and fewer than 4 alliances isn't allowed, so it needs teams.
		assert.Contains(t, teams.Detail, "South has 0 teams but 8 alliances of 3 need 24; add teams to it")
	}
}

func TestMultiFieldStatusText(t *testing.T) {
	for _, test := range []struct {
		status   field.MultiFieldStatus
		expected string
	}{
		{field.MultiFieldStatus{IsHub: true}, "Hub"},
		{field.MultiFieldStatus{}, ""},
		{field.MultiFieldStatus{IsNode: true, FieldName: "Field 1", Connected: true, Approved: true},
			"Field 1 · Hub connected · 0 queued"},
		{field.MultiFieldStatus{IsNode: true, FieldName: "Field 1", Connected: true},
			"Field 1 · Awaiting hub approval · 0 queued"},
		{field.MultiFieldStatus{IsNode: true, FieldName: "Field 2", Refused: true, OutboxSize: 1},
			"Field 2 · Hub refused this field · 1 queued"},
		{field.MultiFieldStatus{IsNode: true, FieldName: "Field 2", OutboxSize: 3, RejectedCount: 1},
			"Field 2 · HUB OFFLINE · 3 queued · 1 rejected"},
	} {
		assert.Equal(t, test.expected, multiFieldStatusText(test.status))
	}
}

func TestAdapterRank(t *testing.T) {
	for _, test := range []struct {
		name string
		rank int
	}{
		{"Ethernet", 1},
		{"Wi-Fi", 1},
		{"en0", 1},
		{"vEthernet (Ethernet)", 2},
		{"Tailscale", 3},
		{"vEthernet (Default Switch)", 4},
		{"vEthernet (WSL (Hyper-V firewall))", 4},
		{"VirtualBox Host-Only Network", 4},
		{"docker0", 4},
	} {
		assert.Equal(t, test.rank, adapterRank(test.name), test.name)
	}

	// The address the page was opened with comes first, and localhost last.
	request := httptest.NewRequest("GET", "http://192.168.50.10:8080/event_control", nil)
	addresses := hubAddressesForNodes(request)
	assert.Equal(t, "http://192.168.50.10:8080", addresses[0].Address)
	assert.Equal(t, "http://localhost:8080", addresses[len(addresses)-1].Address)
}

func TestRoleChangeTakesEffectWithoutRestart(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.SimulateHardware = true
	previous := *web.arena.EventSettings

	// Standalone to node starts the node client.
	web.arena.EventSettings.MultiFieldRole = model.NodeRole
	web.arena.EventSettings.FieldId = 1
	web.arena.EventSettings.HubAddress = "http://127.0.0.1:1"
	web.arena.EventSettings.HubSharedSecret = "dev-secret"
	assert.Equal(t, "", web.applyMultiFieldRole(&previous))
	assert.NotNil(t, web.node)
	assert.NotNil(t, web.arena.NodeLink)

	// Node to hub stops the node client and starts the hub service.
	previous = *web.arena.EventSettings
	web.arena.EventSettings.MultiFieldRole = model.HubRole
	assert.Equal(t, "", web.applyMultiFieldRole(&previous))
	assert.Nil(t, web.node)
	assert.Nil(t, web.arena.NodeLink)
	assert.NotNil(t, web.hub)
	recorder := web.getHttpResponse("/event_control")
	assert.Contains(t, recorder.Body.String(), "Event Checklist")

	// Hub to standalone stops the hub service; with real hardware, a restart is suggested.
	web.arena.SimulateHardware = false
	previous = *web.arena.EventSettings
	web.arena.EventSettings.MultiFieldRole = model.StandaloneRole
	assert.Contains(t, web.applyMultiFieldRole(&previous), "restart")
	assert.Nil(t, web.hub)
	recorder = web.getHttpResponse("/api/hub/versions")
	assert.Equal(t, 404, recorder.Code)
}

func TestNodeStatusAndHubMatchPlayNotice(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.EventSettings.MultiFieldRole = model.NodeRole
	web.arena.EventSettings.FieldId = 1
	assert.Nil(t, web.arena.Database.CreateSyncOutboxEntry(&model.SyncOutboxEntry{
		FieldId: 1, MatchId: 5, PlayNumber: 1, Match: model.Match{ShortName: "Q5"}, Rejected: true,
		LastError: "match Q5 is assigned to field 2, not field 1",
	}))
	assert.Nil(t, web.arena.Database.CreateSyncOutboxEntry(&model.SyncOutboxEntry{
		FieldId: 1, MatchId: 6, PlayNumber: 1, Match: model.Match{ShortName: "Q6"},
	}))
	recorder := web.getHttpResponse("/setup/node/status")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"Queued":["Q6"]`)
	assert.Contains(t, body, "assigned to field 2")

	web.arena.EventSettings.MultiFieldRole = model.HubRole
	recorder = web.getHttpResponse("/match_play")
	assert.Contains(t, recorder.Body.String(), "This is the hub, which has no field of its own.")
}

func TestSimulateWholeEventRequiresSimulationMode(t *testing.T) {
	web := setupTestHubWeb(t)
	recorder := web.postHttpResponse("/event_control/simulate_all", "intervalSec=1")
	assert.Contains(t, recorder.Body.String(), "only available when running with -simulate")
	assert.False(t, web.eventRunnerActive())
	assert.NotContains(t, web.getHttpResponse("/event_control").Body.String(), "Simulate the whole event")
}

func TestSimulateWholeEventSeedsAnEmptyEvent(t *testing.T) {
	web := setupTestHubWeb(t)
	web.arena.SimulateHardware = true
	// Internal calls must work even when an admin password is set.
	web.arena.EventSettings.AdminPassword = "secret"

	finished, status, err := web.simulateEventStep(1)
	assert.Nil(t, err)
	assert.False(t, finished)
	assert.Contains(t, status, "36 test teams")
	teams, _ := web.arena.Database.GetAllTeams()
	assert.Equal(t, 36, len(teams))

	// With no fields connected, it waits for them.
	_, status, err = web.simulateEventStep(1)
	assert.Nil(t, err)
	assert.Contains(t, status, "Waiting for a field")

	// A browser request can't use the internal-call bypass.
	recorder := httptest.NewRecorder()
	web.devSeedEventHandler(recorder, httptest.NewRequest("POST", "/api/dev/seed_event", nil))
	assert.Equal(t, 307, recorder.Code)
}

func TestSimulateWholeEventNeedsAScheduleForExistingTeams(t *testing.T) {
	web := setupTestHubWeb(t)
	web.arena.SimulateHardware = true
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))
	_, _, err := web.simulateEventStep(1)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "generate one on Match Scheduling")
	}
}
