// Copyright 2026 Team 254. All Rights Reserved.

package hub

import (
	"bytes"
	"encoding/json"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "correct horse"

func setupTestHub(t *testing.T) *Hub {
	arena := field.SetupTestArena(t)
	model.BaseDir = t.TempDir()
	arena.EventSettings.MultiFieldRole = model.HubRole
	arena.EventSettings.HubSharedSecret = testSecret
	assert.Nil(t, arena.Database.UpdateEventSettings(arena.EventSettings))
	hub := newHub(arena)
	hub.AutoApproveNodes = true
	return hub
}

func createTestQualMatches(t *testing.T, database *model.Database, count int) []model.Match {
	var matches []model.Match
	for i := 1; i <= count; i++ {
		teamBase := 100 * i
		match := model.Match{
			Type:      model.Qualification,
			TypeOrder: i,
			ShortName: "Q" + strconv.Itoa(i),
			LongName:  "Qualification " + strconv.Itoa(i),
			Time:      time.Unix(int64(1000*i), 0),
			Red1:      teamBase + 1, Red2: teamBase + 2, Red3: teamBase + 3,
			Blue1: teamBase + 4, Blue2: teamBase + 5, Blue3: teamBase + 6,
			FieldId: 1 + (i+1)%2,
			Status:  game.MatchScheduled,
		}
		assert.Nil(t, database.CreateMatch(&match))
		for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
			_ = database.CreateTeam(&model.Team{Id: teamId})
		}
		matches = append(matches, match)
	}
	return matches
}

func testSubmission(match model.Match, fieldId, playNumber int) *ResultSubmission {
	result := model.BuildTestMatchResult(match.Id, playNumber)
	result.RedCards = map[string]string{}
	return &ResultSubmission{
		FieldId:     fieldId,
		MatchId:     match.Id,
		PlayNumber:  playNumber,
		Match:       match,
		MatchResult: *result,
	}
}

func TestDuplicateResultIsAppliedOnce(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 2)

	response, err := hub.ApplySubmission(testSubmission(matches[0], 1, 1))
	assert.Nil(t, err)
	assert.True(t, response.Applied)
	assert.False(t, response.Duplicate)
	assert.Equal(t, 6, len(response.Rankings))

	response, err = hub.ApplySubmission(testSubmission(matches[0], 1, 1))
	assert.Nil(t, err)
	assert.False(t, response.Applied)
	assert.True(t, response.Duplicate)

	matchResult, err := hub.arena.Database.GetMatchResultForMatch(matches[0].Id)
	assert.Nil(t, err)
	assert.Equal(t, 1, matchResult.PlayNumber)
	receipts, _ := hub.arena.Database.GetAllHubResultReceipts()
	assert.Equal(t, 1, len(receipts))
	match, _ := hub.arena.Database.GetMatchById(matches[0].Id)
	assert.True(t, match.IsComplete())
	rankings, _ := hub.arena.Database.GetAllRankings()
	for _, ranking := range rankings {
		assert.Equal(t, 1, ranking.Played)
	}
}

func TestResultForWrongFieldIsRejected(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 2)

	// Q2 is on field 2.
	_, err := hub.ApplySubmission(testSubmission(matches[1], 1, 1))
	if assert.NotNil(t, err) {
		assert.Equal(t, 409, err.(*SubmissionError).StatusCode)
		assert.Contains(t, err.Error(), "assigned to field 2")
	}
	matchResult, _ := hub.arena.Database.GetMatchResultForMatch(matches[1].Id)
	assert.Nil(t, matchResult)

	_, err = hub.ApplySubmission(&ResultSubmission{FieldId: 1, MatchId: 999, PlayNumber: 1, Match: model.Match{Id: 999}})
	if assert.NotNil(t, err) {
		assert.Equal(t, 404, err.(*SubmissionError).StatusCode)
	}
}

func TestOlderResultIsAConflictAndHubWins(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 1)

	_, err := hub.ApplySubmission(testSubmission(matches[0], 1, 2))
	assert.Nil(t, err)
	response, err := hub.ApplySubmission(testSubmission(matches[0], 1, 1))
	assert.Nil(t, err)
	assert.True(t, response.Conflict)
	assert.False(t, response.Applied)
	assert.Equal(t, 1, len(hub.Conflicts()))
	matchResult, _ := hub.arena.Database.GetMatchResultForMatch(matches[0].Id)
	assert.Equal(t, 2, matchResult.PlayNumber)

	// A newer play is applied normally.
	response, err = hub.ApplySubmission(testSubmission(matches[0], 1, 3))
	assert.Nil(t, err)
	assert.True(t, response.Applied)
}

func TestResultsHandlerAuthentication(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 1)
	server := httptest.NewServer(http.HandlerFunc(hub.ResultsHandler))
	defer server.Close()

	post := func(secret string, fieldId int, submission *ResultSubmission) *http.Response {
		body, _ := json.Marshal(submission)
		request, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
		request.Header.Set(SecretHeader, secret)
		request.Header.Set(FieldIdHeader, strconv.Itoa(fieldId))
		response, err := http.DefaultClient.Do(request)
		assert.Nil(t, err)
		return response
	}

	assert.Equal(t, http.StatusUnauthorized, post("wrong", 1, testSubmission(matches[0], 1, 1)).StatusCode)

	// Only fields 1 and 2 exist, so any other field ID is refused without being registered.
	for _, fieldId := range []int{0, 3, 99} {
		assert.Equal(t, http.StatusBadRequest, post(testSecret, fieldId, testSubmission(matches[0], 1, 1)).StatusCode)
		registration, _ := hub.arena.Database.GetNodeRegistrationById(fieldId)
		assert.Nil(t, registration)
	}

	// A node that hasn't been approved is refused.
	hub.AutoApproveNodes = false
	assert.Equal(t, http.StatusForbidden, post(testSecret, 2, testSubmission(matches[0], 2, 1)).StatusCode)
	registration, _ := hub.arena.Database.GetNodeRegistrationById(2)
	assert.False(t, registration.Approved)

	// Register field 1 while new nodes are approved automatically; it can't send results as another field.
	hub.AutoApproveNodes = true
	assert.Equal(t, http.StatusForbidden, post(testSecret, 1, testSubmission(matches[0], 2, 1)).StatusCode)
	assert.Equal(t, http.StatusOK, post(testSecret, 1, testSubmission(matches[0], 1, 1)).StatusCode)

	// Approving the node lets its requests through; resending the same result is acknowledged as a duplicate.
	assert.Nil(t, hub.ApproveNode(2, true))
	response := post(testSecret, 1, testSubmission(matches[0], 1, 1))
	assert.Equal(t, http.StatusOK, response.StatusCode)
	var resultResponse ResultResponse
	assert.Nil(t, json.NewDecoder(response.Body).Decode(&resultResponse))
	assert.True(t, resultResponse.Duplicate)
	registration, _ = hub.arena.Database.GetNodeRegistrationById(2)
	assert.True(t, registration.Approved)
}

func TestSecondLaptopWithSameFieldIsRefused(t *testing.T) {
	hub := setupTestHub(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /websocket", hub.NodeWebsocketHandler)
	mux.HandleFunc("GET /versions", hub.VersionsHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	headers := func(instance string) http.Header {
		header := http.Header{}
		header.Set(SecretHeader, testSecret)
		header.Set(FieldIdHeader, "1")
		header.Set(NodeInstanceHeader, instance)
		return header
	}
	getVersions := func(instance string) *http.Response {
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/versions", nil)
		request.Header = headers(instance)
		response, err := http.DefaultClient.Do(request)
		assert.Nil(t, err)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		response.Status = string(body)
		return response
	}

	// The first laptop connects as field 1.
	websocketUrl := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket"
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(websocketUrl, headers("laptop-a"))
	if !assert.Nil(t, err) {
		return
	}
	assert.Equal(t, http.StatusOK, getVersions("laptop-a").StatusCode)

	// A second laptop that is also set up as field 1 is refused, with an explanation for both operators.
	_, response, err := gorillawebsocket.DefaultDialer.Dial(websocketUrl, headers("laptop-b"))
	assert.NotNil(t, err)
	if assert.NotNil(t, response) {
		assert.Equal(t, http.StatusLocked, response.StatusCode)
	}
	refused := getVersions("laptop-b")
	assert.Equal(t, http.StatusLocked, refused.StatusCode)
	assert.Contains(t, refused.Status, "each field laptop needs its own field ID")
	statuses := hub.NodeStatuses()
	if assert.Equal(t, 1, len(statuses)) {
		assert.NotEmpty(t, statuses[0].DuplicateFrom)
	}
	// Repeated attempts are logged once rather than flooding the conflict log.
	assert.Equal(t, 1, len(hub.Conflicts()))
	assert.Contains(t, hub.Conflicts()[0], "a second laptop")

	// A node that doesn't identify itself (an older version) isn't affected.
	assert.Equal(t, http.StatusOK, getVersions("").StatusCode)

	// Once the first laptop disconnects, the second one can take over.
	assert.Nil(t, conn.Close())
	assert.Eventually(
		t,
		func() bool { return getVersions("laptop-b").StatusCode == http.StatusOK },
		5*time.Second,
		50*time.Millisecond,
	)
}

func TestSnapshotVersionsAndSharedSettings(t *testing.T) {
	hub := setupTestHub(t)
	hub.arena.EventSettings.AdminPassword = "hub-admin"
	hub.arena.EventSettings.ApAddress = "10.0.100.2"
	hub.arena.EventSettings.MultiConferenceEnabled = true

	hub.scan()
	versions := hub.Versions()
	for _, collection := range append(model.MirroredCollections, model.EventSettingsSharedKey) {
		assert.NotZero(t, versions[collection], collection)
	}

	// Unchanged data keeps its version.
	hub.scan()
	assert.Equal(t, versions, hub.Versions())

	// Changed data gets a newer version.
	createTestQualMatches(t, hub.arena.Database, 1)
	hub.scan()
	newVersions := hub.Versions()
	assert.Greater(t, newVersions[model.MatchesCollection], versions[model.MatchesCollection])
	assert.Greater(t, newVersions[model.TeamsCollection], versions[model.TeamsCollection])
	assert.Equal(t, versions[model.AwardsCollection], newVersions[model.AwardsCollection])

	// The shared settings snapshot leaves out anything local to the hub.
	_, snapshot, err := hub.Snapshot(model.EventSettingsSharedKey)
	assert.Nil(t, err)
	var shared model.EventSettings
	assert.Nil(t, json.Unmarshal(snapshot, &shared))
	assert.True(t, shared.MultiConferenceEnabled)
	assert.Equal(t, "", shared.AdminPassword)
	assert.Equal(t, "", shared.ApAddress)
	assert.Equal(t, "", shared.HubSharedSecret)
	assert.Equal(t, model.StandaloneRole, shared.MultiFieldRole)
}

func TestClaimAndReleaseRace(t *testing.T) {
	hub := setupTestHub(t)
	hub.arena.EventSettings.QualFieldAssignmentMode = model.DynamicFieldAssignment
	hub.arena.EventSettings.MinTurnaroundSec = 0
	matches := createTestQualMatches(t, hub.arena.Database, 4)
	for _, match := range matches {
		match.FieldId = 0
		assert.Nil(t, hub.arena.Database.UpdateMatch(&match))
	}

	// Two nodes claim at the same moment and must get different matches.
	var wait sync.WaitGroup
	claimed := make([]*model.Match, 3)
	for fieldId := 1; fieldId <= 2; fieldId++ {
		wait.Add(1)
		go func(fieldId int) {
			defer wait.Done()
			match, _, err := hub.ClaimNextMatch(fieldId)
			assert.Nil(t, err)
			claimed[fieldId] = match
		}(fieldId)
	}
	wait.Wait()
	if assert.NotNil(t, claimed[1]) && assert.NotNil(t, claimed[2]) {
		assert.NotEqual(t, claimed[1].Id, claimed[2].Id)
		assert.ElementsMatch(t, []int{1, 2}, []int{claimed[1].TypeOrder, claimed[2].TypeOrder})
	}

	// Claiming again returns the field's existing unplayed claim.
	again, _, err := hub.ClaimNextMatch(1)
	assert.Nil(t, err)
	assert.Equal(t, claimed[1].Id, again.Id)

	// Releasing gives the match back; the other field can't release a match it doesn't hold.
	assert.NotNil(t, hub.ReleaseMatch(2, claimed[1].Id))
	assert.Nil(t, hub.ReleaseMatch(1, claimed[1].Id))
	match, _ := hub.arena.Database.GetMatchById(claimed[1].Id)
	assert.Equal(t, 0, match.FieldId)
}

func TestClaimRespectsBusyTeamsAndTurnaround(t *testing.T) {
	hub := setupTestHub(t)
	hub.arena.EventSettings.QualFieldAssignmentMode = model.DynamicFieldAssignment
	hub.arena.EventSettings.MinTurnaroundSec = 600
	database := hub.arena.Database
	matches := createTestQualMatches(t, database, 3)
	// Q2 shares a team with Q1, and Q3 shares a team with a match that just finished.
	matches[1].Red1 = matches[0].Red1
	matches[2].Blue3 = 9999
	finished := model.Match{
		Type: model.Qualification, TypeOrder: 99, Red1: 9999, Status: game.RedWonMatch,
		StartedAt: time.Now().Add(-10 * time.Minute),
		FieldId:   2,
	}
	assert.Nil(t, database.CreateMatch(&finished))
	for _, match := range matches {
		match.FieldId = 0
		assert.Nil(t, database.UpdateMatch(&match))
	}

	match, reason, err := hub.ClaimNextMatch(1)
	assert.Nil(t, err)
	if !assert.NotNil(t, match, reason) {
		return
	}
	assert.Equal(t, 1, match.TypeOrder)

	// Q2's team is busy on field 1 and Q3's team hasn't rested, so field 2 gets nothing.
	match, reason, err = hub.ClaimNextMatch(2)
	assert.Nil(t, err)
	assert.Nil(t, match)
	assert.Contains(t, reason, "Q2")

	// With no turnaround requirement, Q3 becomes available.
	hub.arena.EventSettings.MinTurnaroundSec = 0
	match, reason, err = hub.ClaimNextMatch(2)
	assert.Nil(t, err)
	if assert.NotNil(t, match, reason) {
		assert.Equal(t, 3, match.TypeOrder)
	}
}

func TestImportBundle(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 3)
	bundle := ResultsBundle{
		FieldId: 1,
		Submissions: []ResultSubmission{
			*testSubmission(matches[0], 1, 1),
			*testSubmission(matches[2], 1, 1),
			*testSubmission(matches[1], 1, 1),
		},
	}
	result, err := hub.ImportBundle(&bundle)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(result.Lines))
	assert.Contains(t, result.Lines[0], "applied")
	assert.Contains(t, result.Lines[1], "applied")
	assert.Contains(t, result.Lines[2], "not applied")
	assert.Equal(t, [3]int{2, 0, 1}, [3]int{result.Applied, result.AlreadyApplied, result.NotApplied})

	// Importing the same bundle again changes nothing.
	result, _ = hub.ImportBundle(&bundle)
	assert.Contains(t, result.Lines[0], "already applied")
	assert.Equal(t, [3]int{0, 2, 1}, [3]int{result.Applied, result.AlreadyApplied, result.NotApplied})

	// Something that isn't a bundle from a field is refused as a whole.
	_, err = hub.ImportBundle(&ResultsBundle{})
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "isn't a results bundle from a field laptop")
	}
}

func TestImportBundleAppliesPlayoffResultsInAnyOrder(t *testing.T) {
	hub := setupTestHub(t)
	arena := hub.arena
	for i := 1; i <= 8; i++ {
		assert.Nil(t, arena.Database.CreateAlliance(&model.Alliance{
			Id: i, TeamIds: []int{100 * i, 100*i + 1, 100*i + 2}, Lineup: [3]int{100 * i, 100*i + 1, 100*i + 2},
		}))
	}
	assert.Nil(t, arena.CreatePlayoffTournament())
	assert.Nil(t, arena.CreatePlayoffMatches(time.Now()))
	playoffMatches, _ := arena.Database.GetMatchesByType(model.Playoff, false)
	matchesByOrder := map[int]model.Match{}
	for _, match := range playoffMatches {
		match.FieldId = 1
		assert.Nil(t, arena.Database.UpdateMatch(&match))
		matchesByOrder[match.TypeOrder] = match
	}
	// Match 7 is between the winners of matches 1 and 2, so it has no alliances until they are played.
	if !assert.Zero(t, matchesByOrder[7].PlayoffRedAlliance) {
		return
	}

	bundle := ResultsBundle{
		FieldId: 1,
		Submissions: []ResultSubmission{
			*testSubmission(matchesByOrder[7], 1, 1),
			*testSubmission(matchesByOrder[1], 1, 1),
			*testSubmission(matchesByOrder[2], 1, 1),
		},
	}
	result, err := hub.ImportBundle(&bundle)
	assert.Nil(t, err)
	assert.Equal(t, 3, result.Applied, result.Lines)
	match7, _ := arena.Database.GetMatchById(matchesByOrder[7].Id)
	assert.True(t, match7.IsComplete())
}

func TestAssignableMatchesAreGroupedByField(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 4)
	matches[2].FieldId = 0
	assert.Nil(t, hub.arena.Database.UpdateMatch(&matches[2]))
	_, err := hub.ApplySubmission(testSubmission(matches[0], 1, 1))
	assert.Nil(t, err)

	// Q1 has been played, so it can't be moved, which leaves nothing on field 1.
	groups := hub.AssignableMatches()
	if assert.Equal(t, 2, len(groups)) {
		assert.Equal(t, "No field yet", groups[0].Label)
		assert.Equal(t, "Q3", groups[0].Matches[0].ShortName)
		assert.Equal(t, "On Field 2", groups[1].Label)
		assert.Equal(t, 2, groups[1].FieldId)
		assert.Equal(t, 2, len(groups[1].Matches))
	}
}

func TestPlayoffSubmissionNeverBlanksTheLineup(t *testing.T) {
	hub := setupTestHub(t)
	database := hub.arena.Database
	assert.Nil(t, database.CreateAlliance(&model.Alliance{Id: 1, TeamIds: []int{101, 102, 103}}))
	assert.Nil(t, database.CreateAlliance(&model.Alliance{Id: 2, TeamIds: []int{201, 202, 203}}))
	notReady := model.Match{
		Type: model.Playoff, TypeOrder: 1, ShortName: "C1", FieldId: 1, PlayoffRedAlliance: 1, Red1: 101,
		Red2: 102, Red3: 103,
	}
	assert.Nil(t, database.CreateMatch(&notReady))
	_, err := hub.ApplySubmission(testSubmission(notReady, 1, 1))
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "doesn't have both alliances")
	}

	ready := model.Match{
		Type: model.Playoff, TypeOrder: 2, ShortName: "C2", FieldId: 1, PlayoffRedAlliance: 1,
		PlayoffBlueAlliance: 2, Red1: 101, Red2: 102, Red3: 103, Blue1: 201, Blue2: 202, Blue3: 203,
	}
	assert.Nil(t, database.CreateMatch(&ready))
	stale := ready
	stale.Blue1, stale.Blue2, stale.Blue3 = 0, 0, 0
	stale.Red3 = 104
	_, _ = hub.ApplySubmission(testSubmission(stale, 1, 1))
	match, _ := database.GetMatchById(ready.Id)
	assert.Equal(t, 104, match.Red3)
	assert.Equal(t, [3]int{201, 202, 203}, [3]int{match.Blue1, match.Blue2, match.Blue3})
	assert.NotEmpty(t, hub.Conflicts())
}

func TestPlayoffSubmissionBeforeAlliancesIsRetryable(t *testing.T) {
	hub := setupTestHub(t)
	database := hub.arena.Database
	assert.Nil(t, database.CreateAlliance(&model.Alliance{Id: 1, TeamIds: []int{101, 102, 103}}))
	notReady := model.Match{
		Type: model.Playoff, TypeOrder: 1, ShortName: "M5", FieldId: 1, PlayoffRedAlliance: 1, Red1: 101,
	}
	assert.Nil(t, database.CreateMatch(&notReady))
	_, err := hub.ApplySubmission(testSubmission(notReady, 1, 1))
	if assert.NotNil(t, err) {
		assert.Equal(t, http.StatusTooEarly, err.(*SubmissionError).StatusCode)
	}
	// No receipt is recorded, so the same submission is applied once the alliances are known.
	receipts, _ := database.GetAllHubResultReceipts()
	assert.Empty(t, receipts)
}

func TestConcurrentDuplicateSubmissionsAreAppliedOnce(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 1)

	var wait sync.WaitGroup
	var mutex sync.Mutex
	applied := 0
	for i := 0; i < 10; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := hub.ApplySubmission(testSubmission(matches[0], 1, 1))
			assert.Nil(t, err)
			if response != nil && response.Applied {
				mutex.Lock()
				applied++
				mutex.Unlock()
			}
		}()
	}
	wait.Wait()
	assert.Equal(t, 1, applied)
	receipts, _ := hub.arena.Database.GetAllHubResultReceipts()
	assert.Equal(t, 1, len(receipts))
	matchResults, _ := hub.arena.Database.GetMatchResultForMatch(matches[0].Id)
	assert.Equal(t, 1, matchResults.PlayNumber)
}

func TestEditFromNodeReplacesResultAndRankings(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 1)

	first := testSubmission(matches[0], 1, 1)
	first.MatchResult.RedScore = &game.Score{}
	first.MatchResult.BlueScore = &game.Score{AutoTowerStatuses: [3]game.TowerStatus{1, 1, 1}}
	response, err := hub.ApplySubmission(first)
	assert.Nil(t, err)
	assert.True(t, response.Applied)
	match, _ := hub.arena.Database.GetMatchById(matches[0].Id)
	assert.Equal(t, game.BlueWonMatch, match.Status)

	// The node's match review sends the corrected score as the next play of the match.
	edit := testSubmission(matches[0], 1, 2)
	edit.IsEdit = true
	edit.MatchResult.RedScore = &game.Score{AutoTowerStatuses: [3]game.TowerStatus{1, 1, 1}}
	edit.MatchResult.BlueScore = &game.Score{}
	response, err = hub.ApplySubmission(edit)
	assert.Nil(t, err)
	assert.True(t, response.Applied)
	match, _ = hub.arena.Database.GetMatchById(matches[0].Id)
	assert.Equal(t, game.RedWonMatch, match.Status)
	matchResult, _ := hub.arena.Database.GetMatchResultForMatch(matches[0].Id)
	assert.Equal(t, 2, matchResult.PlayNumber)
	for _, ranking := range response.Rankings {
		assert.Equal(t, 1, ranking.Played)
		if ranking.TeamId <= 103 {
			assert.Equal(t, 1, ranking.Wins, "team %d", ranking.TeamId)
		} else {
			assert.Equal(t, 0, ranking.Wins, "team %d", ranking.TeamId)
		}
	}
}

func TestAssignMatchFieldValidation(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 2)

	assert.NotNil(t, hub.AssignMatchField(matches[0].Id, 3))
	assert.NotNil(t, hub.AssignMatchField(matches[0].Id, -1))
	assert.NotNil(t, hub.AssignMatchField(999, 1))

	// A match that field 1 is playing right now can't be moved to field 2.
	hub.mutex.Lock()
	status := hub.statusFor(1)
	status.Connected = true
	status.LastContact = time.Now()
	status.Report = NodeStatusReport{CurrentMatchId: matches[0].Id, MatchInProgress: true}
	hub.mutex.Unlock()
	err := hub.AssignMatchField(matches[0].Id, 2)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "being played on field 1")
	}

	// Once the match is over (or just loaded), it can be moved.
	hub.mutex.Lock()
	status.Report.MatchInProgress = false
	hub.mutex.Unlock()
	assert.Nil(t, hub.AssignMatchField(matches[0].Id, 2))
	match, _ := hub.arena.Database.GetMatchById(matches[0].Id)
	assert.Equal(t, 2, match.FieldId)

	// A played match can't be moved.
	_, err = hub.ApplySubmission(testSubmission(*match, 2, 1))
	assert.Nil(t, err)
	assert.NotNil(t, hub.AssignMatchField(matches[0].Id, 1))
}

func TestClaimUsesCommitTimeForRest(t *testing.T) {
	hub := setupTestHub(t)
	hub.arena.EventSettings.QualFieldAssignmentMode = model.DynamicFieldAssignment
	hub.arena.EventSettings.MinTurnaroundSec = 0
	database := hub.arena.Database
	matches := createTestQualMatches(t, database, 2)
	for _, match := range matches {
		match.FieldId = 0
		assert.Nil(t, database.UpdateMatch(&match))
	}

	// Q1's team just played a match that finished early; with no turnaround required, Q1 is available right away.
	now := time.Now()
	finished := model.Match{
		Type: model.Qualification, TypeOrder: 99, Red1: matches[0].Red1, Status: game.RedWonMatch, FieldId: 2,
		StartedAt: now.Add(-40 * time.Second), ScoreCommittedAt: now.Add(-time.Second),
	}
	assert.Nil(t, database.CreateMatch(&finished))
	match, reason, err := hub.claimNextMatch(1, now)
	assert.Nil(t, err)
	if assert.NotNil(t, match, reason) {
		assert.Equal(t, matches[0].Id, match.Id)
	}

	// A team whose match has started but not been committed is still on the field.
	underway := model.Match{
		Type: model.Qualification, TypeOrder: 100, Blue1: matches[1].Blue1, FieldId: 0,
		StartedAt: now.Add(-40 * time.Second),
	}
	assert.Nil(t, database.CreateMatch(&underway))
	match, reason, err = hub.claimNextMatch(2, now)
	assert.Nil(t, err)
	assert.Nil(t, match)
	assert.Contains(t, reason, "still finishing")
}

func TestHubKeepsNodeCommitTime(t *testing.T) {
	hub := setupTestHub(t)
	matches := createTestQualMatches(t, hub.arena.Database, 2)

	// A result queued during an outage keeps the time it was committed on the field.
	committedAt := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	submission := testSubmission(matches[0], 1, 1)
	submission.Match.ScoreCommittedAt = committedAt
	_, err := hub.ApplySubmission(submission)
	assert.Nil(t, err)
	match, _ := hub.arena.Database.GetMatchById(matches[0].Id)
	assert.True(t, committedAt.Equal(match.ScoreCommittedAt), match.ScoreCommittedAt)

	// A node clock running ahead of the hub can't record a commit time in the future.
	submission = testSubmission(matches[1], 2, 1)
	submission.Match.ScoreCommittedAt = time.Now().Add(time.Hour)
	_, err = hub.ApplySubmission(submission)
	assert.Nil(t, err)
	match, _ = hub.arena.Database.GetMatchById(matches[1].Id)
	assert.False(t, match.ScoreCommittedAt.After(time.Now()))
}
