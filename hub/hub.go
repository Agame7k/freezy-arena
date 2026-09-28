// Copyright 2026 Team 254. All Rights Reserved.
//
// The hub of a multi-field event: the single source of truth that mirrors event data to its field nodes, receives their
// match results, and hands out matches in dynamic field assignment mode.

package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	scanPeriod              = 500 * time.Millisecond
	nodeStaleAfter          = 5 * time.Second
	clockOffsetWarningMs    = 2000
	maxConflictLogEntries   = 100
	fieldTeamsBroadcastRate = time.Second
	// How long the warning about a second laptop claiming a field that is already connected stays up.
	duplicateWarningPeriod = time.Minute
)

// NodeStatus is the hub's in-memory view of a field node; it is not persisted.
type NodeStatus struct {
	FieldId        int
	FieldName      string
	Connected      bool
	Approved       bool
	RemoteAddr     string
	LastContact    time.Time
	ClockOffsetMs  int64
	ClockWarning   bool
	OutboxSize     int
	Report         NodeStatusReport
	VersionsBehind []string
	// Address of another laptop that recently tried to connect as this field while it was already connected, if any.
	DuplicateFrom string
	DuplicateAt   time.Time
}

type nodeConnection struct {
	fieldId    int
	instance   string
	remoteAddr string
	conn       *gorillawebsocket.Conn
	writeMutex sync.Mutex
}

func (connection *nodeConnection) send(messageType string, data any) error {
	connection.writeMutex.Lock()
	defer connection.writeMutex.Unlock()
	_ = connection.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return connection.conn.WriteJSON(websocket.Message{Type: messageType, Data: data})
}

type collectionState struct {
	hash     [32]byte
	version  int64
	snapshot []byte
}

// Hub holds the hub-side state of a multi-field event.
type Hub struct {
	arena            *field.Arena
	AutoApproveNodes bool
	StatusNotifier   *websocket.Notifier

	// Serializes result submissions, claims and releases so that they are applied one at a time.
	resultMutex sync.Mutex

	mutex       sync.Mutex
	collections map[string]*collectionState
	connections map[int]*nodeConnection
	statuses    map[int]*NodeStatus
	conflicts   []string
	pokeChan    chan struct{}
	stopChan    chan struct{}
	stopped     bool
	lastVersion int64
}

// New creates the hub and starts its background loops.
func New(arena *field.Arena) *Hub {
	hub := newHub(arena)
	go hub.run()
	return hub
}

func newHub(arena *field.Arena) *Hub {
	hub := &Hub{
		arena:       arena,
		collections: make(map[string]*collectionState),
		connections: make(map[int]*nodeConnection),
		statuses:    make(map[int]*NodeStatus),
		pokeChan:    make(chan struct{}, 1),
		stopChan:    make(chan struct{}),
	}
	hub.StatusNotifier = websocket.NewNotifier("hubStatus", hub.generateStatusMessage)
	arena.OnDataChanged = hub.Poke
	return hub
}

// Stop shuts down the hub's background loops and disconnects every node (e.g. when this machine stops being the hub).
func (hub *Hub) Stop() {
	hub.mutex.Lock()
	if hub.stopped {
		hub.mutex.Unlock()
		return
	}
	hub.stopped = true
	connections := hub.connections
	hub.connections = make(map[int]*nodeConnection)
	hub.mutex.Unlock()
	close(hub.stopChan)
	for _, connection := range connections {
		_ = connection.conn.Close()
	}
	if hub.arena.OnDataChanged != nil {
		hub.arena.OnDataChanged = nil
	}
}

// Poke asks the hub to check for changed collections right away.
func (hub *Hub) Poke() {
	select {
	case hub.pokeChan <- struct{}{}:
	default:
	}
}

func (hub *Hub) run() {
	scanTicker := time.NewTicker(scanPeriod)
	fieldTeamsTicker := time.NewTicker(fieldTeamsBroadcastRate)
	for {
		select {
		case <-hub.stopChan:
			scanTicker.Stop()
			fieldTeamsTicker.Stop()
			return
		case <-scanTicker.C:
		case <-hub.pokeChan:
		case <-fieldTeamsTicker.C:
			hub.broadcast(FieldTeamsMessage, hub.FieldTeams())
			hub.refreshStaleness()
			continue
		}
		hub.scan()
	}
}

// scan snapshots every mirrored collection and bumps the version of any that changed, notifying all nodes.
func (hub *Hub) scan() {
	changed := hub.scanCollections()
	for _, dataChanged := range changed {
		hub.broadcast(DataChangedMessage, dataChanged)
	}
	if len(changed) > 0 {
		hub.StatusNotifier.Notify()
	}
}

func (hub *Hub) scanCollections() []DataChanged {
	var changed []DataChanged
	for _, collection := range append(append([]string{}, model.MirroredCollections...), model.EventSettingsSharedKey) {
		snapshot, err := hub.buildSnapshot(collection)
		if err != nil {
			log.Printf("Hub failed to snapshot %s: %v", collection, err)
			continue
		}
		hash := sha256.Sum256(snapshot)

		hub.mutex.Lock()
		state, ok := hub.collections[collection]
		if !ok || state.hash != hash {
			version := hub.nextVersion()
			hub.collections[collection] = &collectionState{hash: hash, version: version, snapshot: snapshot}
			changed = append(changed, DataChanged{Collection: collection, Version: version})
		}
		hub.mutex.Unlock()
	}
	return changed
}

// Versions are based on the clock so that they keep increasing across hub restarts. Must be called with the mutex.
func (hub *Hub) nextVersion() int64 {
	version := time.Now().UnixNano()
	if version <= hub.lastVersion {
		version = hub.lastVersion + 1
	}
	hub.lastVersion = version
	return version
}

func (hub *Hub) buildSnapshot(collection string) ([]byte, error) {
	if collection == model.EventSettingsSharedKey {
		// Only the event-wide settings are shared; secrets and hardware addresses stay on the hub.
		var shared model.EventSettings
		model.CopySharedEventSettings(&shared, hub.arena.EventSettings)
		return json.Marshal(shared)
	}
	return hub.arena.Database.SnapshotCollection(collection)
}

// Snapshot returns the latest version and snapshot of the given collection.
func (hub *Hub) Snapshot(collection string) (int64, []byte, error) {
	hub.mutex.Lock()
	state, ok := hub.collections[collection]
	hub.mutex.Unlock()
	if !ok {
		hub.scan()
		hub.mutex.Lock()
		state, ok = hub.collections[collection]
		hub.mutex.Unlock()
		if !ok {
			return 0, nil, fmt.Errorf("unknown collection %q", collection)
		}
	}
	return state.version, state.snapshot, nil
}

// Versions returns the current version of every collection.
func (hub *Hub) Versions() map[string]int64 {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	versions := make(map[string]int64, len(hub.collections))
	for collection, state := range hub.collections {
		versions[collection] = state.version
	}
	return versions
}

func (hub *Hub) broadcast(messageType string, data any) {
	hub.mutex.Lock()
	connections := make([]*nodeConnection, 0, len(hub.connections))
	for _, connection := range hub.connections {
		connections = append(connections, connection)
	}
	hub.mutex.Unlock()
	for _, connection := range connections {
		if !hub.isApproved(connection.fieldId) {
			continue
		}
		if err := connection.send(messageType, data); err != nil {
			log.Printf("Hub failed to send %s to field %d: %v", messageType, connection.fieldId, err)
		}
	}
}

// authenticate checks the shared secret and field ID of a node request, registering the node on first contact. It
// returns the node's field ID and whether it has been approved by the hub admin. When it refuses the request, the error
// is a *SubmissionError carrying the HTTP status to answer with.
func (hub *Hub) authenticate(r *http.Request) (int, bool, error) {
	secret := hub.arena.EventSettings.HubSharedSecret
	if secret == "" {
		return 0, false, &SubmissionError{http.StatusUnauthorized, "the hub has no shared secret configured"}
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(SecretHeader)), []byte(secret)) != 1 {
		return 0, false, &SubmissionError{http.StatusUnauthorized, "invalid shared secret"}
	}
	fieldId, err := strconv.Atoi(r.Header.Get(FieldIdHeader))
	if err != nil || fieldId < 1 || fieldId > model.MaxFieldId {
		return 0, false, &SubmissionError{
			http.StatusBadRequest,
			fmt.Sprintf("invalid field ID %q; it must be 1 or 2", r.Header.Get(FieldIdHeader)),
		}
	}
	if err = hub.checkNodeInstance(fieldId, r.Header.Get(NodeInstanceHeader), r.RemoteAddr); err != nil {
		return 0, false, err
	}

	registration, err := hub.arena.Database.GetNodeRegistrationById(fieldId)
	if err != nil {
		return 0, false, err
	}
	if registration == nil {
		registration = &model.NodeRegistration{
			Id:         fieldId,
			FieldName:  r.Header.Get(FieldNameHeader),
			Approved:   hub.AutoApproveNodes,
			RemoteAddr: r.RemoteAddr,
			FirstSeen:  time.Now(),
		}
		if err = hub.arena.Database.CreateNodeRegistration(registration); err != nil {
			return 0, false, err
		}
		log.Printf("Hub registered new node for field %d from %s (approved: %v)", fieldId, r.RemoteAddr,
			registration.Approved)
		hub.StatusNotifier.Notify()
	}
	return fieldId, registration.Approved, nil
}

// checkNodeInstance refuses a request from a node process other than the one that is connected as the given field while
// that connection is alive, since it means that two laptops are set up as the same field. Otherwise they would take the
// connection from each other every few seconds, and both could play (and report) that field's matches.
func (hub *Hub) checkNodeInstance(fieldId int, instance, remoteAddr string) error {
	if instance == "" {
		// A node that doesn't identify itself can't be told apart from the connected one.
		return nil
	}
	hub.mutex.Lock()
	connection, ok := hub.connections[fieldId]
	if !ok || connection.instance == "" || connection.instance == instance {
		hub.mutex.Unlock()
		return nil
	}
	status := hub.statusFor(fieldId)
	if time.Since(status.LastContact) > nodeStaleAfter {
		// The connected laptop has gone quiet, so let this one take over (e.g. a replacement laptop).
		hub.mutex.Unlock()
		return nil
	}
	from := hostOf(remoteAddr)
	connectedFrom := hostOf(connection.remoteAddr)
	newWarning := status.DuplicateFrom != from || time.Since(status.DuplicateAt) > duplicateWarningPeriod
	status.DuplicateFrom = from
	status.DuplicateAt = time.Now()
	hub.mutex.Unlock()

	if newWarning {
		hub.logConflict(
			fmt.Sprintf(
				"a second laptop (%s) tried to connect as field %d, which is already connected from %s; it was refused",
				from, fieldId, connectedFrom,
			),
		)
	}
	return &SubmissionError{
		http.StatusLocked,
		fmt.Sprintf(
			"field %d is already connected to the hub from %s; each field laptop needs its own field ID",
			fieldId, connectedFrom,
		),
	}
}

// Returns the host part of a network address such as "10.0.0.5:52311".
func hostOf(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

func (hub *Hub) isApproved(fieldId int) bool {
	registration, err := hub.arena.Database.GetNodeRegistrationById(fieldId)
	return err == nil && registration != nil && registration.Approved
}

// ApproveNode marks the given node as approved (or revokes its approval).
func (hub *Hub) ApproveNode(fieldId int, approved bool) error {
	registration, err := hub.arena.Database.GetNodeRegistrationById(fieldId)
	if err != nil {
		return err
	}
	if registration == nil {
		return fmt.Errorf("no node is registered for field %d", fieldId)
	}
	registration.Approved = approved
	if err = hub.arena.Database.UpdateNodeRegistration(registration); err != nil {
		return err
	}

	hub.mutex.Lock()
	connection := hub.connections[fieldId]
	if status, ok := hub.statuses[fieldId]; ok {
		status.Approved = approved
	}
	hub.mutex.Unlock()
	if connection != nil {
		_ = connection.send(HelloMessage, hub.hello(approved))
		if approved {
			_ = connection.send(VersionsMessage, hub.Versions())
		}
	}
	hub.StatusNotifier.Notify()
	return nil
}

// ForgetNode removes a node's registration so that it must be approved again.
func (hub *Hub) ForgetNode(fieldId int) error {
	if err := hub.arena.Database.DeleteNodeRegistration(fieldId); err != nil {
		return err
	}
	hub.mutex.Lock()
	delete(hub.statuses, fieldId)
	hub.mutex.Unlock()
	hub.StatusNotifier.Notify()
	return nil
}

// ForceResync tells every node to re-fetch every collection.
func (hub *Hub) ForceResync() {
	hub.mutex.Lock()
	for _, state := range hub.collections {
		state.version = hub.nextVersion()
	}
	hub.mutex.Unlock()
	hub.broadcast(VersionsMessage, hub.Versions())
	hub.StatusNotifier.Notify()
}

func (hub *Hub) hello(approved bool) Hello {
	return Hello{Approved: approved, ServerTimeMs: time.Now().UnixMilli(), EventName: hub.arena.EventSettings.Name}
}

// handleNodeConnection services a node's websocket until it disconnects.
func (hub *Hub) handleNodeConnection(
	fieldId int, approved bool, conn *gorillawebsocket.Conn, remoteAddr, instance string,
) {
	connection := &nodeConnection{fieldId: fieldId, instance: instance, remoteAddr: remoteAddr, conn: conn}
	hub.mutex.Lock()
	if previous, ok := hub.connections[fieldId]; ok {
		_ = previous.conn.Close()
	}
	hub.connections[fieldId] = connection
	status := hub.statusFor(fieldId)
	status.Connected = true
	status.Approved = approved
	status.RemoteAddr = remoteAddr
	status.LastContact = time.Now()
	hub.mutex.Unlock()
	hub.StatusNotifier.Notify()
	log.Printf("Field %d node connected to the hub from %s", fieldId, remoteAddr)

	defer func() {
		hub.mutex.Lock()
		if hub.connections[fieldId] == connection {
			delete(hub.connections, fieldId)
			hub.statusFor(fieldId).Connected = false
		}
		hub.mutex.Unlock()
		_ = conn.Close()
		hub.StatusNotifier.Notify()
		log.Printf("Field %d node disconnected from the hub", fieldId)
	}()

	if err := connection.send(HelloMessage, hub.hello(approved)); err != nil {
		return
	}
	if approved {
		if err := connection.send(VersionsMessage, hub.Versions()); err != nil {
			return
		}
	}

	for {
		var message struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		if message.Type == StatusMessage {
			var report NodeStatusReport
			if err := json.Unmarshal(message.Data, &report); err == nil {
				hub.recordStatus(fieldId, report)
			}
		}
	}
}

// Must be called with the mutex held.
func (hub *Hub) statusFor(fieldId int) *NodeStatus {
	status, ok := hub.statuses[fieldId]
	if !ok {
		status = &NodeStatus{FieldId: fieldId}
		hub.statuses[fieldId] = status
	}
	return status
}

func (hub *Hub) recordStatus(fieldId int, report NodeStatusReport) {
	now := time.Now()
	versions := hub.Versions()
	hub.mutex.Lock()
	status := hub.statusFor(fieldId)
	status.Report = report
	status.FieldName = report.FieldName
	status.LastContact = now
	status.OutboxSize = report.OutboxSize
	status.ClockOffsetMs = report.ClockUnixMs - now.UnixMilli()
	status.ClockWarning = status.ClockOffsetMs > clockOffsetWarningMs || status.ClockOffsetMs < -clockOffsetWarningMs
	status.VersionsBehind = nil
	for collection, version := range versions {
		if report.SyncedVersions[collection] != version {
			status.VersionsBehind = append(status.VersionsBehind, collection)
		}
	}
	sort.Strings(status.VersionsBehind)
	hub.mutex.Unlock()
	hub.StatusNotifier.Notify()
}

func (hub *Hub) refreshStaleness() {
	changed := false
	hub.mutex.Lock()
	for _, status := range hub.statuses {
		if status.Connected && time.Since(status.LastContact) > nodeStaleAfter {
			changed = true
		}
	}
	hub.mutex.Unlock()
	if changed {
		hub.StatusNotifier.Notify()
	}
}

// NodeStatuses returns the status of every node that has registered with the hub, ordered by field ID.
func (hub *Hub) NodeStatuses() []NodeStatus {
	registrations, _ := hub.arena.Database.GetAllNodeRegistrations()
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	for _, registration := range registrations {
		status := hub.statusFor(registration.Id)
		status.Approved = registration.Approved
		if status.FieldName == "" {
			status.FieldName = registration.FieldName
		}
		if status.RemoteAddr == "" {
			status.RemoteAddr = registration.RemoteAddr
		}
	}
	statuses := make([]NodeStatus, 0, len(hub.statuses))
	for _, status := range hub.statuses {
		statusCopy := *status
		if statusCopy.Connected && time.Since(statusCopy.LastContact) > nodeStaleAfter {
			statusCopy.Connected = false
		}
		if time.Since(statusCopy.DuplicateAt) > duplicateWarningPeriod {
			statusCopy.DuplicateFrom = ""
		}
		statuses = append(statuses, statusCopy)
	}
	sort.Slice(
		statuses,
		func(i, j int) bool {
			return statuses[i].FieldId < statuses[j].FieldId
		},
	)
	return statuses
}

// FieldTeams returns the teams that each connected field currently has loaded.
func (hub *Hub) FieldTeams() []FieldTeams {
	var fieldTeams []FieldTeams
	for _, status := range hub.NodeStatuses() {
		if !status.Connected {
			continue
		}
		fieldTeams = append(
			fieldTeams,
			FieldTeams{
				FieldId:         status.FieldId,
				TeamIds:         status.Report.TeamIds,
				MatchInProgress: status.Report.MatchInProgress,
			},
		)
	}
	return fieldTeams
}

// logConflict records a disagreement between the hub and a node; the hub's data always wins.
func (hub *Hub) logConflict(message string) {
	log.Printf("Hub conflict: %s", message)
	hub.mutex.Lock()
	hub.conflicts = append(hub.conflicts, time.Now().Format("15:04:05")+" "+message)
	if len(hub.conflicts) > maxConflictLogEntries {
		hub.conflicts = hub.conflicts[len(hub.conflicts)-maxConflictLogEntries:]
	}
	hub.mutex.Unlock()
	hub.StatusNotifier.Notify()
}

// Conflicts returns the logged conflicts, most recent last.
func (hub *Hub) Conflicts() []string {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	return append([]string(nil), hub.conflicts...)
}

func (hub *Hub) generateStatusMessage() any {
	return struct {
		Nodes     []NodeStatus
		Versions  map[string]int64
		Conflicts []string
		Fields    []FieldOverview
	}{hub.NodeStatuses(), hub.Versions(), hub.Conflicts(), hub.FieldOverviews()}
}

// BroadcastSimulate sends a simulation command to every approved node.
func (hub *Hub) BroadcastSimulate(command SimulateCommand) {
	hub.broadcast(SimulateMessage, command)
}
