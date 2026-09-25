// Copyright 2023 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the referee interface.

var websocket;
let redFoulsHashCode = 0;
let blueFoulsHashCode = 0;
let scoreIsReady = false;
let isPostMatch = false;
let isHeadReferee = true;
let latestScoringStatus = null;
let latestCards = {};
let allianceStationDisplayMode = "";

// In playoffs, cards apply to the whole alliance and a red card disqualifies the alliance from the match.
let isPlayoff = false;
let playoffAllianceNumbers = {red: 0, blue: 0};

// State for rendering the foul list without clobbering in-progress edits.
let foulListRequestId = 0;
let pendingFoulListHtml = null;
let knownFoulIds = null;

// State for the undo toast shown after deleting a foul.
let lastDeletedFoulId = null;
let undoToastTimeout = null;
const undoToastDurationMs = 6000;

// Sends the foul to the server to add it to the list.
const addFoul = function (alliance, isMajor) {
  websocket.send("addFoul", {Alliance: alliance, IsMajor: isMajor});
}

// Toggles the foul type between minor and major.
const toggleFoulType = function (alliance, foulId) {
  websocket.send("toggleFoulType", {Alliance: alliance, FoulId: foulId});
}

// Updates the team that the foul is attributed to.
const updateFoulTeam = function (alliance, foulId, teamId) {
  websocket.send("updateFoulTeam", {Alliance: alliance, FoulId: foulId, TeamId: teamId});
}

// Updates the rule that the foul is for.
const updateFoulRule = function (alliance, foulId, ruleId) {
  websocket.send("updateFoulRule", {Alliance: alliance, FoulId: foulId, RuleId: ruleId});
}

// Removes the foul with the given ID from the list and offers to undo the deletion.
const deleteFoul = function (alliance, foulId) {
  const label = $(`.foul[data-foul-id="${foulId}"] .foul-number`).contents().first().text().trim();
  websocket.send("deleteFoul", {Alliance: alliance, FoulId: foulId});
  showUndoToast(foulId, label);
};

// Shows the toast allowing the most recently deleted foul to be restored.
const showUndoToast = function (foulId, label) {
  lastDeletedFoulId = foulId;
  $("#undoToastText").text(`Deleted foul ${label}`);
  $("#undoToast").attr("data-visible", true);
  clearTimeout(undoToastTimeout);
  undoToastTimeout = setTimeout(hideUndoToast, undoToastDurationMs);
};

const hideUndoToast = function () {
  lastDeletedFoulId = null;
  $("#undoToast").attr("data-visible", false);
  clearTimeout(undoToastTimeout);
};

// Restores the most recently deleted foul.
const undoDelete = function () {
  if (lastDeletedFoulId !== null) {
    websocket.send("restoreFoul", {FoulId: lastDeletedFoulId});
  }
  hideUndoToast();
};

// Cycles through the card options for the selected team.
var cycleCard = function (cardButton) {
  if (isPostMatch) {
    // Cycle card.
    const currentCard = $(cardButton).attr("data-card");
    const hasOldYellowCard = $(cardButton).attr("data-old-yellow-card") === "true";
    let newCard = "";
    if (currentCard === "" && hasOldYellowCard) {
      newCard = "red";
    } else if (currentCard === "") {
      newCard = "yellow";
    } else if (currentCard === "yellow") {
      newCard = "red";
    }
    const alliance = $(cardButton).attr("data-alliance");
    websocket.send("card", {Alliance: alliance, TeamId: parseInt($(cardButton).attr("data-team")), Card: newCard});
    if (isPlayoff) {
      // The server applies the card to every team on the alliance.
      $(`#${alliance}Cards .team-card`).attr("data-card", newCard);
    } else {
      $(cardButton).attr("data-card", newCard);
    }
    return;
  }

  // Toggle bypass.
  const isDisabled = $(cardButton).hasClass("bypassed-status");
  const team = $(cardButton).attr("data-team");
  $("#confirmBypassTitle").text(`${isDisabled ? "Enable" : "Disable"} ${team}?`);
  $("#confirmBypassAction").text(isDisabled ? "Enable" : "Disable")
  $("#confirmBypass").attr("data-station", $(cardButton).attr("data-station")?.toUpperCase());

  if (team === "0") {
    toggleBypass();
  } else {
    $("#confirmBypass").modal("show");
  }
};

const toggleBypass = function () {
  const station = $("#confirmBypass").attr("data-station");
  websocket.send("toggleBypass", station);
}

// Sends a websocket message to signal to the volunteers that they may enter the field.
var signalVolunteers = function () {
  websocket.send("signalVolunteers");
};

// Sends a websocket message to signal to the teams that they may enter the field.
var signalReset = function () {
  websocket.send("signalReset");
};

// Returns the cards that will be issued when the match is committed, as a list of {text, isDq} objects.
const getPendingCardDescriptions = function () {
  const descriptions = [];
  if (isPlayoff) {
    // Cards are per alliance in playoffs, so describe each alliance once rather than listing all of its teams.
    for (const alliance of ["red", "blue"]) {
      const cards = $(`#${alliance}Cards .team-card`).map((i, button) => latestCards[$(button).attr("data-team")]).get();
      const allianceName = `Alliance ${playoffAllianceNumbers[alliance]} (${alliance === "red" ? "Red" : "Blue"})`;
      if (cards.includes("red")) {
        descriptions.push({text: `${allianceName}: RED CARD – disqualified from this match`, isDq: true});
      } else if (cards.includes("yellow")) {
        descriptions.push({text: `${allianceName}: Yellow card`, isDq: false});
      }
    }
    return descriptions;
  }

  for (const card of ["yellow", "red"]) {
    const teams = Object.entries(latestCards).filter(([, value]) => value === card).map(([teamId]) => teamId);
    if (teams.length > 0) {
      descriptions.push({text: `${card === "yellow" ? "Yellow" : "Red (DQ)"}: ${teams.join(", ")}`, isDq: card === "red"});
    }
  }
  return descriptions;
};

// Shows a confirmation modal if anything needs a second look, otherwise directly commits and posts.
var confirmCommit = function () {
  const items = [];
  if (!scoreIsReady && latestScoringStatus !== null) {
    const red = latestScoringStatus.PositionStatuses["red"];
    const blue = latestScoringStatus.PositionStatuses["blue"];
    items.push(
      `Not all scoring tablets have committed (Red ${red.NumPanelsReady}/${red.NumPanels}, ` +
      `Blue ${blue.NumPanelsReady}/${blue.NumPanels}).`
    );
  }
  for (const description of getPendingCardDescriptions()) {
    const text = `Card to be issued &ndash; ${description.text}`;
    items.push(description.isDq ? `<strong class="text-danger">${text}</strong>` : text);
  }

  if (items.length === 0) {
    commitAndPost();
    return;
  }
  $("#confirmCommitItems").html(items.map(item => `<li>${item}</li>`).join(""));
  $("#confirmCommit").modal("show");
};

// Commits the score and posts results to the audience.
var commitAndPost = function () {
  websocket.send("commitAndPost");
};

// Toggles the simulated FTA ready PLC input.
var toggleFtaReady = function () {
  websocket.send("toggleFtaReady");
};

// Handles a websocket message to update the teams for the current match.
var handleMatchLoad = function (data) {
  $("#matchName").text(data.Match.LongName);
  hideUndoToast();
  knownFoulIds = null;

  isPlayoff = data.Match.Type === matchTypePlayoff;
  playoffAllianceNumbers = {red: data.Match.PlayoffRedAlliance, blue: data.Match.PlayoffBlueAlliance};
  $("#cards").attr("data-playoff", isPlayoff);
  $("#redAllianceLabel").text(isPlayoff ? `Alliance ${data.Match.PlayoffRedAlliance}` : "");
  $("#blueAllianceLabel").text(isPlayoff ? `Alliance ${data.Match.PlayoffBlueAlliance}` : "");

  // In playoffs a yellow card belongs to the whole alliance, including any members who are sitting out this match.
  const allianceHasYellowCard = function (stations, offFieldTeams) {
    const teams = stations.map(station => data.Teams[station]).concat(offFieldTeams || []);
    return teams.some(team => team?.YellowCard);
  };
  const redYellowCard = isPlayoff ? allianceHasYellowCard(["R1", "R2", "R3"], data.RedOffFieldTeams) : null;
  const blueYellowCard = isPlayoff ? allianceHasYellowCard(["B1", "B2", "B3"], data.BlueOffFieldTeams) : null;

  setTeamCard("red", 1, data.Teams["R1"], redYellowCard);
  setTeamCard("red", 2, data.Teams["R2"], redYellowCard);
  setTeamCard("red", 3, data.Teams["R3"], redYellowCard);
  setTeamCard("blue", 1, data.Teams["B1"], blueYellowCard);
  setTeamCard("blue", 2, data.Teams["B2"], blueYellowCard);
  setTeamCard("blue", 3, data.Teams["B3"], blueYellowCard);

  $("#redScoreSummary .team-1").text(data.Teams["R1"]?.Id || "");
  $("#redScoreSummary .team-2").text(data.Teams["R2"]?.Id || "");
  $("#redScoreSummary .team-3").text(data.Teams["R3"]?.Id || "");
  $("#blueScoreSummary .team-1").text(data.Teams["B1"]?.Id || "");
  $("#blueScoreSummary .team-2").text(data.Teams["B2"]?.Id || "");
  $("#blueScoreSummary .team-3").text(data.Teams["B3"]?.Id || "");
};

// Handles a websocket message to update the match status.
const handleMatchTime = function (data) {
  const matchState = matchStates[data.MatchState];
  isPostMatch = matchState === "POST_MATCH";
  $(".control-button").attr("data-enabled", isPostMatch);

  let title = isPlayoff ? "Alliance Cards" : "Red/Yellow Cards";
  let hint = isPlayoff ? "Cards apply to the whole alliance · Red = alliance DQ" : "Tap a team to issue a card";
  let mode = "card";
  if (!isPostMatch) {
    if (matchState === "PRE_MATCH") {
      title = "Bypass";
      hint = "Tap a team to bypass its station";
      mode = "bypass";
    } else {
      title = "Disable";
      hint = "Tap a team to DISABLE its robot";
      mode = "disable";
    }
  }
  $("#teamTitle").text(title);
  $("#cardsHint").text(hint);
  $("#cards").attr("data-mode", mode);
  $("body").attr("data-match-state", matchState);

  if (matchTiming !== undefined) {
    translateMatchTime(data, function (matchState, matchStateText, countdownSec) {
      $("#matchState").text(matchStateText);
      $("#matchTimer").text(getCountdownString(countdownSec));
    });
  }
  updatePostMatchChecklist();
};

const towerStatusNames = [
  "None",
  "Level 1",
  "Level 2",
  "Level 3",
];

const setTowerStatus = function (selector, status) {
  $(selector).text(towerStatusNames[status]);
  $(selector).attr("data-status", status);
};

// Handles a websocket message to update the realtime scoring fields.
const handleRealtimeScore = function (data) {
  latestCards = Object.assign({}, data.RedCards, data.BlueCards);
  for (const [teamId, card] of Object.entries(latestCards)) {
    $(`[data-team="${teamId}"]`).attr("data-card", card);
  }

  const newRedFoulsHashCode = hashObject(data.Red.Score.Fouls);
  const newBlueFoulsHashCode = hashObject(data.Blue.Score.Fouls);
  if (newRedFoulsHashCode !== redFoulsHashCode || newBlueFoulsHashCode !== blueFoulsHashCode) {
    redFoulsHashCode = newRedFoulsHashCode;
    blueFoulsHashCode = newBlueFoulsHashCode;
    fetchFoulList();
  }

  // Fouls committed by one alliance award points to the other.
  setFoulTotals("#redFoulTotals", "Red", data.Red.Score.Fouls, data.Blue.ScoreSummary.FoulPoints, "Blue");
  setFoulTotals("#blueFoulTotals", "Blue", data.Blue.Score.Fouls, data.Red.ScoreSummary.FoulPoints, "Red");

  for (const [alliance, score] of [["red", data.Red.Score], ["blue", data.Blue.Score]]) {
    const scoreRoot = `${alliance}ScoreSummary`;
    setTowerStatus(`#${scoreRoot} .team-1-auto-tower`, score.AutoTowerStatuses[0]);
    setTowerStatus(`#${scoreRoot} .team-2-auto-tower`, score.AutoTowerStatuses[1]);
    setTowerStatus(`#${scoreRoot} .team-3-auto-tower`, score.AutoTowerStatuses[2]);
    setTowerStatus(`#${scoreRoot} .team-1-endgame-tower`, score.EndgameTowerStatuses[0]);
    setTowerStatus(`#${scoreRoot} .team-2-endgame-tower`, score.EndgameTowerStatuses[1]);
    setTowerStatus(`#${scoreRoot} .team-3-endgame-tower`, score.EndgameTowerStatuses[2]);
  }
  updatePostMatchChecklist();
}

// Updates the summary of the number of fouls against an alliance and the points they give the opponent.
const setFoulTotals = function (selector, allianceName, fouls, opponentFoulPoints, opponentName) {
  // The server sends null rather than an empty list when there are no fouls.
  fouls = fouls || [];
  const numMajor = fouls.filter(foul => foul.IsMajor).length;
  const numMinor = fouls.length - numMajor;
  $(selector).text(`${allianceName}: ${numMinor} minor / ${numMajor} major (+${opponentFoulPoints} ${opponentName})`);
};

// Requests the latest rendering of the foul list from the server.
const fetchFoulList = function () {
  const requestId = ++foulListRequestId;
  fetch("/panels/referee/foul_list")
    .then(response => response.text())
    .then(html => {
      // Ignore responses that arrive after a newer request was made.
      if (requestId === foulListRequestId) {
        renderFoulList(html);
      }
    });
};

// Replaces the foul list with the given HTML, unless a rule dropdown is in use, in which case the update is deferred
// until the dropdown loses focus so that it doesn't close in the referee's hand.
const renderFoulList = function (html) {
  if ($(document.activeElement).is("#foulList select")) {
    pendingFoulListHtml = html;
    return;
  }
  pendingFoulListHtml = null;
  $("#foulList").html(html);

  // Highlight any fouls that have appeared since the last render.
  const foulIds = new Set();
  $("#foulList .foul").each(function () {
    const foulId = $(this).attr("data-foul-id");
    foulIds.add(foulId);
    if (knownFoulIds !== null && !knownFoulIds.has(foulId)) {
      $(this).addClass("new-foul");
    }
  });
  knownFoulIds = foulIds;
};

// Handles a websocket message to update the scoring commit status.
const handleScoringStatus = function (data) {
  latestScoringStatus = data;
  if (data.RefereeScoreReady) {
    $("#commitButton").attr("data-enabled", false);
  }
  updateScoreStatus(data, "red", "#redScoreStatus", "Red");
  updateScoreStatus(data, "blue", "#blueScoreStatus", "Blue");

  scoreIsReady = Object.values(data.PositionStatuses).every(status => status.Ready);

  // Make the button visually distinct if not all refs have committed.
  // HR can still press the button with confirm modal.
  $("#commitButton").toggleClass("disabled", !scoreIsReady);

  const numHead = data.NumHeadRefereePanels;
  const numRef = data.NumRefereePanels;
  $("#refCount").text(`Head Ref panels: ${numHead} · Ref panels: ${numRef}`);
  $("#refCount").attr("data-warning", numHead > 1);
  updatePostMatchChecklist();
}

const handleArenaStatus = function (data) {
  setTeamBypassedStatus("red1", data.AllianceStations["R1"]?.Bypass);
  setTeamBypassedStatus("red2", data.AllianceStations["R2"]?.Bypass);
  setTeamBypassedStatus("red3", data.AllianceStations["R3"]?.Bypass);
  setTeamBypassedStatus("blue1", data.AllianceStations["B1"]?.Bypass);
  setTeamBypassedStatus("blue2", data.AllianceStations["B2"]?.Bypass);
  setTeamBypassedStatus("blue3", data.AllianceStations["B3"]?.Bypass);
  $("#ftaReadyButton").attr("data-ready", data.IsFtaReady);
  $("#ftaReadyButton").text(data.IsFtaReady ? "FTA Ready" : "FTA Not Ready");
};

// Handles a websocket message indicating what the alliance station displays are showing (e.g. count or reset).
const handleAllianceStationDisplayMode = function (data) {
  allianceStationDisplayMode = data;
  updatePostMatchChecklist();
};

// Updates the head referee's post-match checklist.
const updatePostMatchChecklist = function () {
  $("#postMatchChecklist").attr("data-visible", isPostMatch);

  const countDone = allianceStationDisplayMode === "signalCount" || allianceStationDisplayMode === "fieldReset";
  $("#checklistCount").attr("data-done", countDone);
  $("#checklistReset").attr("data-done", allianceStationDisplayMode === "fieldReset");

  $("#checklistTablets").attr("data-done", scoreIsReady);
  if (latestScoringStatus !== null) {
    const red = latestScoringStatus.PositionStatuses["red"];
    const blue = latestScoringStatus.PositionStatuses["blue"];
    $("#checklistTablets .checklist-text").text(
      `Tablets: Red ${red.NumPanelsReady}/${red.NumPanels}, Blue ${blue.NumPanelsReady}/${blue.NumPanels}`
    );
  }

  const cardDescriptions = getPendingCardDescriptions();
  $("#checklistCards").attr("data-has-cards", cardDescriptions.length > 0);
  $("#checklistCards").attr("data-has-dq", cardDescriptions.some(description => description.isDq));
  $("#checklistCards .checklist-text").text(
    cardDescriptions.length > 0 ? cardDescriptions.map(description => description.text).join(" · ") : "No cards"
  );
};

const setTeamBypassedStatus = function (station, bypassed) {
  const cardButton = $(`#${station}Card`);
  cardButton.toggleClass("bypassed-status", bypassed && !isPostMatch);
}

// Helper function to update a badge that shows scoring panel commit status.
const updateScoreStatus = function (data, position, element, displayName) {
  const status = data.PositionStatuses[position];
  $(element).text(`${displayName} ${status.NumPanelsReady}/${status.NumPanels}`);
  $(element).attr("data-present", status.NumPanels > 0);
  $(element).attr("data-ready", status.Ready);
};

// Populates the red/yellow card button for a given team. If allianceYellowCard is non-null (in playoffs), it overrides
// the team's own yellow card status.
const setTeamCard = function (alliance, position, team, allianceYellowCard) {
  const cardButton = $(`#${alliance}${position}Card`);
  if (team === null) {
    cardButton.text("-");
    cardButton.attr("data-team", 0)
    cardButton.attr("data-old-yellow-card", "");
  } else {
    cardButton.text(team.Id);
    cardButton.attr("data-team", team.Id)
    cardButton.attr("data-old-yellow-card", allianceYellowCard ?? team.YellowCard);
  }
  cardButton.attr("data-card", "");
}

// Produces a hash code of the given object for use in equality comparisons.
const hashObject = function (object) {
  const s = JSON.stringify(object);
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = Math.imul(31, h) + s.charCodeAt(i) | 0;
  }
  return h;
}

// Keeps the tablet's screen from sleeping while the panel is open, where supported.
const requestWakeLock = function () {
  if ("wakeLock" in navigator && document.visibilityState === "visible") {
    navigator.wakeLock.request("screen").catch(err => console.log(`Wake lock unavailable: ${err}`));
  }
};

$(function () {
  // Read the configuration for this display from the URL query string.
  const urlParams = new URLSearchParams(window.location.search);
  isHeadReferee = urlParams.get("hr") !== "false";
  $(".headRef-dependent").attr("data-hr", isHeadReferee);

  // Apply any foul list update that was deferred while a rule dropdown was open.
  $("#foulList").on("focusout", "select", function () {
    setTimeout(function () {
      if (pendingFoulListHtml !== null) {
        renderFoulList(pendingFoulListHtml);
      }
    }, 0);
  });

  requestWakeLock();
  document.addEventListener("visibilitychange", requestWakeLock);

  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/panels/referee/websocket", {
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
    realtimeScore: function (event) {
      handleRealtimeScore(event.data);
    },
    scoringStatus: function (event) {
      handleScoringStatus(event.data);
    },
    arenaStatus: function (event) {
      handleArenaStatus(event.data);
    },
    allianceStationDisplayMode: function (event) {
      handleAllianceStationDisplayMode(event.data);
    },
  });
  websocket.onConnectionChange = function (isConnected) {
    $("body").attr("data-connected", isConnected);
  };
});
