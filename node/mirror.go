// Copyright 2026 Team 254. All Rights Reserved.
//
// Keeps the node's local database a read-only mirror of the hub's event data.

package node

import (
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/model"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// syncLoop fetches any collection whose hub version differs from the version last applied locally.
func (node *Node) syncLoop() {
	poll := time.NewTicker(versionPollPeriod)
	defer poll.Stop()
	for {
		select {
		case <-node.syncChan:
		case <-poll.C:
			node.pollVersions()
		case <-node.stopChan:
			return
		}
		if err := node.syncPending(); err != nil {
			log.Printf("Failed to sync from the hub: %v", err)
			node.setError(err)
			// Try again shortly.
			go func() {
				time.Sleep(2 * time.Second)
				node.kickSync()
			}()
		}
	}
}

// pollVersions fetches the collection versions over HTTP, as a fallback in case a websocket notification was missed.
func (node *Node) pollVersions() {
	node.mutex.Lock()
	connected, approved := node.connected, node.approved
	node.mutex.Unlock()
	if !connected || !approved {
		return
	}
	var versions map[string]int64
	if err := node.request(http.MethodGet, "/api/hub/versions", nil, resultTimeout, &versions); err != nil {
		return
	}
	node.mutex.Lock()
	for collection, version := range versions {
		node.hubVersions[collection] = version
	}
	node.mutex.Unlock()
}

// syncPending applies every collection that is out of date, in dependency order.
func (node *Node) syncPending() error {
	node.mutex.Lock()
	var pending []string
	order := append(append([]string{}, model.EventSettingsSharedKey), model.MirroredCollections...)
	for _, collection := range order {
		if version, ok := node.hubVersions[collection]; ok && node.syncedVersions[collection] != version {
			pending = append(pending, collection)
		}
	}
	node.mutex.Unlock()
	if len(pending) == 0 {
		return nil
	}

	changed := make(map[string]bool)
	for _, collection := range pending {
		version, snapshot, err := node.fetchSnapshot(collection)
		if err != nil {
			return err
		}
		applied, err := node.applySnapshot(collection, snapshot)
		if err != nil {
			return fmt.Errorf("failed to apply %s from the hub: %v", collection, err)
		}
		if !applied {
			// Deferred until the field is idle; retry later.
			go func() {
				time.Sleep(2 * time.Second)
				node.kickSync()
			}()
			continue
		}
		changed[collection] = true
		node.mutex.Lock()
		node.syncedVersions[collection] = version
		node.mutex.Unlock()
	}
	node.afterMirrorUpdate(changed)
	return nil
}

func (node *Node) fetchSnapshot(collection string) (int64, []byte, error) {
	requestUrl, err := node.hubUrl("/api/hub/snapshot/"+collection, false)
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Header = node.authHeaders()
	response, err := node.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}
	if response.StatusCode != http.StatusOK {
		return 0, nil, &hubError{statusCode: response.StatusCode, message: string(body)}
	}
	version, _ := strconv.ParseInt(response.Header.Get("X-Collection-Version"), 10, 64)
	return version, body, nil
}

// applySnapshot replaces a local collection with the hub's copy, keeping anything that is local to this field. It
// returns false if the update had to be deferred.
func (node *Node) applySnapshot(collection string, snapshot []byte) (bool, error) {
	database := node.arena.Database
	switch collection {
	case model.EventSettingsSharedKey:
		if !node.fieldIsIdle() {
			return false, nil
		}
		var shared model.EventSettings
		if err := json.Unmarshal(snapshot, &shared); err != nil {
			return false, err
		}
		settings, err := database.GetEventSettings()
		if err != nil {
			return false, err
		}
		before, _ := json.Marshal(settings)
		model.CopySharedEventSettings(settings, &shared)
		after, _ := json.Marshal(settings)
		if string(before) == string(after) {
			return true, nil
		}
		if err = database.UpdateEventSettings(settings); err != nil {
			return false, err
		}
		return true, node.arena.LoadSettings()
	case model.TeamsCollection:
		merged, err := mergeTeams(database, snapshot)
		if err != nil {
			return false, err
		}
		return true, database.ReplaceCollection(collection, merged)
	default:
		return true, database.ReplaceCollection(collection, snapshot)
	}
}

// mergeTeams keeps the fields of each team that are local to this field (connection history, FTA notes and the
// yellow card display state) when replacing the team list with the hub's copy.
func mergeTeams(database *model.Database, snapshot []byte) ([]byte, error) {
	var hubTeams []model.Team
	if err := json.Unmarshal(snapshot, &hubTeams); err != nil {
		return nil, err
	}
	localTeams, err := database.GetAllTeams()
	if err != nil {
		return nil, err
	}
	localById := make(map[int]model.Team, len(localTeams))
	for _, team := range localTeams {
		localById[team.Id] = team
	}
	for i, team := range hubTeams {
		if local, ok := localById[team.Id]; ok {
			hubTeams[i].HasConnected = local.HasConnected
			hubTeams[i].FtaNotes = local.FtaNotes
			hubTeams[i].YellowCard = local.YellowCard
		}
	}
	return json.Marshal(hubTeams)
}

// afterMirrorUpdate re-applies results that haven't reached the hub yet and refreshes in-memory state and displays.
func (node *Node) afterMirrorUpdate(changed map[string]bool) {
	if len(changed) == 0 {
		return
	}
	arena := node.arena
	if changed[model.MatchesCollection] || changed[model.MatchResultsCollection] {
		if err := node.reapplyOutbox(); err != nil {
			log.Printf("Failed to re-apply queued results after mirror update: %v", err)
		}
		node.refreshLoadedMatch()
	}
	if changed[model.MatchesCollection] {
		node.loadFirstMatchIfNewlyAvailable()
	}
	if changed[model.EventSettingsSharedKey] || changed[model.ConferencesCollection] ||
		changed[model.AlliancesCollection] || changed[model.MatchesCollection] {
		if err := arena.RebuildPlayoffTournamentReadOnly(); err != nil {
			log.Printf("Failed to rebuild the playoff tournament after mirror update: %v", err)
		}
	}
	arena.MatchListNotifier.Notify()
	if changed[model.RankingsCollection] || changed[model.MatchesCollection] || changed[model.AlliancesCollection] {
		// Bracket and rankings displays refresh themselves when a score is posted.
		arena.ScorePostedNotifier.Notify()
	}
	arena.MultiFieldStatusNotifier.Notify()
}

// reapplyOutbox writes the results that are still waiting to be delivered back into the local database, since a mirror
// update from the hub will not include them yet.
func (node *Node) reapplyOutbox() error {
	database := node.arena.Database
	entries, err := database.GetAllSyncOutboxEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Rejected {
			// The hub refused this result, so its copy of the match stands.
			continue
		}
		match, err := database.GetMatchById(entry.MatchId)
		if err != nil {
			return err
		}
		if match == nil {
			continue
		}
		existingResult, err := database.GetMatchResultForMatch(entry.MatchId)
		if err != nil {
			return err
		}
		if existingResult == nil || existingResult.PlayNumber < entry.PlayNumber {
			matchResult := entry.MatchResult
			matchResult.Id = 0
			if err = database.CreateMatchResult(&matchResult); err != nil {
				return err
			}
		}
		updated := entry.Match
		if err = database.UpdateMatch(&updated); err != nil {
			return err
		}
	}
	return nil
}

// fieldIsIdle returns true if no match is being played or awaiting its result on this field.
func (node *Node) fieldIsIdle() bool {
	switch node.arena.MatchState {
	case field.PreMatch, field.TimeoutActive, field.PostTimeout:
		return true
	}
	return false
}

// SyncAll fetches every collection from the hub right away, regardless of version.
func (node *Node) SyncAll() error {
	var versions map[string]int64
	if err := node.request(http.MethodGet, "/api/hub/versions", nil, resultTimeout, &versions); err != nil {
		return err
	}
	node.mutex.Lock()
	for collection, version := range versions {
		node.hubVersions[collection] = version
	}
	node.syncedVersions = make(map[string]int64)
	node.mutex.Unlock()
	return node.syncPending()
}

// loadFirstMatchIfNewlyAvailable loads this field's first playable match when the hub has just given it matches to play
// (e.g. the schedule was generated or alliance selection finished) and the field is idle on the test match, so that the
// operator doesn't have to find it. It does nothing if the field already had matches to play, so as not to interrupt
// a test match that the operator loaded deliberately.
func (node *Node) loadFirstMatchIfNewlyAvailable() {
	arena := node.arena
	var first *model.Match
	playable := 0
	for _, matchType := range []model.MatchType{model.Qualification, model.Playoff} {
		matches, err := arena.Database.GetMatchesByType(matchType, false)
		if err != nil {
			return
		}
		for i, match := range matches {
			if match.FieldId != arena.EventSettings.FieldId || match.IsComplete() ||
				match.Type == model.Playoff && (match.PlayoffRedAlliance == 0 || match.PlayoffBlueAlliance == 0) {
				continue
			}
			playable++
			if first == nil {
				first = &matches[i]
			}
		}
		if first != nil {
			// Finish the qualifications before looking at the playoffs.
			break
		}
	}

	node.mutex.Lock()
	previouslyPlayable := node.playableMatches
	node.playableMatches = playable
	node.mutex.Unlock()
	if first == nil || previouslyPlayable > 0 || arena.MatchState != field.PreMatch ||
		arena.CurrentMatch == nil || arena.CurrentMatch.Type != model.Test {
		return
	}
	if err := arena.LoadMatch(first); err != nil {
		log.Printf("Couldn't load %s automatically: %v", first.ShortName, err)
		return
	}
	log.Printf("Loaded %s, the first match for this field from the hub", first.ShortName)
	arena.MatchListNotifier.Notify()
}

// refreshLoadedMatch keeps the loaded match in sync with the mirror unless it is being played, reloading it if the
// hub has changed its teams (e.g. a playoff match whose alliances were decided on the other field).
func (node *Node) refreshLoadedMatch() {
	arena := node.arena
	current := arena.CurrentMatch
	if current == nil || current.Type == model.Test || arena.MatchState != field.PreMatch {
		return
	}
	match, err := arena.Database.GetMatchById(current.Id)
	if err != nil || match == nil {
		return
	}
	if match.FieldId != 0 && match.FieldId != arena.EventSettings.FieldId {
		// (An unassigned match may just be a mirror update that predates this field's claim, so only a move to the
		// other field counts.)
		// The hub has moved the match to another field; don't leave it staged here.
		log.Printf("%s was moved to field %d by the hub; loading the next match", match.ShortName, match.FieldId)
		if err = arena.LoadNextMatch(false); err != nil {
			log.Printf("Failed to load the next match: %v", err)
		}
		return
	}
	if !match.IsLineupEqual(current.Red1, current.Red2, current.Red3, current.Blue1, current.Blue2, current.Blue3) ||
		match.PlayoffRedAlliance != current.PlayoffRedAlliance ||
		match.PlayoffBlueAlliance != current.PlayoffBlueAlliance {
		if err = arena.LoadMatch(match); err != nil {
			log.Printf("Failed to reload %s after the hub updated it: %v", match.ShortName, err)
		}
		return
	}
	current.Status = match.Status
	current.FieldId = match.FieldId
	current.Time = match.Time
}
