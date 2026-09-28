// Copyright 2026 Team 254. All Rights Reserved.
//
// "Simulate the whole event" for dry runs of a multi-field event: the hub seeds an empty event, has both field nodes
// play the qualifications, runs alliance selection, has the nodes play the playoffs and stops at the champion. Only
// available when running with -simulate.

package web

import (
	"context"
	"fmt"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/hub"
	"github.com/Team254/cheesy-arena/model"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

const eventRunnerStepPeriod = 2 * time.Second

// State of the whole-event simulation on the hub.
type eventRunner struct {
	mutex    sync.Mutex
	stopChan chan struct{}
	status   string
}

type internalRequestKey struct{}

// Returns true if the request was made by the server itself rather than by a browser.
func isInternalRequest(r *http.Request) bool {
	internal, _ := r.Context().Value(internalRequestKey{}).(bool)
	return internal
}

// Calls one of this server's own handlers with the given form values, as an admin, and returns the status code and
// response body.
func callHandlerInternally(handler http.HandlerFunc, form string) (int, string) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), internalRequestKey{}, true))
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder.Code, strings.TrimSpace(recorder.Body.String())
}

// Returns a description of the whole-event simulation for the Event Control page, or "" if it has never run.
func (web *Web) eventRunnerStatus() string {
	web.eventRunner.mutex.Lock()
	defer web.eventRunner.mutex.Unlock()
	return web.eventRunner.status
}

func (web *Web) eventRunnerActive() bool {
	web.eventRunner.mutex.Lock()
	defer web.eventRunner.mutex.Unlock()
	return web.eventRunner.stopChan != nil
}

func (web *Web) setEventRunnerStatus(status string) {
	web.eventRunner.mutex.Lock()
	changed := web.eventRunner.status != status
	web.eventRunner.status = status
	web.eventRunner.mutex.Unlock()
	if changed && web.hub != nil {
		web.hub.StatusNotifier.Notify()
	}
}

// Starts (or with stop=true, stops) simulating the whole event from the Event Control page.
func (web *Web) eventControlSimulateAllHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if web.hub == nil {
		web.renderEventControl(w, r, "This instance is not running as a multi-field hub.", nil)
		return
	}
	if !web.arena.SimulateHardware {
		web.renderEventControl(w, r, "Simulating the event is only available when running with -simulate.", nil)
		return
	}
	if r.PostFormValue("stop") != "" {
		web.stopEventRunner("Stopped.")
	} else {
		intervalSec, _ := strconv.Atoi(r.PostFormValue("intervalSec"))
		web.startEventRunner(max(1, min(60, intervalSec)))
	}
	http.Redirect(w, r, "/event_control", 303)
}

func (web *Web) startEventRunner(intervalSec int) {
	web.eventRunner.mutex.Lock()
	if web.eventRunner.stopChan != nil {
		web.eventRunner.mutex.Unlock()
		return
	}
	stopChan := make(chan struct{})
	web.eventRunner.stopChan = stopChan
	web.eventRunner.mutex.Unlock()
	web.setEventRunnerStatus("Starting...")
	go web.runEvent(stopChan, intervalSec)
}

func (web *Web) stopEventRunner(status string) {
	web.eventRunner.mutex.Lock()
	if web.eventRunner.stopChan != nil {
		close(web.eventRunner.stopChan)
		web.eventRunner.stopChan = nil
	}
	web.eventRunner.mutex.Unlock()
	if web.hub != nil {
		web.hub.BroadcastSimulate(hub.SimulateCommand{})
	}
	web.setEventRunnerStatus(status)
}

func (web *Web) runEvent(stopChan chan struct{}, intervalSec int) {
	ticker := time.NewTicker(eventRunnerStepPeriod)
	defer ticker.Stop()
	for {
		finished, status, err := web.simulateEventStep(intervalSec)
		if err != nil {
			log.Printf("Event simulation stopped: %v", err)
			web.stopEventRunner("Stopped: " + err.Error())
			return
		}
		if finished {
			web.stopEventRunner(status)
			return
		}
		web.setEventRunnerStatus(status)
		select {
		case <-stopChan:
			return
		case <-ticker.C:
		}
	}
}

// Moves the simulated event along by one step and describes where it is. Returns finished=true once the playoffs are
// over, and an error if the simulation can't continue.
func (web *Web) simulateEventStep(intervalSec int) (bool, string, error) {
	eventHub := web.hub
	if eventHub == nil {
		return false, "", fmt.Errorf("this machine is no longer the hub")
	}
	database := web.arena.Database

	// Seed an empty event.
	quals, err := database.GetMatchesByType(model.Qualification, true)
	if err != nil {
		return false, "", err
	}
	if len(quals) == 0 {
		teams, err := database.GetAllTeams()
		if err != nil {
			return false, "", err
		}
		if len(teams) > 0 {
			return false, "", fmt.Errorf(
				"there are %d teams but no qualification schedule; generate one on Match Scheduling first", len(teams),
			)
		}
		code, body := callHandlerInternally(web.devSeedEventHandler, "teams=36&matchesPerTeam=8")
		if code != http.StatusOK {
			return false, "", fmt.Errorf("couldn't create a test event: %s", body)
		}
		return false, "Created 36 test teams and a qualification schedule.", nil
	}

	fields := 0
	for _, status := range eventHub.NodeStatuses() {
		if status.Connected && status.Approved {
			fields++
		}
	}
	if fields == 0 {
		eventHub.BroadcastSimulate(hub.SimulateCommand{})
		return false, "Waiting for a field to connect (start the field laptops with -simulate).", nil
	}

	// Qualifications.
	played, _ := countPlayed(quals)
	if played < len(quals) {
		eventHub.BroadcastSimulate(hub.SimulateCommand{MatchType: "qualification", IntervalSec: intervalSec})
		return false, fmt.Sprintf("Playing qualifications on %d field(s): %d of %d played.", fields, played,
			len(quals)), nil
	}

	// Alliance selection.
	alliances, err := database.GetAllAlliances()
	if err != nil {
		return false, "", err
	}
	if len(alliances) == 0 {
		eventHub.BroadcastSimulate(hub.SimulateCommand{})
		code, body := callHandlerInternally(web.devAutoAllianceSelectionHandler, "")
		if code != http.StatusOK {
			return false, "", fmt.Errorf("automatic alliance selection failed: %s", body)
		}
		return false, "Qualifications done; alliances selected from the rankings.", nil
	}

	// Playoffs.
	if web.arena.PlayoffTournament != nil && web.arena.PlayoffTournament.IsComplete() {
		return true, "Event complete. " + web.championDescription(), nil
	}
	if err = web.assignWaitingPlayoffMatches(eventHub); err != nil {
		return false, "", err
	}
	playoffs, err := database.GetMatchesByType(model.Playoff, false)
	if err != nil {
		return false, "", err
	}
	playoffsPlayed, _ := countPlayed(playoffs)
	eventHub.BroadcastSimulate(hub.SimulateCommand{MatchType: "playoff", IntervalSec: intervalSec})
	return false, fmt.Sprintf("Playing the playoffs: %d matches played.", playoffsPlayed), nil
}

// Gives any ready playoff match without a field (e.g. a championship match whose field is chosen at load time) to the
// field with the fewest matches waiting.
func (web *Web) assignWaitingPlayoffMatches(eventHub *hub.Hub) error {
	matches, err := web.arena.Database.GetMatchesByType(model.Playoff, false)
	if err != nil {
		return err
	}
	waiting := map[int]int{1: 0, 2: 0}
	for _, match := range matches {
		if !match.IsComplete() && match.FieldId > 0 {
			waiting[match.FieldId]++
		}
	}
	for _, match := range matches {
		if match.IsComplete() || match.FieldId != 0 || match.Status == game.MatchHidden ||
			match.PlayoffRedAlliance == 0 || match.PlayoffBlueAlliance == 0 {
			continue
		}
		fieldId := 1
		if waiting[2] < waiting[1] {
			fieldId = 2
		}
		if err = eventHub.AssignMatchField(match.Id, fieldId); err != nil {
			return err
		}
		waiting[fieldId]++
	}
	return nil
}

// Describes the winner of the event (or of each conference if there is no championship).
func (web *Web) championDescription() string {
	awards, err := web.arena.Database.GetAllAwards()
	if err != nil {
		return ""
	}
	winners := map[string][]string{}
	var order []string
	for _, award := range awards {
		if award.Type != model.EventChampionAward && award.Type != model.WinnerAward &&
			award.Type != model.ConferenceWinnerAward {
			continue
		}
		if _, ok := winners[award.AwardName]; !ok {
			order = append(order, award.AwardName)
		}
		winners[award.AwardName] = append(winners[award.AwardName], strconv.Itoa(award.TeamId))
	}
	var parts []string
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s: %s", name, strings.Join(winners[name], ", ")))
	}
	return strings.Join(parts, "; ")
}
