// Copyright 2026 Team 254. All Rights Reserved.
//
// Message types exchanged between a multi-field hub and its field nodes.

package hub

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
)

const (
	// HTTP headers used to authenticate node requests.
	SecretHeader    = "X-Hub-Secret"
	FieldIdHeader   = "X-Field-Id"
	FieldNameHeader = "X-Field-Name"
	// Identifies the running node process, so that the hub can tell two laptops that both claim the same field apart.
	NodeInstanceHeader = "X-Node-Instance"

	// Websocket message types sent from the hub to a node.
	HelloMessage       = "hello"
	VersionsMessage    = "versions"
	DataChangedMessage = "dataChanged"
	FieldTeamsMessage  = "fieldTeams"
	// Tells nodes started with -simulate to start or stop simulating matches.
	SimulateMessage = "simulate"

	// Websocket message types sent from a node to the hub.
	StatusMessage = "status"
)

// Hello is sent by the hub to a node as soon as it connects.
type Hello struct {
	Approved     bool
	ServerTimeMs int64
	EventName    string
}

// DataChanged tells a node that a mirrored collection has a new version.
type DataChanged struct {
	Collection string
	Version    int64
}

// FieldTeams describes which teams are loaded on a field and whether its match is underway.
type FieldTeams struct {
	FieldId         int
	TeamIds         []int
	MatchInProgress bool
}

// NodeStatusReport is sent periodically by a node to the hub.
type NodeStatusReport struct {
	FieldId          int
	FieldName        string
	ClockUnixMs      int64
	MatchState       int
	MatchStateName   string
	MatchInProgress  bool
	CurrentMatchId   int
	CurrentMatchName string
	CurrentMatchType string
	MatchTimeSec     int
	TeamIds          []int
	RedScore         int
	BlueScore        int
	OutboxSize       int
	RejectedCount    int
	EarlyLateMessage string
	SyncedVersions   map[string]int64
}

// ResultSubmission carries a committed match result from a node to the hub.
type ResultSubmission struct {
	FieldId     int
	MatchId     int
	PlayNumber  int
	IsEdit      bool
	Match       model.Match
	MatchResult model.MatchResult
}

// IdempotencyKey returns the key identifying this submission.
func (submission *ResultSubmission) IdempotencyKey() string {
	return model.ResultIdempotencyKey(submission.FieldId, submission.MatchId, submission.PlayNumber)
}

// ResultResponse is the hub's reply to a result submission.
type ResultResponse struct {
	Applied        bool
	Duplicate      bool
	Conflict       bool
	Message        string
	Rankings       game.Rankings
	PlayoffMatches []model.Match
}

// ClaimRequest asks the hub for the next match to play (dynamic field assignment), or releases a claimed match.
type ClaimRequest struct {
	FieldId int
	MatchId int
}

// ClaimResponse is the hub's reply to a claim request.
type ClaimResponse struct {
	Match   *model.Match
	Message string
}

// ResultsBundle is exported from a node and imported on the hub to move results by hand when the network is down.
type ResultsBundle struct {
	FieldId     int
	FieldName   string
	ExportedAt  int64
	Submissions []ResultSubmission
}

// SimulateCommand asks nodes running with -simulate to play matches of the given type automatically every IntervalSec
// seconds, or to stop if IntervalSec is zero.
type SimulateCommand struct {
	MatchType   string
	IntervalSec int
}
