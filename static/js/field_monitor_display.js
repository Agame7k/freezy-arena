// Copyright 2018 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the field monitor display.

let websocket;
let currentMatchId;
let redSide;
let blueSide;
let latestRealtimeScore;
let matchTimeRemainingSec;
const lowBatteryThreshold = 8;
const highBtuThreshold = 7.0;

// FTA mode shows a readiness strip; faults are logged by the server and shown on the FTA console.
let isFtaMode = false;
const matchStateStartMatch = 1;
const matchStatePostMatch = 5;

// Briefly flashes an element to draw the eye to a status that just went bad, so a mid-match disconnect or E-stop
// is impossible to miss without permanently blinking for as long as it stays bad.
const flashFault = function (element) {
  element.removeClass("fault-flash");
  void element[0].offsetWidth; // Force a reflow so the animation can be retriggered.
  element.addClass("fault-flash");
};

// Sets data-status-ok on an element, flashing it if this is a transition from a previously-known-good state into a
// bad one. Doesn't flash on the very first update (e.g. nothing being connected yet before a match starts), since
// that's not a fault -- only an actual working connection dropping counts.
const setStatusOk = function (element, ok) {
  const isGoodNow = ok === true || ok === "true";
  const wasGood = element.attr("data-last-ok") === "true";
  if (wasGood && !isGoodNow) {
    flashFault(element);
  }
  element.attr("data-last-ok", isGoodNow ? "true" : "false");
  element.attr("data-status-ok", ok);
};

// Sets data-status on the team ID box, flashing it if the robot just dropped out of a full connection.
const setTeamStatus = function (element, status) {
  const isFullyLinkedNow = status === "robot-linked";
  const wasFullyLinked = element.attr("data-last-status") === "robot-linked";
  if (wasFullyLinked && !isFullyLinkedNow) {
    flashFault(element);
  }
  element.attr("data-last-status", status);
  element.attr("data-status", status);
};

const handleArenaStatus = function (data) {
  // If getting data for the wrong match (e.g. after a server restart), reload the page.
  if (currentMatchId == null) {
    currentMatchId = data.MatchId;
  } else if (currentMatchId !== data.MatchId) {
    location.reload();
  }

  if (isFtaMode) {
    updateReadiness(data);
  }

  $.each(data.AllianceStations, function (station, stationStatus) {
    // Select the DOM elements corresponding to the team station.
    let teamElementPrefix;
    if (station[0] === "R") {
      teamElementPrefix = "#" + redSide + "Team" + station[1];
    } else {
      teamElementPrefix = "#" + blueSide + "Team" + station[1];
    }
    const teamIdElement = $(teamElementPrefix + "Id");
    const teamNotesElement = $(teamElementPrefix + "Notes");
    const teamNotesTextElement = $(teamElementPrefix + "Notes div");
    const teamEthernetElement = $(teamElementPrefix + "Ethernet");
    const teamDsElement = $(teamElementPrefix + "Ds");
    const teamRadioElement = $(teamElementPrefix + "Radio");
    const teamRadioIconElement = $(teamElementPrefix + "Radio i");
    const teamRobotElement = $(teamElementPrefix + "Robot");
    const teamBatteryElement = $(teamElementPrefix + "Battery");
    const teamBypassElement = $(teamElementPrefix + "Bypass");
    const teamStatsElement = $(teamElementPrefix + "Stats");
    const teamBandwidthElement = $(teamElementPrefix + "Bandwidth");
    const teamTripTimeElement = $(teamElementPrefix + "TripTime");
    const teamMissedPacketsElement = $(teamElementPrefix + "MissedPackets");

    teamNotesTextElement.attr("data-station", station);

    if (stationStatus.Team) {
      // Set the team number and status.
      teamIdElement.text(stationStatus.Team.Id);
      let status = "no-link";
      if (stationStatus.Bypass) {
        status = "";
      } else if (stationStatus.DsConn) {
        if (stationStatus.DsConn.WrongStation) {
          status = "wrong-station";
        } else if (stationStatus.DsConn.RobotLinked) {
          status = "robot-linked";
        } else if (stationStatus.DsConn.RioLinked) {
          status = "rio-linked";
        } else if (stationStatus.DsConn.RadioLinked) {
          status = "radio-linked";
        } else if (stationStatus.DsConn.DsLinked) {
          status = "ds-linked";
        }
      }
      setTeamStatus(teamIdElement, status);
      teamNotesTextElement.text(stationStatus.Team.FtaNotes);
      teamNotesElement.attr("data-status", status);
    } else {
      // No team is present in this position for this match; blank out the status.
      teamIdElement.text("");
      teamNotesTextElement.text("");
      teamNotesElement.attr("data-status", "");
    }

    // Format the Ethernet status box.
    setStatusOk(teamEthernetElement, stationStatus.Ethernet ? "true" : "");
    if (stationStatus.DsConn && stationStatus.DsConn.DsRobotTripTimeMs > 0) {
      teamEthernetElement.text(stationStatus.DsConn.DsRobotTripTimeMs);
    } else {
      teamEthernetElement.text("ETH");
    }

    const wifiStatus = stationStatus.WifiStatus;
    teamRadioIconElement.attr("class", `bi-reception-${wifiStatus.ConnectionQuality}`);

    $("#accessPointStatus").attr("data-status", data.AccessPointStatus);
    $("#switchStatus").attr("data-status", data.SwitchStatus);

    if (stationStatus.DsConn) {
      // Format the driver station status box.
      const dsConn = stationStatus.DsConn;
      setStatusOk(teamDsElement, dsConn.DsLinked);
      teamDsElement.text(dsConn.MissedPacketCount);

      // Format the radio status box according to the connection status of the robot radio.
      const radioOkay = stationStatus.Team && stationStatus.Team.Id === wifiStatus.TeamId &&
        (wifiStatus.RadioLinked || dsConn.RobotLinked);
      setStatusOk(teamRadioElement, radioOkay);

      // Format the robot status box.
      const rioOkay = dsConn.RobotLinked;
      setStatusOk(teamRobotElement, rioOkay);
      if (stationStatus.DsConn.SecondsSinceLastRobotLink > 1 && stationStatus.DsConn.SecondsSinceLastRobotLink < 1000) {
        teamRobotElement.text(stationStatus.DsConn.SecondsSinceLastRobotLink.toFixed());
      } else {
        teamRobotElement.text("RIO");
      }
      const batteryOkay = dsConn.BatteryVoltage > lowBatteryThreshold && dsConn.RobotLinked;
      setStatusOk(teamBatteryElement, batteryOkay);
      teamBatteryElement.text(dsConn.BatteryVoltage.toFixed(1) + "V");

      const btuOkay = wifiStatus.MBits < highBtuThreshold && dsConn.RobotLinked;
      setStatusOk(teamStatsElement, btuOkay);
      if (wifiStatus.MBits >= 0.01) {
        teamBandwidthElement.text(wifiStatus.MBits.toFixed(2));
        teamTripTimeElement.text(dsConn.DsRobotTripTimeMs);
        teamMissedPacketsElement.text(dsConn.MissedPacketCount);
      } else {
        teamBandwidthElement.text("-");
        teamTripTimeElement.text("-");
        teamMissedPacketsElement.text("-");
      }
    } else {
      setStatusOk(teamDsElement, "");
      teamDsElement.text("DS");
      setStatusOk(teamRobotElement, "");
      teamRobotElement.text("RIO");
      teamBatteryElement.text("0.0V");
      teamBandwidthElement.text("-");
      teamTripTimeElement.text("-");
      teamMissedPacketsElement.text("-");

      // Format the robot status box according to whether the AP is configured with the correct SSID.
      const expectedTeamId = stationStatus.Team ? stationStatus.Team.Id : 0;
      if (wifiStatus.TeamId === expectedTeamId) {
        if (wifiStatus.RadioLinked) {
          setStatusOk(teamRadioElement, true);
        } else {
          setStatusOk(teamRadioElement, "");
        }
      } else {
        setStatusOk(teamRadioElement, false);
      }
    }

    if (stationStatus.EStop) {
      setStatusOk(teamBypassElement, false);
      teamBypassElement.text("E-STP");
    } else if (stationStatus.AStop) {
      setStatusOk(teamBypassElement, true);
      teamBypassElement.text("A-STP");
    } else if (stationStatus.Bypass) {
      setStatusOk(teamBypassElement, false);
      teamBypassElement.text("BYP");
    } else {
      setStatusOk(teamBypassElement, true);
      teamBypassElement.text("");
    }
  });
};

// Handles a websocket message to update the match time countdown.
const handleMatchTime = function (data) {
  translateMatchTime(data, function (matchState, matchStateText, countdownSec) {
    matchTimeRemainingSec = countdownSec;
    $("#matchState").text(matchStateText);
    $("#matchTime").text(countdownSec);
    $("#matchTimeAllianceStation").text(countdownSec);
    updateAllianceStationMatchState();
    if (matchStateText === "PRE-MATCH" || matchStateText === "POST-MATCH") {
      $(".ds-dependent").attr("data-preMatch", "true");
    } else {
      $(".ds-dependent").attr("data-preMatch", "false");
    }
  });
};

const updateAllianceStationMatchState = function () {
  if (!latestRealtimeScore) {
    return;
  }

  const redActiveRemainingSec = latestRealtimeScore.Red.ActiveRemainingSec;
  const blueActiveRemainingSec = latestRealtimeScore.Blue.ActiveRemainingSec;
  let matchStateText = latestRealtimeScore.MatchState;
  let activeAlliance = "";

  if (latestRealtimeScore.MatchState === 2) {
    matchStateText = "Auto";
  } else if (latestRealtimeScore.MatchState === 4) {
    const redActive = redActiveRemainingSec > 0;
    const blueActive = blueActiveRemainingSec > 0;
    if (redActive && blueActive) {
      const activeRemainingSec = Math.max(redActiveRemainingSec, blueActiveRemainingSec);
      matchStateText = matchTimeRemainingSec === activeRemainingSec ? "End Game" : "Transition";
    } else if (redActive) {
      matchStateText = "Red Active";
      activeAlliance = "red";
    } else if (blueActive) {
      matchStateText = "Blue Active";
      activeAlliance = "blue";
    }
  }

  $("#matchStateAllianceStation")
    .attr("data-active-alliance", activeAlliance)
    .text(matchStateText);
};

// Handles a websocket message to play a sound to signal match start/stop/etc.
const handlePlaySound = function(sound) {
  $("audio").each(function(k, v) {
    // Stop and reset any sounds that are still playing.
    v.pause();
    v.currentTime = 0;
  });
  $("#sound-" + sound)[0].play();
};

// Handles a websocket message to update the match score.
const handleRealtimeScore = function (data, reversed) {
  latestRealtimeScore = data;
  const allianceScore = reversed === "true" ? data.Blue : data.Red;
  const activeRemainingSec = Math.max(data.Red.ActiveRemainingSec, data.Blue.ActiveRemainingSec);
  updateAllianceStationMatchState();
  $("#activeRemainingSecAllianceStation").text(activeRemainingSec);
  $("#fuelNumeratorAllianceStation").text(
    allianceScore.ScoreSummary.NumFuel - allianceScore.ScoreSummary.NumFuelPostMatch
  );
  $("#fuelDenominatorAllianceStation").text(allianceScore.ScoreSummary.NumFuelGoal);

  const autoWinnerElement = $("#autoWinnerAllianceStation");
  if (data.Red.Score.Hub.WonAuto) {
    autoWinnerElement.attr("data-alliance", "red").text("Red Won Auto").prop("hidden", false);
  } else if (data.Blue.Score.Hub.WonAuto) {
    autoWinnerElement.attr("data-alliance", "blue").text("Blue Won Auto").prop("hidden", false);
  } else {
    autoWinnerElement.attr("data-alliance", "").text("").prop("hidden", true);
  }

  if (reversed === "true") {
    $("#rightScore").text(data.Red.ScoreSummary.Score);
    $("#leftScore").text(data.Blue.ScoreSummary.Score);
    $("#rightScoreAllianceDisplay").text(data.Blue.ScoreSummary.Score);
    $("#leftScoreAllianceDisplay").text(data.Red.ScoreSummary.Score);
  } else {
    $("#rightScore").text(data.Blue.ScoreSummary.Score);
    $("#leftScore").text(data.Red.ScoreSummary.Score);
    $("#rightScoreAllianceDisplay").text(data.Red.ScoreSummary.Score);
    $("#leftScoreAllianceDisplay").text(data.Blue.ScoreSummary.Score);
  }
};

// Handles a websocket message to update current match
const handleMatchLoad = function (data) {
  $("#matchName").text(data.Match.LongName);
};

// Handles a websocket message to update the event status message.
const handleEventStatus = function (data) {
  if (data.CycleTime === "") {
    $("#cycleTimeMessage").text("Last cycle time: Unknown");
  } else {
    $("#cycleTimeMessage").text("Last cycle time: " + data.CycleTime);
  }
  $("#earlyLateMessage").text(data.EarlyLateMessage);
};

// Makes the team notes section editable and handles saving edits to the server. Enter saves, Shift+Enter inserts a
// newline, and Escape discards the edit.
const editFtaNotes = function (element) {
  const teamNotesTextElement = $(element);
  const originalText = teamNotesTextElement.text();
  const textArea = $("<textarea />");
  textArea.val(originalText);
  teamNotesTextElement.replaceWith(textArea);
  textArea.focus();
  textArea.on("keydown", function (event) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      textArea.blur();
    } else if (event.key === "Escape") {
      textArea.val(originalText);
      textArea.blur();
    }
  });
  textArea.blur(function () {
    textArea.replaceWith(teamNotesTextElement);
    if (textArea.val() !== originalText) {
      teamNotesTextElement.text(textArea.val());
      websocket.send("updateTeamNotes", {station: teamNotesTextElement.attr("data-station"), notes: textArea.val()});
    }
  });
};

// Shows whether the match can be started and, if not, exactly what is blocking it.
const updateReadiness = function (data) {
  const readiness = $("#ftaReadiness");
  const text = $("#ftaReadinessText");
  if (data.MatchState >= matchStateStartMatch && data.MatchState < matchStatePostMatch) {
    readiness.attr("data-state", "running");
    text.text("Match in progress");
  } else if (data.MatchState === matchStatePostMatch) {
    readiness.attr("data-state", "running");
    text.text("Post-match — see the FTA console for this match's faults");
  } else if (data.CanStartMatch) {
    readiness.attr("data-state", "ready");
    text.text("✔ Ready to start");
  } else {
    readiness.attr("data-state", "blocked");
    text.text("Not ready: " + data.StartMatchConditions.join(" • "));
  }
  text.attr("title", data.StartMatchConditions ? data.StartMatchConditions.join("\n") : "");
};

$(function () {
  // Read the configuration for this display from the URL query string.
  const urlParams = new URLSearchParams(window.location.search);
  const reversed = urlParams.get("reversed");
  if (reversed === "true") {
    redSide = "right";
    blueSide = "left";
  } else {
    redSide = "left";
    blueSide = "right";
  }

  //Read if display to be used in a Driver Station, ignore FTA flag if so.
  const driverStation = urlParams.get("ds");
  if (driverStation === "true") {
    $(".fta-dependent").attr("data-fta", "false");
    $(".ds-dependent").attr("data-ds", driverStation);
  } else {
    $(".fta-dependent").attr("data-fta", urlParams.get("fta"));
    $(".ds-dependent").attr("data-ds", driverStation);
    isFtaMode = urlParams.get("fta") === "true";
  }
  $("body").toggleClass("fta-mode", isFtaMode);

  $(".reversible-left").attr("data-reversed", reversed);
  $(".reversible-right").attr("data-reversed", reversed);


  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/displays/field_monitor/websocket", {
    arenaStatus: function (event) {
      handleArenaStatus(event.data);
    },
    eventStatus: function (event) {
      handleEventStatus(event.data);
    },
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
    realtimeScore: function (event) {
      handleRealtimeScore(event.data, reversed);
    },
    playSound: function(event) {
      handlePlaySound(event.data);
    },
  });
});
