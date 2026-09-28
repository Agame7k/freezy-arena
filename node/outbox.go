// Copyright 2026 Team 254. All Rights Reserved.
//
// Delivers committed results from the node to the hub, in order and exactly once, surviving hub outages and restarts.

package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/tournament"
	"log"
	"net/http"
	"time"
)

// SubmitResult implements field.NodeLink: it queues the result in the persistent outbox and tries to deliver it (and
// anything queued before it) within a few seconds.
func (node *Node) SubmitResult(
	match *model.Match, matchResult *model.MatchResult, isEdit bool,
) (*field.ResultReceipt, error) {
	entry := model.SyncOutboxEntry{
		FieldId:     node.arena.EventSettings.FieldId,
		MatchId:     match.Id,
		PlayNumber:  matchResult.PlayNumber,
		IsEdit:      isEdit,
		Match:       *match,
		MatchResult: *matchResult,
		CreatedAt:   time.Now(),
	}
	if err := node.arena.Database.CreateSyncOutboxEntry(&entry); err != nil {
		return nil, err
	}

	receipt := &field.ResultReceipt{Queued: true}
	done := make(chan *hub.ResultResponse, 1)
	go func() {
		responses, err := node.deliverOutbox()
		if err != nil {
			log.Printf("Result for %s queued for the hub: %v", match.ShortName, err)
		}
		done <- responses[entry.Id]
	}()
	select {
	case response := <-done:
		if response != nil {
			receipt.Queued = false
			receipt.Rankings = response.Rankings
			receipt.Duplicate = response.Duplicate
		}
	case <-time.After(resultTimeout):
		// Delivery continues in the background; the score is shown without the rank change for now.
	}
	return receipt, nil
}

// outboxLoop retries delivery of queued results periodically.
func (node *Node) outboxLoop() {
	ticker := time.NewTicker(outboxRetryPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-node.outboxChan:
		case <-node.stopChan:
			return
		}
		if node.outboxSize() == 0 {
			continue
		}
		if _, err := node.deliverOutbox(); err != nil {
			node.setError(err)
		}
	}
}

// deliverOutbox sends queued results to the hub in the order in which they were committed, stopping at the first one
// that can't be delivered. It returns the hub's response to each delivered entry, keyed by outbox entry ID.
func (node *Node) deliverOutbox() (map[int]*hub.ResultResponse, error) {
	node.outboxMutex.Lock()
	defer node.outboxMutex.Unlock()

	responses := make(map[int]*hub.ResultResponse)
	entries, err := node.arena.Database.GetAllSyncOutboxEntries()
	if err != nil {
		return responses, err
	}
	for _, entry := range entries {
		if entry.Rejected {
			continue
		}
		submission := submissionFromEntry(&entry)
		var response hub.ResultResponse
		err = node.request(http.MethodPost, "/api/hub/results", submission, resultTimeout, &response)
		if err != nil {
			var responseError *hubError
			if errors.As(err, &responseError) && responseError.isPermanent() {
				// Retrying won't help, so stop blocking the queue, but keep the result so that it isn't lost: it is still
				// counted, exported in results bundles and retried when the operator forces a resync.
				log.Printf("Hub rejected result for %s: %v", entry.Match.ShortName, err)
				entry.Attempts++
				entry.LastError = responseError.message
				entry.Rejected = true
				if err = node.arena.Database.UpdateSyncOutboxEntry(&entry); err != nil {
					return responses, err
				}
				node.setError(fmt.Errorf("hub rejected result for %s: %s", entry.Match.ShortName, responseError.message))
				continue
			}
			entry.Attempts++
			entry.LastError = err.Error()
			_ = node.arena.Database.UpdateSyncOutboxEntry(&entry)
			node.arena.MultiFieldStatusNotifier.Notify()
			return responses, err
		}
		if err = node.arena.Database.DeleteSyncOutboxEntry(entry.Id); err != nil {
			return responses, err
		}
		responses[entry.Id] = &response
		node.applyResponse(&response)
	}
	if len(responses) > 0 {
		node.mutex.Lock()
		connected := node.connected
		node.mutex.Unlock()
		if !connected {
			// The hub is evidently reachable again, so don't wait out the reconnect backoff.
			node.kickReconnect()
		}
	}
	node.arena.MultiFieldStatusNotifier.Notify()
	return responses, nil
}

// RetryRejected puts every result that the hub rejected back in the queue (e.g. after the hub admin has fixed the field
// assignment of its match) and tries to deliver them.
func (node *Node) RetryRejected() error {
	node.outboxMutex.Lock()
	entries, err := node.arena.Database.GetAllSyncOutboxEntries()
	if err == nil {
		for _, entry := range entries {
			if entry.Rejected {
				entry.Rejected = false
				if err = node.arena.Database.UpdateSyncOutboxEntry(&entry); err != nil {
					break
				}
			}
		}
	}
	node.outboxMutex.Unlock()
	if err != nil {
		return err
	}
	node.kickOutbox()
	node.arena.MultiFieldStatusNotifier.Notify()
	return nil
}

// applyResponse writes what the hub returned into the local mirror right away, ahead of the next mirror update.
func (node *Node) applyResponse(response *hub.ResultResponse) {
	database := node.arena.Database
	if len(response.Rankings) > 0 {
		if err := database.ReplaceAllRankings(response.Rankings); err != nil {
			log.Printf("Failed to save rankings from the hub: %v", err)
		}
	}
	for _, match := range response.PlayoffMatches {
		local, err := database.GetMatchById(match.Id)
		if err != nil || local == nil {
			continue
		}
		updated := match
		if err = database.UpdateMatch(&updated); err != nil {
			log.Printf("Failed to save playoff match from the hub: %v", err)
		}
	}
	if len(response.PlayoffMatches) > 0 {
		if err := node.arena.RebuildPlayoffTournamentReadOnly(); err != nil {
			log.Printf("Failed to refresh the playoff tournament: %v", err)
		}
		node.refreshLoadedMatch()
		node.arena.MatchListNotifier.Notify()
	}
	node.kickSync()
}

func submissionFromEntry(entry *model.SyncOutboxEntry) hub.ResultSubmission {
	return hub.ResultSubmission{
		FieldId:     entry.FieldId,
		MatchId:     entry.MatchId,
		PlayNumber:  entry.PlayNumber,
		IsEdit:      entry.IsEdit,
		Match:       entry.Match,
		MatchResult: entry.MatchResult,
	}
}

// ExportBundle builds a results bundle containing every queued result plus the latest result of every completed match
// on this field, for carrying to the hub by hand when the network is down.
func (node *Node) ExportBundle() (*hub.ResultsBundle, error) {
	database := node.arena.Database
	settings := node.arena.EventSettings
	bundle := &hub.ResultsBundle{
		FieldId:    settings.FieldId,
		FieldName:  settings.DisplayFieldName(),
		ExportedAt: time.Now().Unix(),
	}
	included := make(map[string]bool)
	entries, err := database.GetAllSyncOutboxEntries()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		submission := submissionFromEntry(&entry)
		bundle.Submissions = append(bundle.Submissions, submission)
		included[submission.IdempotencyKey()] = true
	}
	for _, matchType := range []model.MatchType{model.Practice, model.Qualification, model.Playoff} {
		matches, err := database.GetMatchesByType(matchType, true)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if match.FieldId != settings.FieldId || !match.IsComplete() {
				continue
			}
			matchResult, err := database.GetMatchResultForMatch(match.Id)
			if err != nil {
				return nil, err
			}
			if matchResult == nil {
				continue
			}
			submission := hub.ResultSubmission{
				FieldId:     settings.FieldId,
				MatchId:     match.Id,
				PlayNumber:  matchResult.PlayNumber,
				Match:       match,
				MatchResult: *matchResult,
			}
			if !included[submission.IdempotencyKey()] {
				bundle.Submissions = append(bundle.Submissions, submission)
				included[submission.IdempotencyKey()] = true
			}
		}
	}
	return bundle, nil
}

// ExportBundleJson returns the results bundle as indented JSON.
func (node *Node) ExportBundleJson() ([]byte, error) {
	bundle, err := node.ExportBundle()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(bundle, "", "  ")
}

// PromoteToStandalone takes a final snapshot from the hub (if it can be reached), switches this machine to the
// standalone role and applies any results that never reached the hub, so that the event can continue on this machine.
func (node *Node) PromoteToStandalone() error {
	if err := node.SyncAll(); err != nil {
		log.Printf("Could not take a final snapshot from the hub before promotion; using the current mirror: %v", err)
	}
	node.Stop()

	arena := node.arena
	arena.NodeLink = nil
	settings, err := arena.Database.GetEventSettings()
	if err != nil {
		return err
	}
	settings.MultiFieldRole = model.StandaloneRole
	if err = arena.Database.UpdateEventSettings(settings); err != nil {
		return err
	}
	if err = arena.LoadSettings(); err != nil {
		return err
	}

	// Results in the outbox are already saved locally but their consequences were left to the hub.
	entries, err := arena.Database.GetAllSyncOutboxEntries()
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		if err = node.reapplyOutbox(); err != nil {
			return err
		}
		if _, err = tournament.CalculateRankings(arena.Database, false); err != nil {
			return err
		}
		if err = arena.UpdatePlayoffTournament(); err != nil {
			return err
		}
	}
	if err = arena.Database.TruncateSyncOutbox(); err != nil {
		return err
	}
	arena.MultiFieldStatusNotifier.Notify()
	arena.MatchListNotifier.Notify()
	return nil
}
