// Copyright 2026 Team 254. All Rights Reserved.

package node

import (
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const testSecret = "shared secret"

// Starts a hub with its node API served over HTTP, returning the hub's arena and the server.
func setupTestHubServer(t *testing.T) (*field.Arena, *hub.Hub, *httptest.Server) {
	arena := field.SetupTestArena(t)
	model.BaseDir = t.TempDir()
	arena.EventSettings.Name = "Hub Event"
	arena.EventSettings.MultiFieldRole = model.HubRole
	arena.EventSettings.HubSharedSecret = testSecret
	arena.EventSettings.AdminPassword = "hub password"
	arena.EventSettings.ApAddress = "1.1.1.1"
	arena.EventSettings.MultiConferenceEnabled = true
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	eventHub := hub.New(arena)
	eventHub.AutoApproveNodes = true

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/hub/node/websocket", eventHub.NodeWebsocketHandler)
	mux.HandleFunc("GET /api/hub/snapshot/{collection}", eventHub.SnapshotHandler)
	mux.HandleFunc("GET /api/hub/versions", eventHub.VersionsHandler)
	mux.HandleFunc("POST /api/hub/results", eventHub.ResultsHandler)
	mux.HandleFunc("POST /api/hub/claim_next", eventHub.ClaimNextHandler)
	mux.HandleFunc("POST /api/hub/release", eventHub.ReleaseHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return arena, eventHub, server
}

func setupTestNodeArena(t *testing.T, fieldId int, hubAddress string) *field.Arena {
	arena := field.SetupTestArena(t)
	arena.EventSettings.MultiFieldRole = model.NodeRole
	arena.EventSettings.FieldId = fieldId
	arena.EventSettings.FieldName = "Field " + strconv.Itoa(fieldId)
	arena.EventSettings.HubAddress = hubAddress
	arena.EventSettings.HubSharedSecret = testSecret
	arena.EventSettings.AdminPassword = "node password"
	arena.EventSettings.ApAddress = "10.0.100.2"
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	return arena
}

func createHubMatches(t *testing.T, database *model.Database, count int) []model.Match {
	var matches []model.Match
	for i := 1; i <= count; i++ {
		match := model.Match{
			Type: model.Qualification, TypeOrder: i, ShortName: "Q" + strconv.Itoa(i),
			Red1: 10*i + 1, Red2: 10*i + 2, Red3: 10*i + 3, Blue1: 10*i + 4, Blue2: 10*i + 5, Blue3: 10*i + 6,
			FieldId: 1, Status: game.MatchScheduled,
		}
		assert.Nil(t, database.CreateMatch(&match))
		for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
			assert.Nil(t, database.CreateTeam(&model.Team{Id: teamId, WpaKey: "hubkey" + strconv.Itoa(teamId)}))
		}
		matches = append(matches, match)
	}
	return matches
}

func waitFor(t *testing.T, description string, condition func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %s", description)
	return false
}

func TestNodeMirrorsHubAndKeepsLocalSettings(t *testing.T) {
	hubArena, _, server := setupTestHubServer(t)
	hubMatches := createHubMatches(t, hubArena.Database, 3)
	hubArena.NotifyDataChanged()

	nodeArena := setupTestNodeArena(t, 1, server.URL)
	// A team with local-only state that must survive the mirror.
	assert.Nil(t, nodeArena.Database.CreateTeam(&model.Team{Id: 11, HasConnected: true, FtaNotes: "radio loose"}))
	node := Start(nodeArena)
	defer node.Stop()

	waitFor(t, "matches to be mirrored", func() bool {
		matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
		return len(matches) == 3
	})
	waitFor(t, "shared settings to be mirrored", func() bool {
		return nodeArena.EventSettings.Name == "Hub Event"
	})

	matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
	assert.Equal(t, hubMatches[0].Id, matches[0].Id)
	settings := nodeArena.EventSettings
	assert.True(t, settings.MultiConferenceEnabled)
	assert.Equal(t, model.NodeRole, settings.MultiFieldRole)
	assert.Equal(t, 1, settings.FieldId)
	assert.Equal(t, "node password", settings.AdminPassword)
	assert.Equal(t, "10.0.100.2", settings.ApAddress)
	assert.Equal(t, server.URL, settings.HubAddress)

	team, _ := nodeArena.Database.GetTeamById(11)
	assert.True(t, team.HasConnected)
	assert.Equal(t, "radio loose", team.FtaNotes)
	assert.Equal(t, "hubkey11", team.WpaKey)

	// Later changes on the hub reach the node.
	hubArena.EventSettings.Name = "Renamed Event"
	assert.Nil(t, hubArena.Database.UpdateEventSettings(hubArena.EventSettings))
	hubArena.NotifyDataChanged()
	waitFor(t, "renamed event", func() bool { return nodeArena.EventSettings.Name == "Renamed Event" })
	assert.True(t, node.Status().Connected)
}

func TestNodeDeliversResultsInOrderAfterOutage(t *testing.T) {
	hubArena, _, server := setupTestHubServer(t)
	createHubMatches(t, hubArena.Database, 3)
	hubArena.NotifyDataChanged()

	nodeArena := setupTestNodeArena(t, 1, server.URL)
	node := Start(nodeArena)
	defer node.Stop()
	waitFor(t, "matches to be mirrored", func() bool {
		matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
		return len(matches) == 3
	})

	// Take the hub "offline" by pointing the node at an address with nothing listening.
	nodeArena.EventSettings.HubAddress = "http://127.0.0.1:1"
	matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
	for _, match := range matches {
		matchResult := model.BuildTestMatchResult(match.Id, 1)
		field.PrepareMatchResult(&match, matchResult)
		assert.Nil(t, nodeArena.SaveMatchResult(&match, matchResult))
		receipt, err := node.SubmitResult(&match, matchResult, false)
		assert.Nil(t, err)
		assert.True(t, receipt.Queued)
	}
	assert.Equal(t, 3, node.Status().OutboxSize)

	// Bring the hub back; the queued results are delivered in the order they were played.
	nodeArena.EventSettings.HubAddress = server.URL
	node.kickOutbox()
	waitFor(t, "outbox to drain", func() bool { return node.Status().OutboxSize == 0 })
	receipts, err := hubArena.Database.GetAllHubResultReceipts()
	assert.Nil(t, err)
	if assert.Equal(t, 3, len(receipts)) {
		for i := 1; i < len(receipts); i++ {
			assert.False(t, receipts[i].ReceivedAt.Before(receipts[i-1].ReceivedAt))
		}
		receiptOrder := map[int]int{}
		for _, receipt := range receipts {
			receiptOrder[receipt.Id] = receipt.MatchId
		}
		assert.Equal(t, matches[0].Id, receiptOrder[1])
		assert.Equal(t, matches[1].Id, receiptOrder[2])
		assert.Equal(t, matches[2].Id, receiptOrder[3])
	}
	rankings, _ := hubArena.Database.GetAllRankings()
	assert.Equal(t, 18, len(rankings))

	// The node receives the hub's rankings through the mirror.
	waitFor(t, "rankings to be mirrored", func() bool {
		nodeRankings, _ := nodeArena.Database.GetAllRankings()
		return len(nodeRankings) == 18
	})
}

func TestNodeResultWhenHubAnswersIncludesRankings(t *testing.T) {
	hubArena, _, server := setupTestHubServer(t)
	createHubMatches(t, hubArena.Database, 1)
	hubArena.NotifyDataChanged()
	nodeArena := setupTestNodeArena(t, 1, server.URL)
	node := Start(nodeArena)
	defer node.Stop()
	waitFor(t, "matches to be mirrored", func() bool {
		matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
		return len(matches) == 1
	})

	match, _ := nodeArena.Database.GetMatchById(1)
	matchResult := model.BuildTestMatchResult(match.Id, 1)
	field.PrepareMatchResult(match, matchResult)
	assert.Nil(t, nodeArena.SaveMatchResult(match, matchResult))
	receipt, err := node.SubmitResult(match, matchResult, false)
	assert.Nil(t, err)
	assert.False(t, receipt.Queued)
	assert.Equal(t, 6, len(receipt.Rankings))
	hubMatch, _ := hubArena.Database.GetMatchById(1)
	assert.True(t, hubMatch.IsComplete())
}

func TestNodeRefusesTeamsOnAnotherField(t *testing.T) {
	nodeArena := setupTestNodeArena(t, 1, "http://127.0.0.1:1")
	node := newNode(nodeArena)
	node.connected = true
	node.fieldTeams = []hub.FieldTeams{
		{FieldId: 1, TeamIds: []int{254}, MatchInProgress: true},
		{FieldId: 2, TeamIds: []int{1114, 2056}, MatchInProgress: true},
		{FieldId: 3, TeamIds: []int{604}, MatchInProgress: false},
	}
	assert.Nil(t, node.CheckTeamsAvailable([]int{254, 604, 0}))
	err := node.CheckTeamsAvailable([]int{1, 2056})
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "team 2056")
		assert.Contains(t, err.Error(), "field 2")
	}
}

func TestExportBundle(t *testing.T) {
	nodeArena := setupTestNodeArena(t, 2, "http://127.0.0.1:1")
	model.BaseDir = t.TempDir()
	node := newNode(nodeArena)
	database := nodeArena.Database
	onField := model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 2}
	otherField := model.Match{Type: model.Qualification, TypeOrder: 2, ShortName: "Q2", FieldId: 1}
	assert.Nil(t, database.CreateMatch(&onField))
	assert.Nil(t, database.CreateMatch(&otherField))
	for _, match := range []*model.Match{&onField, &otherField} {
		matchResult := model.BuildTestMatchResult(match.Id, 1)
		field.PrepareMatchResult(match, matchResult)
		assert.Nil(t, nodeArena.SaveMatchResult(match, matchResult))
	}
	assert.Nil(t, database.CreateSyncOutboxEntry(&model.SyncOutboxEntry{FieldId: 2, MatchId: onField.Id, PlayNumber: 1,
		Match: onField, MatchResult: *model.BuildTestMatchResult(onField.Id, 1)}))

	bundle, err := node.ExportBundle()
	assert.Nil(t, err)
	assert.Equal(t, 2, bundle.FieldId)
	if assert.Equal(t, 1, len(bundle.Submissions)) {
		assert.Equal(t, onField.Id, bundle.Submissions[0].MatchId)
	}
}

func TestRejectedResultIsKeptAndDoesNotBlockTheQueue(t *testing.T) {
	hubArena, _, server := setupTestHubServer(t)
	hubMatches := createHubMatches(t, hubArena.Database, 2)
	hubArena.NotifyDataChanged()
	nodeArena := setupTestNodeArena(t, 1, server.URL)
	node := Start(nodeArena)
	defer node.Stop()
	waitFor(t, "matches to be mirrored", func() bool {
		matches, _ := nodeArena.Database.GetMatchesByType(model.Qualification, true)
		return len(matches) == 2
	})

	// The hub moves Q1 to field 2 after the node has already played it.
	moved := hubMatches[0]
	moved.FieldId = 2
	assert.Nil(t, hubArena.Database.UpdateMatch(&moved))

	for _, matchId := range []int{hubMatches[0].Id, hubMatches[1].Id} {
		match, _ := nodeArena.Database.GetMatchById(matchId)
		match.FieldId = 1
		matchResult := model.BuildTestMatchResult(match.Id, 1)
		field.PrepareMatchResult(match, matchResult)
		assert.Nil(t, nodeArena.SaveMatchResult(match, matchResult))
		_, err := node.SubmitResult(match, matchResult, false)
		assert.Nil(t, err)
	}

	// Q2 still reaches the hub, while Q1 is kept on the node as rejected rather than being thrown away.
	waitFor(t, "Q2 to reach the hub", func() bool {
		match, _ := hubArena.Database.GetMatchById(hubMatches[1].Id)
		return match.IsComplete()
	})
	status := node.Status()
	assert.Equal(t, 0, status.OutboxSize)
	assert.Equal(t, 1, status.RejectedCount)
	assert.Contains(t, status.LastError, "Q1")
	entries, _ := nodeArena.Database.GetAllSyncOutboxEntries()
	if assert.Equal(t, 1, len(entries)) {
		assert.True(t, entries[0].Rejected)
		assert.Equal(t, hubMatches[0].Id, entries[0].MatchId)
	}
	bundle, err := node.ExportBundle()
	assert.Nil(t, err)
	bundledMatchIds := []int{}
	for _, submission := range bundle.Submissions {
		bundledMatchIds = append(bundledMatchIds, submission.MatchId)
	}
	assert.Contains(t, bundledMatchIds, hubMatches[0].Id)

	// After the hub admin moves the match back, a manual retry delivers it.
	moved.FieldId = 1
	assert.Nil(t, hubArena.Database.UpdateMatch(&moved))
	assert.Nil(t, node.RetryRejected())
	waitFor(t, "Q1 to reach the hub", func() bool {
		match, _ := hubArena.Database.GetMatchById(hubMatches[0].Id)
		return match.IsComplete()
	})
	waitFor(t, "outbox to empty", func() bool {
		entries, _ := nodeArena.Database.GetAllSyncOutboxEntries()
		return len(entries) == 0
	})
}

func TestNodeWontLoadMatchOnAnotherField(t *testing.T) {
	nodeArena := setupTestNodeArena(t, 1, "http://127.0.0.1:1")
	otherField := model.Match{Type: model.Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 2}
	unassigned := model.Match{Type: model.Qualification, TypeOrder: 2, ShortName: "Q2"}
	thisField := model.Match{Type: model.Qualification, TypeOrder: 3, ShortName: "Q3", FieldId: 1}
	for _, match := range []*model.Match{&otherField, &unassigned, &thisField} {
		assert.Nil(t, nodeArena.Database.CreateMatch(match))
	}
	err := nodeArena.LoadMatch(&otherField)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "not assigned to this field")
	}
	assert.NotNil(t, nodeArena.LoadMatch(&unassigned))
	assert.Nil(t, nodeArena.LoadMatch(&thisField))
	assert.Nil(t, nodeArena.LoadTestMatch())
}

func TestNodeLoadsFirstMatchWhenScheduleArrives(t *testing.T) {
	hubArena, _, server := setupTestHubServer(t)
	nodeArena := setupTestNodeArena(t, 2, server.URL)
	node := Start(nodeArena)
	defer node.Stop()
	waitFor(t, "node to connect", func() bool { return node.Status().Connected })
	assert.Equal(t, model.Test, nodeArena.CurrentMatch.Type)

	// The hub generates the schedule; Q2 is the first match on field 2.
	matches := createHubMatches(t, hubArena.Database, 3)
	matches[1].FieldId = 2
	assert.Nil(t, hubArena.Database.UpdateMatch(&matches[1]))
	hubArena.NotifyDataChanged()
	waitFor(t, "Q2 to be loaded", func() bool { return nodeArena.CurrentMatch.ShortName == "Q2" })

	// Once the field has matches to play, a test match loaded on purpose isn't replaced by later updates.
	assert.Nil(t, nodeArena.LoadTestMatch())
	matches[2].FieldId = 2
	assert.Nil(t, hubArena.Database.UpdateMatch(&matches[2]))
	hubArena.NotifyDataChanged()
	waitFor(t, "Q3 to be mirrored", func() bool {
		match, _ := nodeArena.Database.GetMatchById(matches[2].Id)
		return match != nil && match.FieldId == 2
	})
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, model.Test, nodeArena.CurrentMatch.Type)
}
