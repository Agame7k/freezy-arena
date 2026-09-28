// Copyright 2026 Team 254. All Rights Reserved.
//
// Whole-table snapshot and replacement of the event data collections that a hub mirrors to its field nodes.

package model

import "fmt"

// Names of the collections that a hub mirrors to its nodes, in the order in which a node should apply them.
const (
	TeamsCollection        = "teams"
	ConferencesCollection  = "conferences"
	MatchesCollection      = "matches"
	MatchResultsCollection = "matchResults"
	RankingsCollection     = "rankings"
	AlliancesCollection    = "alliances"
	BreaksCollection       = "breaks"
	AwardsCollection       = "awards"
	EventSettingsSharedKey = "eventSettings"
)

// MirroredCollections lists the table-backed collections that are mirrored from the hub to nodes. The shared event
// settings are handled separately since they are merged into the local record rather than replacing it.
var MirroredCollections = []string{
	TeamsCollection,
	ConferencesCollection,
	MatchesCollection,
	MatchResultsCollection,
	RankingsCollection,
	AlliancesCollection,
	BreaksCollection,
	AwardsCollection,
}

type collectionTable interface {
	snapshotJson() ([]byte, error)
	replaceFromJson(recordsJson []byte) error
}

func (database *Database) collectionTable(collection string) (collectionTable, error) {
	switch collection {
	case TeamsCollection:
		return database.teamTable, nil
	case ConferencesCollection:
		return database.conferenceTable, nil
	case MatchesCollection:
		return database.matchTable, nil
	case MatchResultsCollection:
		return database.matchResultTable, nil
	case RankingsCollection:
		return database.rankingTable, nil
	case AlliancesCollection:
		return database.allianceTable, nil
	case BreaksCollection:
		return database.scheduledBreakTable, nil
	case AwardsCollection:
		return database.awardTable, nil
	}
	return nil, fmt.Errorf("unknown collection %q", collection)
}

// SnapshotCollection returns a JSON array of every record in the given collection.
func (database *Database) SnapshotCollection(collection string) ([]byte, error) {
	table, err := database.collectionTable(collection)
	if err != nil {
		return nil, err
	}
	return table.snapshotJson()
}

// ReplaceCollection atomically replaces every record in the given collection with the records in the given JSON array,
// preserving their IDs.
func (database *Database) ReplaceCollection(collection string, recordsJson []byte) error {
	table, err := database.collectionTable(collection)
	if err != nil {
		return err
	}
	return table.replaceFromJson(recordsJson)
}
