// Copyright 2026 Team 254. All Rights Reserved.
//
// Web routes for the hub lighting simulator, which shows what the Hub LEDs are doing without the physical lights, and
// the field simulator, which shows the whole field: the Hubs, driver stations, signs, and displays.

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Team254/cheesy-arena/dssim"
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/led"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"github.com/mitchellh/mapstructure"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	hubSimFramePeriod        = 20 * time.Millisecond
	hubSimIdleFramePeriod    = 250 * time.Millisecond
	fieldSimStatusFrames     = 5
	fieldSimIdleStatusPeriod = time.Second
	simHiddenUpdatePeriod    = time.Second
)

// Shows the hub lighting simulator for one Hub ("red" or "blue") or both.
func (web *Web) hubSimHandler(w http.ResponseWriter, r *http.Request) {
	alliance := r.URL.Query().Get("alliance")
	if alliance != "red" && alliance != "blue" {
		alliance = "both"
	}
	web.renderSimPage(w, r, "hub_sim.html", alliance)
}

// Shows the field simulator.
func (web *Web) fieldSimHandler(w http.ResponseWriter, r *http.Request) {
	web.renderSimPage(w, r, "field_sim.html", "both")
}

// renderSimPage renders one of the simulator pages along with the configuration its script needs.
func (web *Web) renderSimPage(w http.ResponseWriter, r *http.Request, templateName, alliance string) {
	template, err := web.parseFiles("templates/" + templateName)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	config, err := json.Marshal(
		struct {
			Alliance         string
			SimEnabled       bool
			CanControl       bool
			ModeNames        map[led.Mode]string
			SideNames        []string
			FixtureNames     []string
			PixelsPerFixture int
			CoilNames        []string
			RobotStates      map[dssim.RobotState]string
			NetworkSecurity  bool
		}{
			alliance,
			web.arena.SimControlsEnabled,
			web.simControlsAllowed(r),
			led.ModeNames,
			led.SideNames[:],
			led.FixtureNames[:],
			led.PixelsPerFixture,
			web.arena.Plc.GetCoilNames(),
			dssim.RobotStateNames,
			web.arena.EventSettings.NetworkSecurityEnabled,
		},
	)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		Alliance string
		Config   string
	}{web.arena.EventSettings, alliance, string(config)}
	err = template.ExecuteTemplate(w, templateName, data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for streaming to the hub lighting simulator and receiving its commands.
func (web *Web) hubSimWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	web.handleSimWebsocket(w, r)
}

// The websocket endpoint for streaming to the field simulator and receiving its commands.
func (web *Web) fieldSimWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	web.handleSimWebsocket(w, r)
}

// handleSimWebsocket streams the simulator state and runs its test match commands until the client disconnects.
func (web *Web) handleSimWebsocket(w http.ResponseWriter, r *http.Request) {
	canControl := web.simControlsAllowed(r)

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer closeWebsocket(ws)

	writeWebsocketMessage(
		ws,
		"hubSimTimeline",
		struct {
			RedWonAuto  field.HubLedTimeline
			BlueWonAuto field.HubLedTimeline
			Swatches    map[string]map[led.Mode]simModeSwatch
		}{field.GetHubLedTimeline(true), field.GetHubLedTimeline(false), simModeSwatches()},
	)

	// Use the same match time messages as the displays so that the clock is computed the same way.
	go ws.HandleNotifiers(web.arena.MatchTimingNotifier, web.arena.MatchTimeNotifier)

	// The page reports when it is hidden (minimized or covered) so that it gets occasional updates instead of a stream.
	var visible atomic.Bool
	visible.Store(true)

	// Stream the LEDs often enough to show the arena loop's animations, and the rest of the field less often; only
	// send unchanged state occasionally.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(hubSimFramePeriod)
		defer ticker.Stop()
		var lastFrame field.HubLedSnapshot
		var lastFrameTime time.Time
		var lastStatus field.FieldSimStatus
		var lastStatusTime time.Time
		for tick := 0; ; tick++ {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			if !visible.Load() && time.Since(lastFrameTime) < simHiddenUpdatePeriod {
				continue
			}
			frame := web.arena.GetHubLedSnapshot()
			if frame != lastFrame || time.Since(lastFrameTime) >= hubSimIdleFramePeriod {
				if err := ws.Write("hubSimFrame", frame); err != nil {
					return
				}
				lastFrame = frame
				lastFrameTime = time.Now()
			}

			if tick%fieldSimStatusFrames == 0 {
				status := web.arena.GetFieldSimStatus()
				if status != lastStatus || time.Since(lastStatusTime) >= fieldSimIdleStatusPeriod {
					if err := ws.Write("fieldSimStatus", status); err != nil {
						return
					}
					lastStatus = status
					lastStatusTime = time.Now()
				}
			}
		}
	}()

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}

		if messageType == "setVisible" {
			if isVisible, ok := data.(bool); ok {
				visible.Store(isVisible)
			}
			continue
		}
		if !canControl {
			writeWebsocketError(
				ws, "Simulator controls are off. Start Cheesy Arena with -hubsim or -simfield (and log in as admin).",
			)
			continue
		}

		switch messageType {
		case "startMatch":
			err = web.arena.StartHubSimMatch()
		case "abortMatch":
			err = web.arena.AbortHubSimMatch()
		case "resetMatch":
			err = web.arena.ResetHubSimMatch()
		case "skipAhead":
			seconds, ok := data.(float64)
			if !ok {
				err = fmt.Errorf("failed to parse '%s' message", messageType)
				break
			}
			err = web.arena.SkipHubSimMatchTime(seconds)
		case "setAutoWinner":
			winner, ok := data.(string)
			if !ok {
				err = fmt.Errorf("failed to parse '%s' message", messageType)
				break
			}
			err = web.arena.SetHubSimAutoWinner(winner)
		case "setLedMode":
			err = web.setHubSimLedMode(data)
		case "setStationStop":
			args := struct {
				Station string
				AStop   bool
				Pressed bool
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.arena.SetSimStationStop(args.Station, args.AStop, args.Pressed)
			}
		case "setFieldEStop":
			pressed, ok := data.(bool)
			if !ok {
				err = fmt.Errorf("failed to parse '%s' message", messageType)
				break
			}
			err = web.arena.SetSimFieldEStop(pressed)
		case "setRobotState":
			args := struct {
				Station string
				State   dssim.RobotState
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.arena.SetSimRobotState(args.Station, args.State)
			}
		case "useEventTeams":
			err = web.arena.UseEventTeamsInSimMatches()
		case "clearSimTeams":
			err = web.arena.ClearSimMatchTeams()
		case "addFuel":
			args := struct {
				Alliance string
				Count    int
			}{}
			if err = mapstructure.Decode(data, &args); err == nil {
				err = web.arena.AddSimHubFuel(args.Alliance, args.Count)
			}
		default:
			err = fmt.Errorf("invalid message type '%s'", messageType)
		}
		if err != nil {
			writeWebsocketError(ws, err.Error())
		}
	}
}

// setHubSimLedMode previews an LED mode on one or both Hubs while no match is running.
func (web *Web) setHubSimLedMode(data any) error {
	args := struct {
		Alliance string
		Mode     led.Mode
	}{}
	if err := mapstructure.Decode(data, &args); err != nil {
		return err
	}
	if !fieldTestingOverridesAllowed(web.arena.MatchState) {
		return errors.New(fieldTestingLedModeDisabledMessage)
	}
	if _, ok := led.ModeNames[args.Mode]; !ok {
		return fmt.Errorf("invalid LED mode '%d'", args.Mode)
	}

	redMode, blueMode := web.arena.Leds.GetModes()
	switch args.Alliance {
	case "red":
		redMode = args.Mode
	case "blue":
		blueMode = args.Mode
	case "both":
		redMode, blueMode = args.Mode, args.Mode
	default:
		return fmt.Errorf("invalid alliance '%s'", args.Alliance)
	}
	web.arena.Leds.SetMode(redMode, blueMode)
	web.arena.LedChangeNotifier.Notify()
	return nil
}

// simControlsAllowed returns true if the server was started with -hubsim or -simfield and the user may run test
// matches.
func (web *Web) simControlsAllowed(r *http.Request) bool {
	if !web.arena.SimControlsEnabled {
		return false
	}
	if web.arena.EventSettings.AdminPassword == "" {
		return true
	}
	session := web.getUserSessionFromCookie(r)
	return session != nil && session.Username == adminUser
}

// simModeSwatch is a preview color for an LED mode, as rendered by the led package.
type simModeSwatch struct {
	Color    string
	Animated bool
}

// simModeSwatches renders every LED mode on each alliance's Hub so that the simulator can draw them without knowing
// what the modes look like.
func simModeSwatches() map[string]map[led.Mode]simModeSwatch {
	swatches := make(map[string]map[led.Mode]simModeSwatch)
	for alliance, baseColor := range map[string]led.Color{"red": led.Red, "blue": led.Blue} {
		swatches[alliance] = make(map[led.Mode]simModeSwatch)
		for mode := range led.ModeNames {
			color, animated := led.ModeSwatch(mode, baseColor)
			swatches[alliance][mode] = simModeSwatch{fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B), animated}
		}
	}
	return swatches
}
