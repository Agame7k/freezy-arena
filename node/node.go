// Copyright 2026 Team 254. All Rights Reserved.
//
// The field node side of a multi-field event: keeps a persistent connection to the hub, mirrors the hub's event data
// into the local database, and delivers committed match results to the hub through a persistent outbox.

package node

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	resultTimeout      = 3 * time.Second
	snapshotTimeout    = 15 * time.Second
	statusPeriod       = time.Second
	outboxRetryPeriod  = 3 * time.Second
	minReconnectDelay  = time.Second
	maxReconnectDelay  = 30 * time.Second
	versionPollPeriod  = 10 * time.Second
	mirrorStaleTimeout = 5 * time.Second
)

// Node is the hub client running on a field node.
type Node struct {
	arena  *field.Arena
	client *http.Client
	// Random identifier of this node process, sent with every request so that the hub can refuse a second laptop that
	// is set up as the same field.
	instanceId string

	mutex     sync.Mutex
	connected bool
	approved  bool
	// True if the last connection attempt reached the hub but was refused (e.g. wrong secret), as opposed to not
	// reaching it at all.
	refused        bool
	lastError      string
	lastContact    time.Time
	hubVersions    map[string]int64
	syncedVersions map[string]int64
	fieldTeams     []hub.FieldTeams
	// Number of matches this field had left to play after the last mirror update of the matches.
	playableMatches int
	stopped         bool
	conn            *gorillawebsocket.Conn

	// Called when the hub asks this node to start or stop simulating matches (only honored with -simulate).
	OnSimulate func(command hub.SimulateCommand)

	// Serializes outbox delivery so that results reach the hub in order.
	outboxMutex sync.Mutex
	syncChan    chan struct{}
	outboxChan  chan struct{}
	// Cuts short the delay before reconnecting the websocket (e.g. once a result has reached the hub over HTTP).
	reconnectChan chan struct{}
	stopChan      chan struct{}
}

// Start creates the node client, attaches it to the arena and starts its background loops.
func Start(arena *field.Arena) *Node {
	node := newNode(arena)
	arena.NodeLink = node
	go node.connectionLoop()
	go node.syncLoop()
	go node.outboxLoop()
	go node.statusLoop()
	return node
}

func newNode(arena *field.Arena) *Node {
	instanceId := make([]byte, 8)
	_, _ = rand.Read(instanceId)
	return &Node{
		arena:          arena,
		client:         &http.Client{Timeout: snapshotTimeout},
		instanceId:     hex.EncodeToString(instanceId),
		hubVersions:    make(map[string]int64),
		syncedVersions: make(map[string]int64),
		syncChan:       make(chan struct{}, 1),
		outboxChan:     make(chan struct{}, 1),
		reconnectChan:  make(chan struct{}, 1),
		stopChan:       make(chan struct{}),
	}
}

// Stop disconnects from the hub and stops all background loops.
func (node *Node) Stop() {
	node.mutex.Lock()
	if node.stopped {
		node.mutex.Unlock()
		return
	}
	node.stopped = true
	conn := node.conn
	node.mutex.Unlock()
	close(node.stopChan)
	if conn != nil {
		_ = conn.Close()
	}
}

func (node *Node) isStopped() bool {
	node.mutex.Lock()
	defer node.mutex.Unlock()
	return node.stopped
}

// hubUrl builds the URL of the given hub API path from the configured hub address.
func (node *Node) hubUrl(path string, websocketScheme bool) (string, error) {
	address, err := model.NormalizeHubAddress(node.arena.EventSettings.HubAddress)
	if err != nil {
		return "", err
	}
	if address == "" {
		return "", fmt.Errorf("no hub address is configured")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	if websocketScheme {
		if parsed.Scheme == "https" {
			parsed.Scheme = "wss"
		} else {
			parsed.Scheme = "ws"
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func (node *Node) authHeaders() http.Header {
	settings := node.arena.EventSettings
	headers := http.Header{}
	headers.Set(hub.SecretHeader, settings.HubSharedSecret)
	headers.Set(hub.FieldIdHeader, strconv.Itoa(settings.FieldId))
	headers.Set(hub.FieldNameHeader, settings.DisplayFieldName())
	headers.Set(hub.NodeInstanceHeader, node.instanceId)
	return headers
}

// request performs an authenticated request to the hub and decodes a JSON response into the given value.
func (node *Node) request(method, path string, body any, timeout time.Duration, response any) error {
	requestUrl, err := node.hubUrl(path, false)
	if err != nil {
		return err
	}
	var bodyReader io.Reader
	if body != nil {
		bodyJson, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(bodyJson)
	}
	request, err := http.NewRequest(method, requestUrl, bodyReader)
	if err != nil {
		return err
	}
	request.Header = node.authHeaders()
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	httpResponse, err := client.Do(request)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return &hubError{statusCode: httpResponse.StatusCode, message: strings.TrimSpace(string(responseBody))}
	}
	if response != nil {
		return json.Unmarshal(responseBody, response)
	}
	return nil
}

// hubError is an error response from the hub (as opposed to a network failure).
type hubError struct {
	statusCode int
	message    string
}

func (err *hubError) Error() string {
	return fmt.Sprintf("hub returned %d: %s", err.statusCode, err.message)
}

// isPermanent returns true for errors that retrying won't fix (e.g. a result for a match on another field).
func (err *hubError) isPermanent() bool {
	return err.statusCode == http.StatusBadRequest || err.statusCode == http.StatusNotFound ||
		err.statusCode == http.StatusConflict
}

func (node *Node) setError(err error) {
	node.mutex.Lock()
	if err == nil {
		node.lastError = ""
	} else {
		node.lastError = explainHubError(err, node.arena.EventSettings.HubAddress)
	}
	node.mutex.Unlock()
	node.arena.MultiFieldStatusNotifier.Notify()
}

// connectionLoop keeps a websocket open to the hub, reconnecting with increasing delays after a failure.
func (node *Node) connectionLoop() {
	delay := minReconnectDelay
	for !node.isStopped() {
		connectedAt := time.Now()
		err := node.connectOnce()
		node.mutex.Lock()
		node.connected = false
		node.conn = nil
		node.mutex.Unlock()
		node.arena.MultiFieldStatusNotifier.Notify()
		if node.isStopped() {
			return
		}
		if err != nil {
			node.setError(err)
		}
		if time.Since(connectedAt) > maxReconnectDelay {
			// The connection was up for a while, so start again with a short delay.
			delay = minReconnectDelay
		}
		select {
		case <-time.After(delay):
		case <-node.reconnectChan:
			delay = minReconnectDelay / 2
		case <-node.stopChan:
			return
		}
		delay *= 2
		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
	}
}

func (node *Node) connectOnce() error {
	websocketUrl, err := node.hubUrl("/api/hub/node/websocket", true)
	if err != nil {
		return err
	}
	dialer := gorillawebsocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, response, err := dialer.Dial(websocketUrl, node.authHeaders())
	node.mutex.Lock()
	node.refused = err != nil && response != nil
	node.mutex.Unlock()
	if err != nil {
		if response != nil {
			body, _ := io.ReadAll(response.Body)
			return fmt.Errorf("hub rejected connection (%d): %s", response.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("can't reach hub: %v", err)
	}
	node.mutex.Lock()
	node.conn = conn
	node.connected = true
	node.lastContact = time.Now()
	node.mutex.Unlock()
	node.setError(nil)
	log.Printf("Connected to the hub at %s", websocketUrl)

	// Send status reports in the background for the life of the connection.
	done := make(chan struct{})
	defer close(done)
	writeMutex := new(sync.Mutex)
	send := func(messageType string, data any) error {
		writeMutex.Lock()
		defer writeMutex.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(websocket.Message{Type: messageType, Data: data})
	}
	go func() {
		ticker := time.NewTicker(statusPeriod)
		defer ticker.Stop()
		for {
			if err := send(hub.StatusMessage, node.statusReport()); err != nil {
				_ = conn.Close()
				return
			}
			select {
			case <-ticker.C:
			case <-done:
				return
			}
		}
	}()

	for {
		var message struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		if err := conn.ReadJSON(&message); err != nil {
			return fmt.Errorf("lost connection to hub: %v", err)
		}
		node.mutex.Lock()
		node.lastContact = time.Now()
		node.mutex.Unlock()
		node.handleMessage(message.Type, message.Data)
	}
}

func (node *Node) handleMessage(messageType string, data json.RawMessage) {
	switch messageType {
	case hub.HelloMessage:
		var hello hub.Hello
		if json.Unmarshal(data, &hello) == nil {
			node.mutex.Lock()
			node.approved = hello.Approved
			node.mutex.Unlock()
			if !hello.Approved {
				node.setError(fmt.Errorf("waiting for the hub admin to approve this field"))
			} else {
				node.setError(nil)
				node.kickOutbox()
			}
		}
	case hub.VersionsMessage:
		var versions map[string]int64
		if json.Unmarshal(data, &versions) == nil {
			node.mutex.Lock()
			for collection, version := range versions {
				node.hubVersions[collection] = version
			}
			node.mutex.Unlock()
			node.kickSync()
		}
	case hub.DataChangedMessage:
		var dataChanged hub.DataChanged
		if json.Unmarshal(data, &dataChanged) == nil {
			node.mutex.Lock()
			node.hubVersions[dataChanged.Collection] = dataChanged.Version
			node.mutex.Unlock()
			node.kickSync()
		}
	case hub.SimulateMessage:
		var command hub.SimulateCommand
		if json.Unmarshal(data, &command) == nil && node.OnSimulate != nil && node.arena.SimulateHardware {
			go node.OnSimulate(command)
		}
	case hub.FieldTeamsMessage:
		var fieldTeams []hub.FieldTeams
		if json.Unmarshal(data, &fieldTeams) == nil {
			node.mutex.Lock()
			node.fieldTeams = fieldTeams
			node.mutex.Unlock()
		}
	}
}

func (node *Node) kickSync() {
	select {
	case node.syncChan <- struct{}{}:
	default:
	}
}

func (node *Node) kickReconnect() {
	select {
	case node.reconnectChan <- struct{}{}:
	default:
	}
}

func (node *Node) kickOutbox() {
	select {
	case node.outboxChan <- struct{}{}:
	default:
	}
}

// statusLoop periodically refreshes the node's status for its local displays.
func (node *Node) statusLoop() {
	ticker := time.NewTicker(statusPeriod * 2)
	defer ticker.Stop()
	lastStatus := field.NodeLinkStatus{}
	for {
		select {
		case <-ticker.C:
		case <-node.stopChan:
			return
		}
		status := node.Status()
		if status.Connected != lastStatus.Connected || status.OutboxSize != lastStatus.OutboxSize ||
			status.RejectedCount != lastStatus.RejectedCount || status.Refused != lastStatus.Refused ||
			status.LastError != lastStatus.LastError || status.Approved != lastStatus.Approved {
			node.arena.MultiFieldStatusNotifier.Notify()
		}
		lastStatus = status
	}
}

func (node *Node) statusReport() hub.NodeStatusReport {
	arena := node.arena
	pending, rejected := node.outboxCounts()
	report := hub.NodeStatusReport{
		FieldId:          arena.EventSettings.FieldId,
		FieldName:        arena.EventSettings.DisplayFieldName(),
		ClockUnixMs:      time.Now().UnixMilli(),
		MatchState:       int(arena.MatchState),
		MatchStateName:   matchStateName(arena.MatchState),
		MatchInProgress:  matchInProgress(arena.MatchState),
		MatchTimeSec:     int(arena.MatchTimeSec()),
		EarlyLateMessage: arena.EventStatus.EarlyLateMessage,
		RedScore:         arena.RedScoreSummary().Score,
		BlueScore:        arena.BlueScoreSummary().Score,
		OutboxSize:       pending,
		RejectedCount:    rejected,
	}
	if match := arena.CurrentMatch; match != nil {
		report.CurrentMatchId = match.Id
		report.CurrentMatchName = match.LongName
		report.CurrentMatchType = match.Type.String()
		if match.Type != model.Test {
			for _, teamId := range []int{match.Red1, match.Red2, match.Red3, match.Blue1, match.Blue2, match.Blue3} {
				if teamId > 0 {
					report.TeamIds = append(report.TeamIds, teamId)
				}
			}
		}
	}
	node.mutex.Lock()
	report.SyncedVersions = make(map[string]int64, len(node.syncedVersions))
	for collection, version := range node.syncedVersions {
		report.SyncedVersions[collection] = version
	}
	node.mutex.Unlock()
	return report
}

func matchInProgress(state field.MatchState) bool {
	switch state {
	case field.StartMatch, field.AutoPeriod, field.PausePeriod, field.TeleopPeriod, field.PostMatch:
		return true
	}
	return false
}

func matchStateName(state field.MatchState) string {
	switch state {
	case field.PreMatch:
		return "Pre-Match"
	case field.StartMatch, field.AutoPeriod:
		return "Auto"
	case field.PausePeriod:
		return "Pause"
	case field.TeleopPeriod:
		return "Teleop"
	case field.PostMatch:
		return "Post-Match"
	case field.TimeoutActive, field.PostTimeout:
		return "Timeout"
	}
	return ""
}

// Status implements field.NodeLink.
func (node *Node) Status() field.NodeLinkStatus {
	outboxSize, rejectedCount := node.outboxCounts()
	node.mutex.Lock()
	defer node.mutex.Unlock()
	status := field.NodeLinkStatus{
		Connected:      node.connected,
		Approved:       node.approved,
		Refused:        node.refused && !node.connected,
		OutboxSize:     outboxSize,
		RejectedCount:  rejectedCount,
		LastError:      node.lastError,
		LastContact:    node.lastContact,
		FieldTeams:     make(map[int][]int),
		SyncedVersions: make(map[string]int64),
	}
	for collection, version := range node.hubVersions {
		if node.syncedVersions[collection] != version {
			status.MirrorPending = true
		}
	}
	for _, fieldTeams := range node.fieldTeams {
		status.FieldTeams[fieldTeams.FieldId] = fieldTeams.TeamIds
	}
	for collection, version := range node.syncedVersions {
		status.SyncedVersions[collection] = version
	}
	return status
}

// outboxSize returns the number of results waiting to be delivered to the hub.
func (node *Node) outboxSize() int {
	pending, _ := node.outboxCounts()
	return pending
}

// outboxCounts returns the number of results waiting to be delivered and the number that the hub rejected.
func (node *Node) outboxCounts() (int, int) {
	entries, err := node.arena.Database.GetAllSyncOutboxEntries()
	if err != nil {
		return 0, 0
	}
	pending, rejected := 0, 0
	for _, entry := range entries {
		if entry.Rejected {
			rejected++
		} else {
			pending++
		}
	}
	return pending, rejected
}

// CheckTeamsAvailable implements field.NodeLink: it refuses teams whose match on another field is still underway, so
// that two access points never broadcast the same team network.
func (node *Node) CheckTeamsAvailable(teamIds []int) error {
	node.mutex.Lock()
	fieldTeams := node.fieldTeams
	connected := node.connected
	node.mutex.Unlock()
	if !connected {
		return nil
	}
	for _, other := range fieldTeams {
		if other.FieldId == node.arena.EventSettings.FieldId || !other.MatchInProgress {
			continue
		}
		for _, teamId := range teamIds {
			for _, otherTeamId := range other.TeamIds {
				if teamId > 0 && teamId == otherTeamId {
					return fmt.Errorf(
						"team %d is still in a match on field %d; wait for that match to finish", teamId,
						other.FieldId,
					)
				}
			}
		}
	}
	return nil
}

// ClaimNextMatch implements field.NodeLink.
func (node *Node) ClaimNextMatch() (*model.Match, error) {
	var response hub.ClaimResponse
	err := node.request(
		http.MethodPost,
		"/api/hub/claim_next",
		hub.ClaimRequest{FieldId: node.arena.EventSettings.FieldId},
		resultTimeout,
		&response,
	)
	if err != nil {
		return nil, err
	}
	if response.Match == nil && response.Message != "" {
		return nil, fmt.Errorf("%s", response.Message)
	}
	return response.Match, nil
}

// ReleaseMatch implements field.NodeLink.
func (node *Node) ReleaseMatch(matchId int) error {
	err := node.request(
		http.MethodPost,
		"/api/hub/release",
		hub.ClaimRequest{FieldId: node.arena.EventSettings.FieldId, MatchId: matchId},
		resultTimeout,
		nil,
	)
	if err == nil {
		// Mark the match as unassigned locally until the mirror catches up.
		if match, _ := node.arena.Database.GetMatchById(matchId); match != nil && !match.IsComplete() {
			match.FieldId = 0
			_ = node.arena.Database.UpdateMatch(match)
		}
	}
	return err
}

// Reconnect drops the current connection to the hub and connects again right away, e.g. after the hub address or
// shared secret has been changed in the settings.
func (node *Node) Reconnect() {
	node.mutex.Lock()
	conn := node.conn
	node.mutex.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	node.kickReconnect()
	node.kickOutbox()
	node.kickSync()
}
