// Copyright 2022 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side methods for the bracket display.

var websocket;

// Whether the current match has been aborted, as of the last match time message; null until the first one arrives.
let matchAborted = null;

// Fetches the bracket and swaps it in, highlighting the current match.
const reloadBracket = function () {
  fetch("/api/bracket/svg?activeMatch=current")
    .then(response => response.text())
    .then(svg => $("#bracket").html(svg));
};

// Handles a websocket message to load a new match.
const handleMatchLoad = function (data) {
  reloadBracket();
};

// Handles a websocket message to update the match time, reloading the bracket to flag or unflag an aborted match.
const handleMatchTime = function (data) {
  const aborted = data.MatchAborted === true;
  if (matchAborted !== null && aborted !== matchAborted) {
    reloadBracket();
  }
  matchAborted = aborted;
};

$(function () {
  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/displays/bracket/websocket", {
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
  });
});
