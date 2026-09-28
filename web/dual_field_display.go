// Copyright 2026 Team 254. All Rights Reserved.
//
// Web routes for the hub's dual-field display, which shows both fields' current matches side by side on the venue's
// main screen.

package web

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"net/http"
)

// Renders the dual-field display.
func (web *Web) dualFieldDisplayHandler(w http.ResponseWriter, r *http.Request) {
	if !web.enforceDisplayConfiguration(w, r, map[string]string{"background": "#000"}) {
		return
	}

	template, err := web.parseFiles("templates/dual_field_display.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		IsHub bool
	}{web.arena.EventSettings, web.hub != nil}
	if err = template.ExecuteTemplate(w, "dual_field_display.html", data); err != nil {
		handleWebErr(w, err)
	}
}

// The websocket endpoint for the dual-field display to receive the status of both fields.
func (web *Web) dualFieldDisplayWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	display, err := web.registerDisplay(r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer web.arena.MarkDisplayDisconnected(display.DisplayConfiguration.Id)

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer closeWebsocket(ws)

	notifiers := []*websocket.Notifier{display.Notifier, web.arena.ReloadDisplaysNotifier}
	if web.hub != nil {
		notifiers = append(notifiers, web.hub.StatusNotifier)
	}
	ws.HandleNotifiers(notifiers...)
}
