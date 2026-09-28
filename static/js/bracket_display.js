// Copyright 2022 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side methods for the bracket display.

var websocket;

// Handles a websocket message to load a new match.
const handleMatchLoad = function (data) {
  const urlParams = new URLSearchParams(window.location.search);
  let bracketUrl = "/api/bracket/svg?activeMatch=current";
  // Pass through the multi-conference view parameters (?bracket=all, ?conference=1|2|all, ?field=1|2).
  const conference = urlParams.get("conference") || (urlParams.get("bracket") === "all" ? "all" : null);
  if (conference) {
    bracketUrl += "&conference=" + encodeURIComponent(conference);
  }
  if (urlParams.get("field")) {
    bracketUrl += "&field=" + encodeURIComponent(urlParams.get("field"));
  }
  fetch(bracketUrl)
    .then(response => response.text())
    .then(svg => $("#bracket").html(svg));
};

$(function () {
  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/displays/bracket/websocket", {
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
  });
});
