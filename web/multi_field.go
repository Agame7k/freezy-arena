// Copyright 2026 Team 254. All Rights Reserved.
//
// Web routes for multi-field events: the hub's node API and Event Control page, node tools, and match simulation for
// development and dry runs.

package web

import (
	"encoding/json"
	"fmt"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/node"
	"github.com/Team254/cheesy-arena/websocket"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Largest results bundle accepted for import; a whole event's results are well under a megabyte.
const maxResultsBundleBytes = 32 << 20

// SetHub attaches the hub service so that its routes and pages become active.
func (web *Web) SetHub(eventHub *hub.Hub) {
	web.hub = eventHub
}

// SetNode attaches the node client so that its routes and pages become active.
func (web *Web) SetNode(eventNode *node.Node) {
	web.node = eventNode
	// Let the hub's "Simulate the whole event" button drive this field's simulator.
	eventNode.OnSimulate = func(command hub.SimulateCommand) {
		matchType, err := simulationMatchType(command.MatchType)
		if err != nil {
			log.Printf("Ignoring simulate command from the hub: %v", err)
			return
		}
		web.setAutoSimulator(matchType, command.IntervalSec)
	}
}

// Registers the routes for multi-field events.
func (web *Web) addMultiFieldRoutes(mux *http.ServeMux) {
	// Node-facing hub API (authenticated with the shared secret, not the admin password).
	mux.HandleFunc("GET /api/hub/node/websocket", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.NodeWebsocketHandler
	}))
	mux.HandleFunc("GET /api/hub/snapshot/{collection}", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.SnapshotHandler
	}))
	mux.HandleFunc("GET /api/hub/versions", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.VersionsHandler
	}))
	mux.HandleFunc("POST /api/hub/results", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.ResultsHandler
	}))
	mux.HandleFunc("POST /api/hub/claim_next", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.ClaimNextHandler
	}))
	mux.HandleFunc("POST /api/hub/release", web.hubHandler(func(h *hub.Hub) http.HandlerFunc {
		return h.ReleaseHandler
	}))

	// Hub admin pages.
	mux.HandleFunc("GET /event_control", web.eventControlHandler)
	mux.HandleFunc("GET /event_control/websocket", web.eventControlWebsocketHandler)
	mux.HandleFunc("POST /event_control/nodes/{fieldId}/approve", web.eventControlApproveHandler)
	mux.HandleFunc("POST /event_control/nodes/{fieldId}/revoke", web.eventControlRevokeHandler)
	mux.HandleFunc("POST /event_control/nodes/{fieldId}/forget", web.eventControlForgetHandler)
	mux.HandleFunc("POST /event_control/resync", web.eventControlResyncHandler)
	mux.HandleFunc("GET /event_control/checklist", web.eventControlChecklistHandler)
	mux.HandleFunc("POST /event_control/secret", web.eventControlSecretHandler)
	mux.HandleFunc("POST /event_control/simulate_all", web.eventControlSimulateAllHandler)
	mux.HandleFunc("POST /event_control/import_bundle", web.eventControlImportBundleHandler)
	mux.HandleFunc("POST /event_control/assign_field", web.eventControlAssignFieldHandler)
	mux.HandleFunc("POST /event_control/audience_display", web.eventControlAudienceDisplayHandler)

	// Node tools.
	mux.HandleFunc("GET /setup/node/export_bundle", web.nodeExportBundleHandler)
	mux.HandleFunc("POST /setup/node/promote", web.nodePromoteHandler)
	mux.HandleFunc("POST /setup/node/resync", web.nodeResyncHandler)
	mux.HandleFunc("GET /setup/node/status", web.nodeStatusHandler)

	// Simulation for development and dry runs.
	mux.HandleFunc("POST /api/dev/simulate_match", web.simulateMatchHandler)
	mux.HandleFunc("POST /api/dev/auto_simulate", web.autoSimulateHandler)
	mux.HandleFunc("GET /api/dev/status", web.devStatusHandler)
	mux.HandleFunc("POST /api/dev/seed_event", web.devSeedEventHandler)
	mux.HandleFunc("POST /api/dev/auto_alliance_selection", web.devAutoAllianceSelectionHandler)
}

// Wraps a hub handler so that it returns 404 unless this instance is running as the hub.
func (web *Web) hubHandler(handler func(*hub.Hub) http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if web.hub == nil {
			http.Error(w, "this instance is not running as a multi-field hub", http.StatusNotFound)
			return
		}
		handler(web.hub)(w, r)
	}
}

// Starts or stops the hub service and the node client to match the multi-field settings that were just saved, so that
// a role change takes effect without restarting, and reconnects a node whose hub connection settings changed. Returns
// a note for the operator if something still needs their attention, or the empty string.
func (web *Web) applyMultiFieldRole(previous *model.EventSettings) string {
	settings := web.arena.EventSettings
	if !settings.IsNode() && web.node != nil {
		web.node.Stop()
		web.arena.NodeLink = nil
		web.node = nil
		log.Printf("Stopped the node client; this machine is now %s", settings.MultiFieldRole)
	}
	if !settings.IsHub() && web.hub != nil {
		web.hub.Stop()
		web.hub = nil
		log.Printf("Stopped the hub service; this machine is now %s", settings.MultiFieldRole)
	}
	if settings.IsHub() && web.hub == nil {
		web.hub = hub.New(web.arena)
		log.Printf("Started the hub service")
	}
	if settings.IsNode() {
		if web.node == nil {
			web.SetNode(node.Start(web.arena))
			log.Printf("Started the node client for %s", settings.DisplayFieldName())
		} else if settings.HubAddress != previous.HubAddress || settings.HubSharedSecret != previous.HubSharedSecret ||
			settings.FieldId != previous.FieldId || settings.FieldName != previous.FieldName {
			web.node.Reconnect()
		}
	}
	web.arena.MultiFieldStatusNotifier.Notify()

	if previous.IsHub() && !settings.IsHub() && !web.arena.SimulateHardware {
		return "Saved. This machine is no longer the hub; restart Cheesy Arena so that it starts talking to the " +
			"field hardware (the hub doesn't)."
	}
	if !previous.IsHub() && settings.IsHub() && !web.arena.SimulateHardware {
		return "Saved. This machine is now the hub. Restart Cheesy Arena when convenient so that it stops talking " +
			"to field hardware, then open Run → Event Control."
	}
	return ""
}

// Wraps a handler that changes event data owned by the hub (teams, schedule, awards, etc.) so that it is refused on a
// field node, where the change would be silently overwritten by the next update from the hub.
func (web *Web) hubOwnedData(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if web.arena.EventSettings.IsNode() {
			web.renderNodeReadOnly(w, r)
			return
		}
		handler(w, r)
	}
}

// Explains that a change to hub-owned data must be made on the hub, with a link to the same page there.
func (web *Web) renderNodeReadOnly(w http.ResponseWriter, r *http.Request) {
	template, err := web.parseFiles("templates/node_readonly.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	// Link to the page the user came from (e.g. the team list), both here and on the hub.
	path := r.URL.Path
	back := ""
	if referer, err := url.Parse(r.Referer()); err == nil && referer.Path != "" && referer.Host == r.Host {
		path = referer.Path
		back = referer.RequestURI()
	}
	data := struct {
		*model.EventSettings
		Path string
		Back string
	}{web.arena.EventSettings, path, back}
	w.WriteHeader(http.StatusConflict)
	if err = template.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Failed to render the node read-only page: %v", err)
	}
}

// Shows the hub's Event Control page.
func (web *Web) eventControlHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	message := ""
	query := r.URL.Query()
	if query.Get("saved") == "secret" {
		message = "Shared secret saved. Enter the same secret on each field laptop (Settings → Multi-Field)."
	} else if moved := query.Get("moved"); moved != "" {
		if query.Get("field") == "0" {
			message = fmt.Sprintf("%s no longer has a field.", moved)
		} else {
			message = fmt.Sprintf("%s is now on Field %s.", moved, query.Get("field"))
		}
	}
	web.renderEventControl(w, r, message, nil)
}

func (web *Web) renderEventControl(w http.ResponseWriter, r *http.Request, message string, importSummary []string) {
	template, err := web.parseFiles("templates/event_control.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	var registrations []model.NodeRegistration
	var assignable []hub.AssignableMatchGroup
	var steps []checklistStep
	if web.hub != nil {
		registrations, _ = web.arena.Database.GetAllNodeRegistrations()
		assignable = web.hub.AssignableMatches()
		if steps, err = web.eventChecklist(); err != nil {
			handleWebErr(w, err)
			return
		}
	}
	data := struct {
		*model.EventSettings
		IsHub             bool
		Message           string
		ImportSummary     []string
		Registrations     []model.NodeRegistration
		AssignableMatches []hub.AssignableMatchGroup
		Steps             []checklistStep
		HubAddresses      []hubAddress
		Simulate          bool
		Simulation        string
		SimulationRunning bool
	}{
		web.arena.EventSettings, web.hub != nil, message, importSummary, registrations, assignable, steps,
		hubAddressesForNodes(r), web.arena.SimulateHardware, web.eventRunnerStatus(), web.eventRunnerActive(),
	}
	if err = template.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
	}
}

// Returns just the checklist part of the Event Control page, so that the page can refresh it as things change.
func (web *Web) eventControlChecklistHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		http.Error(w, "this instance is not running as a multi-field hub", http.StatusNotFound)
		return
	}
	steps, err := web.eventChecklist()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	template, err := web.parseFiles("templates/event_control.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		Steps             []checklistStep
		Simulate          bool
		Simulation        string
		SimulationRunning bool
	}{web.arena.EventSettings, steps, web.arena.SimulateHardware, web.eventRunnerStatus(), web.eventRunnerActive()}
	if err = template.ExecuteTemplate(w, "checklist", data); err != nil {
		handleWebErr(w, err)
	}
}

// Sets the hub's shared secret from the Event Control page.
func (web *Web) eventControlSecretHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	secret := strings.TrimSpace(r.PostFormValue("hubSharedSecret"))
	if len(secret) < 4 {
		web.renderEventControl(w, r, "The shared secret must be at least 4 characters long.", nil)
		return
	}
	settings, err := web.arena.Database.GetEventSettings()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	settings.HubSharedSecret = secret
	if err = web.arena.Database.UpdateEventSettings(settings); err != nil {
		handleWebErr(w, err)
		return
	}
	if err = web.arena.LoadSettings(); err != nil {
		handleWebErr(w, err)
		return
	}
	web.hub.StatusNotifier.Notify()
	http.Redirect(w, r, "/event_control?saved=secret", 303)
}

// The websocket endpoint for the Event Control page to receive hub status updates.
func (web *Web) eventControlWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		http.Error(w, "this instance is not running as a multi-field hub", http.StatusNotFound)
		return
	}
	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer closeWebsocket(ws)
	ws.HandleNotifiers(web.hub.StatusNotifier, web.arena.EventStatusNotifier)
}

func (web *Web) eventControlApproveHandler(w http.ResponseWriter, r *http.Request) {
	web.eventControlNodeAction(w, r, func(fieldId int) error { return web.hub.ApproveNode(fieldId, true) })
}

func (web *Web) eventControlRevokeHandler(w http.ResponseWriter, r *http.Request) {
	web.eventControlNodeAction(w, r, func(fieldId int) error { return web.hub.ApproveNode(fieldId, false) })
}

func (web *Web) eventControlForgetHandler(w http.ResponseWriter, r *http.Request) {
	web.eventControlNodeAction(w, r, func(fieldId int) error { return web.hub.ForgetNode(fieldId) })
}

func (web *Web) eventControlNodeAction(w http.ResponseWriter, r *http.Request, action func(int) error) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	fieldId, err := strconv.Atoi(r.PathValue("fieldId"))
	if err != nil {
		web.renderEventControl(w, r, "Invalid field ID.", nil)
		return
	}
	if err = action(fieldId); err != nil {
		web.renderEventControl(w, r, err.Error(), nil)
		return
	}
	http.Redirect(w, r, "/event_control", 303)
}

func (web *Web) eventControlResyncHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	web.hub.ForceResync()
	http.Redirect(w, r, "/event_control", 303)
}

// Imports a results bundle exported from a node (e.g. carried over on a USB stick while the network was down).
func (web *Web) eventControlImportBundleHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxResultsBundleBytes)
	file, _, err := r.FormFile("bundleFile")
	if err != nil {
		web.renderEventControl(
			w, r, "Choose the results bundle file (results-field….json, exported from a field laptop) first.", nil,
		)
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil {
		web.renderEventControl(w, r, "Could not read the results bundle: "+err.Error(), nil)
		return
	}
	var bundle hub.ResultsBundle
	if err = json.Unmarshal(contents, &bundle); err != nil {
		web.renderEventControl(
			w, r, "That file isn't a results bundle (it should be the results-field….json file exported from a field "+
				"laptop's Settings → Multi-Field tab).", nil,
		)
		return
	}
	if len(bundle.Submissions) == 0 {
		web.renderEventControl(
			w, r, fmt.Sprintf("The results bundle from field %d has no results in it.", bundle.FieldId), nil,
		)
		return
	}
	result, err := web.hub.ImportBundle(&bundle)
	if err != nil {
		web.renderEventControl(w, r, "Couldn't import the results bundle: "+err.Error()+".", nil)
		return
	}
	web.renderEventControl(
		w,
		r,
		fmt.Sprintf(
			"Imported the results bundle from field %d: %d applied, %d already on the hub, %d not applied.",
			bundle.FieldId, result.Applied, result.AlreadyApplied, result.NotApplied,
		),
		result.Lines,
	)
}

// Assigns an unplayed match (e.g. a championship match) to a field.
func (web *Web) eventControlAssignFieldHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	matchId, _ := strconv.Atoi(r.PostFormValue("matchId"))
	fieldId, _ := strconv.Atoi(r.PostFormValue("fieldId"))
	if err := web.hub.AssignMatchField(matchId, fieldId); err != nil {
		web.renderEventControl(w, r, err.Error(), nil)
		return
	}
	query := url.Values{}
	if match, _ := web.arena.Database.GetMatchById(matchId); match != nil {
		query.Set("moved", match.LongName)
		query.Set("field", strconv.Itoa(fieldId))
	}
	http.Redirect(w, r, "/event_control?"+query.Encode(), 303)
}

// Downloads a results bundle from a node, for carrying to the hub by hand.
func (web *Web) nodeExportBundleHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.node == nil {
		http.Error(w, "this instance is not running as a field node", http.StatusNotFound)
		return
	}
	bundleJson, err := web.node.ExportBundleJson()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	filename := fmt.Sprintf(
		"results-field%d-%s.json", web.arena.EventSettings.FieldId, time.Now().Format("20060102150405"),
	)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write(bundleJson)
}

// Emergency action: turns this node into a standalone FMS so that the event can continue without the hub.
func (web *Web) nodePromoteHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.node == nil {
		web.renderSettings(w, r, "This instance is not running as a field node.")
		return
	}
	if !settingsSaveAllowed(web.arena.MatchState) {
		web.renderSettings(w, r, "Can't promote this node while a match is in progress or is uncommitted.")
		return
	}
	if err := web.node.PromoteToStandalone(); err != nil {
		web.renderSettings(w, r, "Failed to promote this node: "+err.Error())
		return
	}
	web.node = nil
	http.Redirect(w, r, "/setup/settings#multifield", 303)
}

// Returns this machine's multi-field status and any results the hub rejected, for the Multi-Field settings tab.
func (web *Web) nodeStatusHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	type rejectedResult struct {
		Match     string
		Error     string
		Attempts  int
		CreatedAt time.Time
	}
	rejected := []rejectedResult{}
	queued := []string{}
	if web.arena.EventSettings.IsNode() {
		entries, err := web.arena.Database.GetAllSyncOutboxEntries()
		if err != nil {
			handleWebErr(w, err)
			return
		}
		for _, entry := range entries {
			if entry.Rejected {
				rejected = append(
					rejected, rejectedResult{entry.Match.ShortName, entry.LastError, entry.Attempts, entry.CreatedAt},
				)
			} else {
				queued = append(queued, entry.Match.ShortName)
			}
		}
	}
	status := web.arena.GetMultiFieldStatus()
	writeJsonResponse(
		w,
		map[string]any{
			"Status":   status,
			"Summary":  multiFieldStatusText(status),
			"Queued":   queued,
			"Rejected": rejected,
		},
	)
}

func (web *Web) nodeResyncHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.node == nil {
		web.renderSettings(w, r, "This instance is not running as a field node.")
		return
	}
	if err := web.node.RetryRejected(); err != nil {
		web.renderSettings(w, r, "Failed to requeue rejected results: "+err.Error())
		return
	}
	if err := web.node.SyncAll(); err != nil {
		web.renderSettings(w, r, "Failed to resync from the hub: "+err.Error())
		return
	}
	http.Redirect(w, r, "/match_play", 303)
}

// State of the automatic match simulator.
type autoSimulator struct {
	mutex       sync.Mutex
	stopChan    chan struct{}
	intervalSec int
	matchType   model.MatchType
	count       int
	lastError   string
}

var simulationRandom = rand.New(rand.NewSource(time.Now().UnixNano()))
var simulationRandomMutex sync.Mutex

// Plays the next match on this field instantly with a random score and commits it (only with -simulate).
func (web *Web) simulateMatchHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	matchType, err := simulationMatchType(r.FormValue("type"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	match, err := web.simulateOneMatch(matchType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJsonResponse(w, map[string]any{"Simulated": match.ShortName, "Status": match.Status})
}

func (web *Web) simulateOneMatch(matchType model.MatchType) (*model.Match, error) {
	simulationRandomMutex.Lock()
	err := web.arena.PrepareSimulatedMatch(simulationRandom, matchType)
	simulationRandomMutex.Unlock()
	if err != nil {
		return nil, err
	}
	match := web.arena.CurrentMatch
	if err = web.commitCurrentMatchScore(); err != nil {
		return nil, err
	}
	web.arena.SetAudienceDisplayMode("score")
	if err = web.arena.ResetMatch(); err != nil {
		return nil, err
	}
	if err = web.arena.LoadNextMatch(false); err != nil {
		log.Printf("Simulator could not load the next match: %v", err)
	}
	web.arena.MatchListNotifier.Notify()
	return match, nil
}

// Starts or stops automatically simulating a match every few seconds (only with -simulate).
func (web *Web) autoSimulateHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if !web.arena.SimulateHardware {
		http.Error(w, "match simulation is only available when running with -simulate", http.StatusConflict)
		return
	}
	intervalSec, _ := strconv.Atoi(r.FormValue("intervalSec"))
	matchType, err := simulationMatchType(r.FormValue("type"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	web.setAutoSimulator(matchType, intervalSec)
	web.devStatusHandler(w, r)
}

// Starts simulating a match of the given type every intervalSec seconds, or stops if intervalSec is zero. Asking for
// what is already running leaves it alone.
func (web *Web) setAutoSimulator(matchType model.MatchType, intervalSec int) {
	simulator := &web.simulator
	simulator.mutex.Lock()
	defer simulator.mutex.Unlock()
	running := simulator.stopChan != nil
	if running && intervalSec > 0 && simulator.intervalSec == intervalSec && simulator.matchType == matchType {
		return
	}
	if running {
		close(simulator.stopChan)
		simulator.stopChan = nil
	}
	if intervalSec > 0 {
		stopChan := make(chan struct{})
		simulator.stopChan = stopChan
		simulator.intervalSec = intervalSec
		simulator.matchType = matchType
		simulator.count = 0
		simulator.lastError = ""
		go web.runAutoSimulator(stopChan, time.Duration(intervalSec)*time.Second, matchType)
	} else {
		simulator.intervalSec = 0
	}
}

func (web *Web) runAutoSimulator(stopChan chan struct{}, interval time.Duration, matchType model.MatchType) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
		}
		match, err := web.simulateOneMatch(matchType)
		web.simulator.mutex.Lock()
		if err != nil {
			web.simulator.lastError = err.Error()
		} else {
			web.simulator.count++
			web.simulator.lastError = ""
			log.Printf("Simulated %s", match.ShortName)
		}
		web.simulator.mutex.Unlock()
	}
}

// Reports the state of this instance for scripts driving a simulated event.
func (web *Web) devStatusHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	web.simulator.mutex.Lock()
	simulatorStatus := map[string]any{
		"Running":     web.simulator.stopChan != nil,
		"IntervalSec": web.simulator.intervalSec,
		"MatchType":   web.simulator.matchType.String(),
		"Simulated":   web.simulator.count,
		"LastError":   web.simulator.lastError,
	}
	web.simulator.mutex.Unlock()
	status := map[string]any{
		"Role":         web.arena.EventSettings.MultiFieldRole.String(),
		"FieldId":      web.arena.EventSettings.FieldId,
		"Simulate":     web.arena.SimulateHardware,
		"CurrentMatch": web.arena.CurrentMatch.ShortName,
		"MultiField":   web.arena.GetMultiFieldStatus(),
		"Simulator":    simulatorStatus,
	}
	writeJsonResponse(w, status)
}

func simulationMatchType(value string) (model.MatchType, error) {
	if value == "" {
		return model.Qualification, nil
	}
	matchType, err := model.MatchTypeFromString(value)
	if err != nil || matchType == model.Test {
		return 0, fmt.Errorf("invalid match type %q", value)
	}
	return matchType, nil
}

func writeJsonResponse(w http.ResponseWriter, data any) {
	// Declaring the charset lets clients such as Windows PowerShell 5.1 decode characters like "→" correctly.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to write JSON response: %v", err)
	}
}

// Returns the multi-field status line shown in page headers, e.g. "Field 2 · Hub connected · 0 queued".
func multiFieldStatusText(status field.MultiFieldStatus) string {
	if status.IsHub {
		return "Hub"
	}
	if !status.IsNode {
		return ""
	}
	parts := []string{status.FieldName}
	if status.Connected && status.Approved {
		parts = append(parts, "Hub connected")
	} else if status.Connected {
		parts = append(parts, "Awaiting hub approval")
	} else if status.Refused {
		parts = append(parts, "Hub refused this field")
	} else {
		parts = append(parts, "HUB OFFLINE")
	}
	parts = append(parts, fmt.Sprintf("%d queued", status.OutboxSize))
	if status.RejectedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d rejected", status.RejectedCount))
	}
	return strings.Join(parts, " · ")
}

// Switches the audience displays to the given mode (e.g. the dual-field view) from the Event Control page.
func (web *Web) eventControlAudienceDisplayHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	switch mode := r.PostFormValue("mode"); mode {
	case "dualField", "standings", "bracket", "blank", "logo", "intro", "score":
		web.arena.SetAudienceDisplayMode(mode)
	default:
		web.renderEventControl(w, r, fmt.Sprintf("Unknown audience display mode %q.", mode), nil)
		return
	}
	http.Redirect(w, r, "/event_control", 303)
}
