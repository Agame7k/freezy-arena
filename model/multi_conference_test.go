// Copyright 2026 Team 254. All Rights Reserved.

package model

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"go.etcd.io/bbolt"
	"testing"
	"time"
)

func TestConferenceCrud(t *testing.T) {
	db := setupTestDb(t)

	conferences, err := db.GetAllConferences()
	assert.Nil(t, err)
	assert.Empty(t, conferences)

	conferences, err = db.EnsureConferences()
	assert.Nil(t, err)
	if assert.Equal(t, 2, len(conferences)) {
		assert.Equal(t, "North", conferences[0].Name)
		assert.Equal(t, "S", conferences[1].ShortName)
		assert.Equal(t, 3, conferences[0].SeriesLength("SF"))
		assert.Equal(t, 2, conferences[1].PlayoffFieldId)
	}

	conference := conferences[0]
	conference.Name = "East"
	conference.SeriesLengths["F"] = 5
	conference.PlayoffStartTime = time.Unix(1234, 0).UTC()
	assert.Nil(t, db.UpdateConference(&conference))

	// Ensuring again does not overwrite existing conferences.
	conferences, err = db.EnsureConferences()
	assert.Nil(t, err)
	assert.Equal(t, "East", conferences[0].Name)
	assert.Equal(t, 5, conferences[0].SeriesLength("F"))
	assert.Equal(t, time.Unix(1234, 0).UTC(), conferences[0].PlayoffStartTime)
}

func TestConferenceValidate(t *testing.T) {
	conference := Conference{
		Id: 1, Name: "North", ShortName: "N", PlayoffType: DoubleEliminationPlayoff, NumAlliances: 8, PlayoffFieldId: 1,
	}
	assert.Nil(t, conference.Validate())
	conference.NumAlliances = 9
	assert.NotNil(t, conference.Validate())
	conference.PlayoffType = SingleEliminationPlayoff
	assert.Nil(t, conference.Validate())
	conference.NumAlliances = 17
	assert.NotNil(t, conference.Validate())
	conference.NumAlliances = 10
	conference.SeriesLengths = map[string]int{"QF": 2}
	assert.NotNil(t, conference.Validate())
	conference.SeriesLengths = map[string]int{"QF": 5}
	assert.Nil(t, conference.Validate())
	conference.ShortName = ""
	assert.NotNil(t, conference.Validate())
}

// Records written before the multi-conference fields existed must load with zero values.
func TestOldRecordsLoadWithZeroValues(t *testing.T) {
	db := setupTestDb(t)
	oldTeam := []byte(`{"Id":254,"Name":"NASA","Nickname":"Cheesy Poofs","YellowCard":true}`)
	oldMatch := []byte(`{"Id":7,"Type":2,"TypeOrder":7,"ShortName":"Q7","Red1":254}`)
	oldAlliance := []byte(`{"Id":1,"TeamIds":[254,1114,2056],"Lineup":[1114,254,2056]}`)
	err := db.bolt.Update(
		func(tx *bbolt.Tx) error {
			if err := tx.Bucket([]byte("Team")).Put([]byte("254"), oldTeam); err != nil {
				return err
			}
			if err := tx.Bucket([]byte("Match")).Put([]byte("7"), oldMatch); err != nil {
				return err
			}
			return tx.Bucket([]byte("Alliance")).Put([]byte("1"), oldAlliance)
		},
	)
	assert.Nil(t, err)

	team, err := db.GetTeamById(254)
	assert.Nil(t, err)
	assert.Equal(t, 0, team.ConferenceId)
	assert.True(t, team.YellowCard)
	match, err := db.GetMatchById(7)
	assert.Nil(t, err)
	assert.Equal(t, 0, match.FieldId)
	assert.Equal(t, 0, match.ConferenceId)
	alliance, err := db.GetAllianceById(1)
	assert.Nil(t, err)
	assert.Equal(t, 0, alliance.ConferenceId)
	assert.Equal(t, 0, alliance.Seed)
}

func TestReplaceAndSnapshotCollection(t *testing.T) {
	db := setupTestDb(t)
	assert.Nil(t, db.CreateMatch(&Match{Type: Qualification, TypeOrder: 1, ShortName: "Q1"}))
	assert.Nil(t, db.CreateMatch(&Match{Type: Qualification, TypeOrder: 2, ShortName: "Q2"}))

	matches := []Match{
		{Id: 5, Type: Qualification, TypeOrder: 1, ShortName: "Q1", FieldId: 1},
		{Id: 9, Type: Qualification, TypeOrder: 2, ShortName: "Q2", FieldId: 2},
	}
	matchesJson, _ := json.Marshal(matches)
	assert.Nil(t, db.ReplaceCollection(MatchesCollection, matchesJson))

	allMatches, err := db.GetMatchesByType(Qualification, true)
	assert.Nil(t, err)
	assert.Equal(t, matches, allMatches)

	// New records get IDs after the highest replaced ID.
	newMatch := Match{Type: Practice, ShortName: "P1"}
	assert.Nil(t, db.CreateMatch(&newMatch))
	assert.Equal(t, 10, newMatch.Id)

	snapshot, err := db.SnapshotCollection(MatchesCollection)
	assert.Nil(t, err)
	var snapshotMatches []Match
	assert.Nil(t, json.Unmarshal(snapshot, &snapshotMatches))
	assert.Equal(t, 3, len(snapshotMatches))

	assert.NotNil(t, db.ReplaceCollection("bogus", []byte("[]")))
	assert.NotNil(t, db.ReplaceCollection(MatchesCollection, []byte(`[{"Id":0}]`)))
	// A failed replacement leaves the table untouched.
	allMatches, _ = db.GetMatchesByType(Qualification, true)
	assert.Equal(t, 2, len(allMatches))
}

func TestCopySharedEventSettings(t *testing.T) {
	local := EventSettings{
		Name: "Old", MultiFieldRole: NodeRole, FieldId: 2, FieldName: "Field 2", HubAddress: "http://hub",
		HubSharedSecret: "secret", ApAddress: "10.0.100.2", PlcAddress: "10.0.100.40", AdminPassword: "local",
	}
	hub := EventSettings{
		Name: "New", MultiFieldRole: HubRole, FieldId: 7, FieldName: "Hub", HubAddress: "x", HubSharedSecret: "y",
		ApAddress: "1.2.3.4", PlcAddress: "5.6.7.8", AdminPassword: "hub", MultiConferenceEnabled: true,
		ChampionshipFormat: DoubleDeckerChampionship, QualFieldAssignmentMode: DynamicFieldAssignment,
		NumPlayoffAlliances: 6,
	}
	CopySharedEventSettings(&local, &hub)
	assert.Equal(t, "New", local.Name)
	assert.True(t, local.MultiConferenceEnabled)
	assert.Equal(t, DoubleDeckerChampionship, local.ChampionshipFormat)
	assert.Equal(t, DynamicFieldAssignment, local.QualFieldAssignmentMode)
	assert.Equal(t, 6, local.NumPlayoffAlliances)
	assert.Equal(t, NodeRole, local.MultiFieldRole)
	assert.Equal(t, 2, local.FieldId)
	assert.Equal(t, "Field 2", local.FieldName)
	assert.Equal(t, "http://hub", local.HubAddress)
	assert.Equal(t, "secret", local.HubSharedSecret)
	assert.Equal(t, "10.0.100.2", local.ApAddress)
	assert.Equal(t, "10.0.100.40", local.PlcAddress)
	assert.Equal(t, "local", local.AdminPassword)
}

func TestSyncOutboxAndReceipts(t *testing.T) {
	db := setupTestDb(t)
	entry1 := SyncOutboxEntry{FieldId: 1, MatchId: 3, PlayNumber: 1}
	entry2 := SyncOutboxEntry{FieldId: 1, MatchId: 4, PlayNumber: 2}
	assert.Nil(t, db.CreateSyncOutboxEntry(&entry1))
	assert.Nil(t, db.CreateSyncOutboxEntry(&entry2))
	entries, err := db.GetAllSyncOutboxEntries()
	assert.Nil(t, err)
	if assert.Equal(t, 2, len(entries)) {
		assert.Equal(t, "1-3-1", entries[0].IdempotencyKey())
		assert.Equal(t, "1-4-2", entries[1].IdempotencyKey())
	}
	assert.Nil(t, db.DeleteSyncOutboxEntry(entry1.Id))
	entries, _ = db.GetAllSyncOutboxEntries()
	assert.Equal(t, 1, len(entries))

	receipt := HubResultReceipt{Key: "2-5-1", FieldId: 2, MatchId: 5, PlayNumber: 1}
	assert.Nil(t, db.CreateHubResultReceipt(&receipt))
	found, err := db.GetHubResultReceiptByKey("2-5-1")
	assert.Nil(t, err)
	assert.Equal(t, 5, found.MatchId)
	found, err = db.GetHubResultReceiptByKey("2-5-2")
	assert.Nil(t, err)
	assert.Nil(t, found)

	registration := NodeRegistration{Id: 2, FieldName: "Field 2"}
	assert.Nil(t, db.CreateNodeRegistration(&registration))
	registration.Approved = true
	assert.Nil(t, db.UpdateNodeRegistration(&registration))
	registrations, err := db.GetAllNodeRegistrations()
	assert.Nil(t, err)
	assert.Equal(t, []NodeRegistration{registration}, registrations)
}

func TestNormalizeHubAddress(t *testing.T) {
	for _, test := range []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"  ", ""},
		{"192.168.50.10", "http://192.168.50.10:8080"},
		{"192.168.50.10:8081", "http://192.168.50.10:8081"},
		{"http://192.168.50.10", "http://192.168.50.10:8080"},
		{"http://hub.local:9000/", "http://hub.local:9000"},
		{"https://hub.example.com", "https://hub.example.com"},
		{"localhost", "http://localhost:8080"},
		{" http://10.0.100.5:8080/event_control?x=1 ", "http://10.0.100.5:8080/event_control"},
	} {
		address, err := NormalizeHubAddress(test.input)
		assert.Nil(t, err, test.input)
		assert.Equal(t, test.expected, address, test.input)
	}
	for _, input := range []string{"ftp://192.168.50.10", "http://", "http://:8080"} {
		_, err := NormalizeHubAddress(input)
		assert.NotNil(t, err, input)
	}
}
