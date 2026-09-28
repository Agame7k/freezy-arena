// Copyright 2014 Team 254. All Rights Reserved.
// Author: nick@team254.com (Nick Eyre)
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side methods for the rankings display.

var websocket;
var initialDwellMs = 3000;  // How long the display waits upon initial load before scrolling.
var scrollMsPerRow;  // How long in milliseconds it takes to scroll a height of one row.
var staticUpdateIntervalMs = 10000;  // How long between updates if not scrolling.
var standingsTemplate = Handlebars.compile($("#standingsTemplate").html());
var rankingsData;
var prevHighestPlayedMatch;

// Which conference to show: "all", a conference ID, or "alternate" to switch between conferences after each pass.
var conferenceMode = "all";
var alternateConferences = ["1", "2"];
var alternateIndex = 0;

// Returns the rankings API URL for the conference that should be shown next.
var rankingsUrl = function () {
  if (conferenceMode === "alternate") {
    return "/api/rankings?conference=" + alternateConferences[alternateIndex];
  }
  return "/api/rankings?conference=" + conferenceMode;
};

// Shows the conference name and color in the title bar.
var showConference = function (data) {
  if (data.Conference) {
    $("#conferenceName").text(data.Conference.Name + " ");
    $("#titlebar").css("border-bottom", "8px solid " + data.Conference.Color);
    $("#rankHeader").text("Conf Rank");
  } else {
    $("#conferenceName").text("");
    $("#titlebar").css("border-bottom", "none");
    $("#rankHeader").text("Rank");
  }
};

// Loads the JSON rankings data from the event server.
var getRankingsData = function (callback) {
  $.getJSON(rankingsUrl(), function (data) {
    rankingsData = data;
    if (callback) {
      callback(data);
    }
  });
};

// Updates the rankings in place and initiates scrolling if they are long enough to require it.
var updateStaticRankings = function () {
  getRankingsData(function () {
    var rankingsHtml = standingsTemplate(rankingsData);
    $("#rankings2").html(rankingsHtml);
    $("#scroller").css("transform", "translate(0px, -2px);");
    prevHighestPlayedMatch = rankingsData.HighestPlayedMatch;
    setHighestPlayedMatch(rankingsData.HighestPlayedMatch);
    if ($("#rankings2").height() > $("#container").height()) {
      // Initiate scrolling.
      setTimeout(cycleRankings, initialDwellMs);
    } else {
      // Rankings are too short; just update in place.
      setTimeout(updateStaticRankings, staticUpdateIntervalMs);
    }
  });
};

// Seamlessly copies the newer table contents to the older one, resets the scrolling, and loads new data.
var cycleRankings = function () {
  // Overwrite the top data with the bottom data and reset the scrolling back up to the top of the top table.
  $("#rankings1").html($("#rankings2").html());
  $("#scroller").css({transform: "translate(0px, -1px);"});

  // Load new data into the now out-of-sight bottom table.
  var rankingsHtml = standingsTemplate(rankingsData);
  $("#rankings2").html(rankingsHtml);

  // Delay updating the "Standings as of" message by one cycle because the tables are always one cycle behind
  // the data loading.
  setHighestPlayedMatch(prevHighestPlayedMatch);
  prevHighestPlayedMatch = rankingsData.HighestPlayedMatch;

  if ($("#rankings1").height() > $("#container").height()) {
    // Kick off another scrolling animation.
    var scrollDistance = $("#rankings1").height() + parseInt($("#rankings1").css("border-bottom-width"));
    var scrollTime = scrollMsPerRow * $("#rankings1 tr").length;
    $("#scroller").transition({y: -scrollDistance}, scrollTime, "linear", cycleRankings);

    // Set the data to be reloaded two seconds before the scrolling terminates.
    var reloadDataTime = Math.max(0, scrollTime - 2000);
    setTimeout(getRankingsData, reloadDataTime);
  } else {
    // The rankings got shorter for whatever reason, so revert to static updating.
    setTimeout(updateStaticRankings, staticUpdateIntervalMs);
  }
};

// Updates the "Standings as of" message with the given value, or blanks it out if there is no data yet.
var setHighestPlayedMatch = function (highestPlayedMatch) {
  if (highestPlayedMatch === "") {
    $("#highestPlayedMatch").text("");
  } else {
    $("#highestPlayedMatch").text("Standings as of " + highestPlayedMatch);
  }
};

// Handles a websocket message to update the event status message.
var handleEventStatus = function (data) {
  $("#earlyLateMessage").text(data.EarlyLateMessage);
};

$(function () {
  // Read the configuration for this display from the URL query string.
  var urlParams = new URLSearchParams(window.location.search);
  scrollMsPerRow = urlParams.get("scrollMsPerRow");

  // Set up the websocket back to the server. Used only for remote forcing of reloads.
  websocket = new CheesyWebsocket("/displays/rankings/websocket", {
    eventStatus: function (event) {
      handleEventStatus(event.data);
    },
  });

  var multiConference = $("#column").data("multi-conference") === true;
  conferenceMode = urlParams.get("conference") || (multiConference ? "alternate" : "all");
  if (conferenceMode === "auto") {
    // In a multi-conference event, show both conferences side by side unless another mode was chosen.
    conferenceMode = multiConference ? "split" : "all";
  }
  if (conferenceMode === "split" && multiConference) {
    runSplitRankings();
  } else if (conferenceMode === "alternate" && multiConference) {
    runAlternateRankings();
  } else {
    if (conferenceMode === "alternate" || conferenceMode === "split") {
      conferenceMode = "all";
    }
    getRankingsData(showConference);
    updateStaticRankings();
  }
});

// Shows one conference's full list (scrolling if needed), then switches to the other conference.
var runAlternateRankings = function () {
  getRankingsData(function (data) {
    showConference(data);
    $("#rankings1").html(standingsTemplate(data));
    $("#rankings2").html("");
    setHighestPlayedMatch(data.HighestPlayedMatch);
    $("#scroller").css({transform: "translate(0px, 0px)"});
    var next = function () {
      alternateIndex = (alternateIndex + 1) % alternateConferences.length;
      $("#scroller").fadeOut(500, function () {
        $("#scroller").css({transform: "translate(0px, 0px)"}).fadeIn(500);
        runAlternateRankings();
      });
    };
    var overflow = $("#rankings1").height() - $("#container").height();
    if (overflow > 0) {
      setTimeout(function () {
        var scrollTime = scrollMsPerRow * $("#rankings1 tr").length;
        $("#scroller").transition({y: -overflow}, scrollTime, "linear", function () {
          setTimeout(next, initialDwellMs);
        });
      }, initialDwellMs);
    } else {
      setTimeout(next, staticUpdateIntervalMs);
    }
  });
};

// Shows both conferences' standings side by side, each scrolling independently through its full list.
var runSplitRankings = function () {
  $("#standings").hide();
  $("#titlebar").hide();
  $("#splitStandings").css("display", "flex");
  $(".split-column").each(function () {
    runSplitColumn($(this));
  });
};

var splitRowsHtml = function (rankings) {
  return rankings.map(function (ranking) {
    return `<tr><td>${ranking.DisplayRank}</td><td>${ranking.TeamId}</td>` +
      `<td class="name">${$("<div>").text(ranking.Nickname || "").html()}</td><td>${ranking.RankingPoints}</td>` +
      `<td>${ranking.Wins}-${ranking.Losses}-${ranking.Ties}</td><td>${ranking.Played}</td>` +
      `<td>${ranking.Rank}</td></tr>`;
  }).join("");
};

var runSplitColumn = function (column) {
  $.getJSON("/api/rankings?conference=" + column.data("conference"), function (data) {
    const conference = data.Conference || {Name: "Conference " + column.data("conference"), Color: "#333"};
    column.html(
      `<div class="split-title" style="background-color: ${conference.Color};">${$("<div>").text(conference.Name).html()}` +
      ` Standings</div>` +
      `<table class="split-header"><tr><td>Rank</td><td>Team</td><td class="name">Name</td><td>RP</td>` +
      `<td>W-L-T</td><td>Pld</td><td>Ovr</td></tr></table>` +
      `<div class="split-body"><div class="split-scroller"><table class="table table-striped split-table">` +
      `<tbody>${splitRowsHtml(data.Rankings)}</tbody></table></div></div>`
    );
    setHighestPlayedMatch(data.HighestPlayedMatch);
    const scroller = column.find(".split-scroller");
    const overflow = scroller.height() - column.find(".split-body").height();
    const again = function () {
      runSplitColumn(column);
    };
    if (overflow > 0) {
      setTimeout(function () {
        scroller.transition({y: -overflow}, scrollMsPerRow * data.Rankings.length, "linear", function () {
          setTimeout(again, initialDwellMs);
        });
      }, initialDwellMs);
    } else {
      setTimeout(again, staticUpdateIntervalMs);
    }
  });
};
