// Copyright 2026 Team 254. All Rights Reserved.
//
// HTTP and websocket endpoints that field nodes use to talk to the hub. All of them require the shared secret and, apart
// from the websocket (which is how a new node first registers), an approved node.

package hub

import (
	"encoding/json"
	"errors"
	"github.com/Team254/cheesy-arena/model"
	gorillawebsocket "github.com/gorilla/websocket"
	"log"
	"net/http"
	"strconv"
)

var nodeUpgrader = gorillawebsocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

// NodeWebsocketHandler accepts the persistent websocket connection from a node.
func (hub *Hub) NodeWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	fieldId, approved, err := hub.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), errorStatus(err))
		return
	}
	conn, err := nodeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade node websocket: %v", err)
		return
	}
	hub.handleNodeConnection(fieldId, approved, conn, r.RemoteAddr, r.Header.Get(NodeInstanceHeader))
}

// SnapshotHandler returns the latest snapshot of a mirrored collection, with its version in a header.
func (hub *Hub) SnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := hub.authorizeApproved(w, r); !ok {
		return
	}
	version, snapshot, err := hub.Snapshot(r.PathValue("collection"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Collection-Version", itoa64(version))
	_, _ = w.Write(snapshot)
}

// VersionsHandler returns the current version of every collection.
func (hub *Hub) VersionsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := hub.authorizeApproved(w, r); !ok {
		return
	}
	writeJson(w, http.StatusOK, hub.Versions())
}

// ResultsHandler receives a committed result from a node.
func (hub *Hub) ResultsHandler(w http.ResponseWriter, r *http.Request) {
	fieldId, ok := hub.authorizeApproved(w, r)
	if !ok {
		return
	}
	var submission ResultSubmission
	if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
		http.Error(w, "invalid result submission: "+err.Error(), http.StatusBadRequest)
		return
	}
	if submission.FieldId != fieldId {
		http.Error(w, "field ID does not match the authenticated node", http.StatusForbidden)
		return
	}
	response, err := hub.ApplySubmission(&submission)
	if err != nil {
		http.Error(w, err.Error(), errorStatus(err))
		return
	}
	writeJson(w, http.StatusOK, response)
}

// ClaimNextHandler hands the next suitable match to a node (dynamic field assignment).
func (hub *Hub) ClaimNextHandler(w http.ResponseWriter, r *http.Request) {
	fieldId, ok := hub.authorizeApproved(w, r)
	if !ok {
		return
	}
	if hub.arena.EventSettings.QualFieldAssignmentMode != model.DynamicFieldAssignment {
		http.Error(w, "the hub is not using dynamic field assignment", http.StatusConflict)
		return
	}
	match, message, err := hub.ClaimNextMatch(fieldId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJson(w, http.StatusOK, ClaimResponse{Match: match, Message: message})
}

// ReleaseHandler gives a claimed match back to the hub.
func (hub *Hub) ReleaseHandler(w http.ResponseWriter, r *http.Request) {
	fieldId, ok := hub.authorizeApproved(w, r)
	if !ok {
		return
	}
	var request ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid release request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := hub.ReleaseMatch(fieldId, request.MatchId); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJson(w, http.StatusOK, ClaimResponse{})
}

func (hub *Hub) authorizeApproved(w http.ResponseWriter, r *http.Request) (int, bool) {
	fieldId, approved, err := hub.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), errorStatus(err))
		return 0, false
	}
	if !approved {
		http.Error(w, "this node has not been approved on the hub yet", http.StatusForbidden)
		return 0, false
	}
	return fieldId, true
}

// Returns the HTTP status for an error from authenticating or applying a node request.
func errorStatus(err error) int {
	var submissionError *SubmissionError
	if errors.As(err, &submissionError) {
		return submissionError.StatusCode
	}
	return http.StatusInternalServerError
}

func writeJson(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to write JSON response: %v", err)
	}
}

func itoa64(value int64) string {
	return strconv.FormatInt(value, 10)
}
