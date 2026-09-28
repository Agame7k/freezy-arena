// Copyright 2026 Team 254. All Rights Reserved.
//
// Client-side logic for the hub's dual-field display.

var websocket;

const escapeText = function (text) {
  return $("<div>").text(text === undefined || text === null ? "" : String(text)).html();
};

// Renders one panel per field with its current match, timer and score.
const handleHubStatus = function (data) {
  const fields = (data.Fields || []).filter(field => field.FieldId > 0);
  $("#fields").html(fields.map(function (field) {
    const teams = field.TeamIds || [];
    const minutes = Math.floor(field.MatchTimeSec / 60);
    const seconds = String(field.MatchTimeSec % 60).padStart(2, "0");
    const upNext = (field.Upcoming || []).slice(0, 2).map(match => escapeText(match.ShortName)).join(", ");
    return `
      <div class="field ${field.Connected ? "" : "offline"}">
        <div class="field-name">${escapeText(field.FieldName)}</div>
        <div class="field-status">
          ${escapeText(field.MatchStateName || (field.Connected ? "" : "Offline"))}
          ${field.EarlyLateMessage ? "&middot; " + escapeText(field.EarlyLateMessage) : ""}
          ${upNext ? "&middot; Up next: " + upNext : ""}
        </div>
        <div class="match-name">${escapeText(field.CurrentMatchName || "")}</div>
        <div class="match-time">${minutes}:${seconds}</div>
        <div class="alliances">
          <div class="alliance red">
            <div class="score">${field.RedScore}</div>
            <div class="teams">${teams.slice(0, 3).join(" &middot; ")}</div>
          </div>
          <div class="alliance blue">
            <div class="score">${field.BlueScore}</div>
            <div class="teams">${teams.slice(3, 6).join(" &middot; ")}</div>
          </div>
        </div>
      </div>`;
  }).join(""));
};

$(function () {
  const urlParams = new URLSearchParams(window.location.search);
  document.body.style.backgroundColor = urlParams.get("background") || "#000";
  websocket = new CheesyWebsocket("/displays/dual_field/websocket", {
    hubStatus: function (event) {
      handleHubStatus(event.data);
    },
  });
});
