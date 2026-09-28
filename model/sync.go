// Copyright 2026 Team 254. All Rights Reserved.
//
// Models used for synchronizing data between a multi-field hub and its field nodes.

package model

import (
	"fmt"
	"sort"
	"time"
)

// SyncOutboxEntry is a committed match result on a node that is waiting to be delivered to the hub.
type SyncOutboxEntry struct {
	Id          int `db:"id"`
	FieldId     int
	MatchId     int
	PlayNumber  int
	IsEdit      bool
	Match       Match
	MatchResult MatchResult
	CreatedAt   time.Time
	Attempts    int
	LastError   string
	// Set when the hub refused the result outright; the entry is kept (and exported in results bundles) but is no longer
	// retried automatically.
	Rejected bool
}

// IdempotencyKey returns the key that uniquely identifies this result submission.
func (entry *SyncOutboxEntry) IdempotencyKey() string {
	return ResultIdempotencyKey(entry.FieldId, entry.MatchId, entry.PlayNumber)
}

// ResultIdempotencyKey builds the key that the hub uses to detect duplicate result submissions.
func ResultIdempotencyKey(fieldId, matchId, playNumber int) string {
	return fmt.Sprintf("%d-%d-%d", fieldId, matchId, playNumber)
}

func (database *Database) CreateSyncOutboxEntry(entry *SyncOutboxEntry) error {
	return database.syncOutboxTable.create(entry)
}

func (database *Database) UpdateSyncOutboxEntry(entry *SyncOutboxEntry) error {
	return database.syncOutboxTable.update(entry)
}

func (database *Database) DeleteSyncOutboxEntry(id int) error {
	return database.syncOutboxTable.delete(id)
}

func (database *Database) TruncateSyncOutbox() error {
	return database.syncOutboxTable.truncate()
}

// GetAllSyncOutboxEntries returns the outbox entries in the order in which they were created.
func (database *Database) GetAllSyncOutboxEntries() ([]SyncOutboxEntry, error) {
	entries, err := database.syncOutboxTable.getAll()
	if err != nil {
		return nil, err
	}
	sort.Slice(
		entries,
		func(i, j int) bool {
			return entries[i].Id < entries[j].Id
		},
	)
	return entries, nil
}

// HubResultReceipt records a result submission that the hub has already applied, for idempotency.
type HubResultReceipt struct {
	Id         int `db:"id"`
	Key        string
	FieldId    int
	MatchId    int
	PlayNumber int
	ReceivedAt time.Time
}

func (database *Database) CreateHubResultReceipt(receipt *HubResultReceipt) error {
	return database.hubResultReceiptTable.create(receipt)
}

// GetHubResultReceiptByKey returns the receipt with the given idempotency key, or nil if none exists.
func (database *Database) GetHubResultReceiptByKey(key string) (*HubResultReceipt, error) {
	receipts, err := database.hubResultReceiptTable.getAll()
	if err != nil {
		return nil, err
	}
	for i := range receipts {
		if receipts[i].Key == key {
			return &receipts[i], nil
		}
	}
	return nil, nil
}

func (database *Database) GetAllHubResultReceipts() ([]HubResultReceipt, error) {
	return database.hubResultReceiptTable.getAll()
}

func (database *Database) TruncateHubResultReceipts() error {
	return database.hubResultReceiptTable.truncate()
}

// NodeRegistration is a field node that has contacted the hub; the hub admin must approve it before its requests are
// accepted.
type NodeRegistration struct {
	Id         int `db:"id,manual"` // Equal to the node's field ID.
	FieldName  string
	Approved   bool
	RemoteAddr string
	FirstSeen  time.Time
}

func (database *Database) CreateNodeRegistration(registration *NodeRegistration) error {
	return database.nodeRegistrationTable.create(registration)
}

func (database *Database) GetNodeRegistrationById(fieldId int) (*NodeRegistration, error) {
	return database.nodeRegistrationTable.getById(fieldId)
}

func (database *Database) UpdateNodeRegistration(registration *NodeRegistration) error {
	return database.nodeRegistrationTable.update(registration)
}

func (database *Database) DeleteNodeRegistration(fieldId int) error {
	return database.nodeRegistrationTable.delete(fieldId)
}

func (database *Database) GetAllNodeRegistrations() ([]NodeRegistration, error) {
	registrations, err := database.nodeRegistrationTable.getAll()
	if err != nil {
		return nil, err
	}
	sort.Slice(
		registrations,
		func(i, j int) bool {
			return registrations[i].Id < registrations[j].Id
		},
	)
	return registrations, nil
}
