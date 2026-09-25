// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web handlers for the referee interface.

package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"github.com/mitchellh/mapstructure"
)

// Renders the referee interface for assigning fouls.
func (web *Web) refereePanelHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/referee_panel.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		*model.EventSettings
	}{web.arena.EventSettings}
	err = template.ExecuteTemplate(w, "base_no_navbar", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// Messages that may only be sent from the head referee panel.
var headRefereeOnlyMessages = map[string]bool{
	"card":             true,
	"toggleBypass":     true,
	"signalVolunteers": true,
	"signalReset":      true,
	"toggleFtaReady":   true,
	"commitAndPost":    true,
}

// A foul along with the metadata needed to render it in the combined foul list.
type refereePanelFoul struct {
	Alliance string
	Number   int // One-based position of the foul within its alliance's list.
	Foul     game.Foul
	TeamIds  [3]int
}

// A foul that was deleted from a referee panel and can still be restored.
type deletedFoul struct {
	alliance string
	match    *model.Match
	foul     game.Foul
}

// Returns whether the request is from the head referee panel rather than a regular referee panel.
func isHeadRefereeRequest(r *http.Request) bool {
	return r.URL.Query().Get("hr") != "false"
}

// Returns a pointer to the given alliance's foul list.
func (web *Web) allianceFouls(alliance string) *[]game.Foul {
	if alliance == "red" {
		return &web.arena.RedRealtimeScore.CurrentScore.Fouls
	}
	return &web.arena.BlueRealtimeScore.CurrentScore.Fouls
}

// Returns the index of the foul with the given ID within the list, or -1 if it is not present.
func findFoulIndex(fouls []game.Foul, foulId int) int {
	for i, foul := range fouls {
		if foul.FoulId == foulId {
			return i
		}
	}
	return -1
}

// Returns the fouls for both alliances as a single list, ordered from newest to oldest.
func (web *Web) combinedFoulList() []refereePanelFoul {
	match := web.arena.CurrentMatch
	var fouls []refereePanelFoul
	for i, foul := range web.arena.RedRealtimeScore.CurrentScore.Fouls {
		fouls = append(fouls, refereePanelFoul{"red", i + 1, foul, [3]int{match.Red1, match.Red2, match.Red3}})
	}
	for i, foul := range web.arena.BlueRealtimeScore.CurrentScore.Fouls {
		fouls = append(fouls, refereePanelFoul{"blue", i + 1, foul, [3]int{match.Blue1, match.Blue2, match.Blue3}})
	}
	sort.SliceStable(fouls, func(i, j int) bool {
		return fouls[i].Foul.FoulId > fouls[j].Foul.FoulId
	})
	return fouls
}

// Renders a partial template for when the foul list is updated.
func (web *Web) refereePanelFoulListHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/referee_panel_foul_list.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}

	data := struct {
		Fouls []refereePanelFoul
		Rules map[int]*game.Rule
	}{
		web.combinedFoulList(),
		game.GetAllRules(),
	}
	err = template.ExecuteTemplate(w, "referee_panel_foul_list", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the refereee interface client to send control commands and receive status updates.
func (web *Web) refereePanelWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	isHeadReferee := isHeadRefereeRequest(r)
	source := "Ref"
	if isHeadReferee {
		source = "Head Ref"
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer closeWebsocket(ws)
	web.arena.RefereePanels.RegisterPanel(isHeadReferee)
	web.arena.ScoringStatusNotifier.Notify()
	defer web.arena.ScoringStatusNotifier.Notify()
	defer web.arena.RefereePanels.UnregisterPanel(isHeadReferee)

	// Subscribe the websocket to the notifiers whose messages will be passed on to the client, in a separate goroutine.
	go ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
		web.arena.RealtimeScoreNotifier,
		web.arena.ScoringStatusNotifier,
		web.arena.ReloadDisplaysNotifier,
		web.arena.ArenaStatusNotifier,
		web.arena.AllianceStationDisplayModeNotifier,
	)

	// Fouls deleted from this panel, keyed by foul ID, so that they can be restored.
	deletedFouls := make(map[int]deletedFoul)

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

		if headRefereeOnlyMessages[messageType] && !isHeadReferee {
			writeWebsocketError(ws, fmt.Sprintf("Only the head referee can send '%s'.", messageType))
			continue
		}

		switch messageType {
		case "addFoul":
			args := struct {
				Alliance string
				IsMajor  bool
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}

			// Add the foul to the correct alliance's list.
			foul := game.Foul{FoulId: web.arena.NextFoulId, IsMajor: args.IsMajor, Source: source}
			web.arena.NextFoulId++
			fouls := web.allianceFouls(args.Alliance)
			*fouls = append(*fouls, foul)
			web.arena.RealtimeScoreNotifier.Notify()
		case "toggleFoulType", "updateFoulTeam", "updateFoulRule", "deleteFoul":
			args := struct {
				Alliance string
				FoulId   int
				TeamId   int
				RuleId   int
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}

			// Find the foul by its ID so that concurrent edits from other panels can't cause the wrong one to change.
			fouls := web.allianceFouls(args.Alliance)
			index := findFoulIndex(*fouls, args.FoulId)
			if index < 0 {
				continue
			}
			switch messageType {
			case "toggleFoulType":
				(*fouls)[index].IsMajor = !(*fouls)[index].IsMajor
				(*fouls)[index].RuleId = 0
			case "deleteFoul":
				deletedFouls[args.FoulId] = deletedFoul{args.Alliance, web.arena.CurrentMatch, (*fouls)[index]}
				*fouls = append((*fouls)[:index], (*fouls)[index+1:]...)
			case "updateFoulTeam":
				if (*fouls)[index].TeamId == args.TeamId {
					(*fouls)[index].TeamId = 0
				} else {
					(*fouls)[index].TeamId = args.TeamId
				}
			case "updateFoulRule":
				(*fouls)[index].RuleId = args.RuleId
			}
			web.arena.RealtimeScoreNotifier.Notify()
		case "restoreFoul":
			args := struct {
				FoulId int
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}

			deleted, ok := deletedFouls[args.FoulId]
			delete(deletedFouls, args.FoulId)
			if !ok || deleted.match != web.arena.CurrentMatch {
				// Don't restore fouls into a different match than the one they were deleted from.
				continue
			}
			fouls := web.allianceFouls(deleted.alliance)
			if findFoulIndex(*fouls, deleted.foul.FoulId) >= 0 {
				continue
			}

			// Re-insert the foul in its original chronological position.
			index := sort.Search(len(*fouls), func(i int) bool {
				return (*fouls)[i].FoulId > deleted.foul.FoulId
			})
			*fouls = append(*fouls, game.Foul{})
			copy((*fouls)[index+1:], (*fouls)[index:])
			(*fouls)[index] = deleted.foul
			web.arena.RealtimeScoreNotifier.Notify()
		case "card":
			args := struct {
				Alliance string
				TeamId   int
				Card     string
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}

			// Set the card in the correct alliance's score.
			var cards map[string]string
			if args.Alliance == "red" {
				cards = web.arena.RedRealtimeScore.Cards
			} else {
				cards = web.arena.BlueRealtimeScore.Cards
			}
			if web.arena.CurrentMatch.Type == model.Playoff {
				// Cards apply to the whole alliance in playoffs.
				if args.Alliance == "red" {
					cards[strconv.Itoa(web.arena.CurrentMatch.Red1)] = args.Card
					cards[strconv.Itoa(web.arena.CurrentMatch.Red2)] = args.Card
					cards[strconv.Itoa(web.arena.CurrentMatch.Red3)] = args.Card
				} else {
					cards[strconv.Itoa(web.arena.CurrentMatch.Blue1)] = args.Card
					cards[strconv.Itoa(web.arena.CurrentMatch.Blue2)] = args.Card
					cards[strconv.Itoa(web.arena.CurrentMatch.Blue3)] = args.Card
				}
			} else {
				cards[strconv.Itoa(args.TeamId)] = args.Card
			}
			web.arena.RealtimeScoreNotifier.Notify()
		case "toggleBypass":
			station, ok := data.(string)
			if !ok {
				writeWebsocketError(ws, fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			err = web.arena.ToggleBypass(station)
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}
		case "signalVolunteers":
			web.arena.SignalVolunteers()
		case "signalReset":
			web.arena.SignalReset()
		case "toggleFtaReady":
			web.arena.Plc.SetFtaReady(!web.arena.Plc.IsFtaReady())
			web.arena.ArenaStatusNotifier.Notify()
		case "commitAndPost":
			if web.arena.MatchState != field.PostMatch {
				// Don't allow committing the fouls until the match is over.
				continue
			}
			web.arena.RedRealtimeScore.FoulsCommitted = true
			web.arena.BlueRealtimeScore.FoulsCommitted = true
			web.arena.ScoringStatusNotifier.Notify()

			err = web.commitPostAndLoadNextMatch()
			if err != nil {
				writeWebsocketError(ws, err.Error())
				continue
			}
		default:
			writeWebsocketError(ws, fmt.Sprintf("Invalid message type '%s'.", messageType))
		}
	}
}
