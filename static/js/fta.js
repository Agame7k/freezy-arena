// Copyright 2026 Team 254. All Rights Reserved.
//
// Client-side logic for the FTA console.

let websocket;
let currentMatchId = null;
let currentMatchName = "";
let matchState = 0;
let lastArenaStatus = null;
let faults = [];
const faultIds = new Set();
let faultFilter = "all";
let openTeamId = null;
let openTeamHistory = null;
let alertsEnabled = false;
let audioContext = null;
let timelineMatchesLoaded = false;
let timelineRefreshTimer = null;
let blockersOpen = false;
let watchlist = null;
const teamSummaries = new Map();
let teamFilter = "flagged";
// The status line for each station, and when it started waiting on something before the match.
const stationLines = {};

const stationIds = ["R1", "R2", "R3", "B1", "B2", "B3"];
const sideTabs = ["faults", "upnext", "teams", "timeline"];
const matchStatePreMatch = 0;
const matchStatePostMatch = 5;
const timelineRefreshMs = 5000;
// A station waiting longer than this is probably holding up the schedule.
const longWaitSec = 120;
const lowSnrDb = 20;
// Match stats are saved as the match ends, so give them a moment before refreshing the watchlist.
const watchlistPostMatchDelayMs = 2000;
const wideLayout = window.matchMedia("(min-width: 960px)");

const noteTagLabels = {
  radio: "Radio",
  ethernet: "Ethernet",
  ds: "DS",
  code: "Code",
  can: "CAN",
  brownout: "Brownout",
  battery: "Battery",
  bumpers: "Bumpers",
  mechanical: "Mechanical",
  other: "Other",
};

// The note category to suggest when adding a note from a fault.
const faultNoteTags = {
  DsLost: "ds",
  DsRestored: "ds",
  RobotLost: "radio",
  RobotRestored: "radio",
  LowBattery: "battery",
  Brownout: "brownout",
  HighTripTime: "radio",
  PacketLoss: "radio",
  WrongStation: "ds",
};

// Each link in the connection chain after the Ethernet cable, in the order they come up.
const chainLinks = {ds: "DsLinked", radio: "RadioLinked", rio: "RioLinked", code: "RobotLinked"};

// Short names for the checklist items, for the "Waiting on" line on each station.
const checkShortNames = {
  "Station": "correct station",
  "Driver station": "DS",
  "Radio": "radio",
  "roboRIO": "RIO",
  "Robot code": "code",
  "E-stop clear": "E-stop release",
  "A-stop reset": "A-stop reset",
};

const isMatchRunning = function () {
  return matchState > matchStatePreMatch && matchState < matchStatePostMatch;
};

const allianceOf = function (station) {
  return station ? station[0] : "";
};

const plural = function (count, noun) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
};

const formatSeconds = function (totalSeconds) {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
};

const formatEventTime = function (event) {
  if (event.MatchTimeSec > 0) {
    return formatSeconds(event.MatchTimeSec);
  }
  return new Date(event.Time).toLocaleTimeString([], {hour: "numeric", minute: "2-digit"});
};

const stationChip = function (station) {
  return $("<span class='chip'>").attr("data-alliance", allianceOf(station)).text(station || "Field");
};

const readSetting = function (key) {
  try {
    return localStorage.getItem(key);
  } catch (e) {
    return null;
  }
};

const saveSetting = function (key, value) {
  try {
    localStorage.setItem(key, value);
  } catch (e) {
    // Storage is unavailable; the setting just won't be remembered.
  }
};

const showToast = function (message, severity) {
  const toast = $("#toast");
  toast.text(message).attr("data-severity", severity || "").prop("hidden", false);
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(function () {
    toast.prop("hidden", true);
  }, 2500);
};

const fetchJson = function (url) {
  return fetch(url, {credentials: "same-origin"}).then(function (response) {
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    return response.json();
  });
};

// Tabs and display options

const showTab = function (tab) {
  // On a wide screen the stations are always shown, so the "field" tab means the fault log.
  if (tab === "field" && wideLayout.matches) {
    tab = "faults";
  }
  $("body").attr("data-tab", tab);
  $("#tabs button").each(function () {
    $(this).attr("aria-selected", $(this).attr("data-tab") === tab ? "true" : "false");
  });
  saveSetting("ftaTab", tab);
  if (tab === "timeline") {
    if (!timelineMatchesLoaded) {
      loadTimelineMatches();
    }
    scheduleTimelineRefresh();
  }
  if ((tab === "upnext" || tab === "teams") && watchlist === null) {
    loadWatchlist();
  }
};

// Switches to a side tab, bringing the side panel back if it was hidden.
const openSideTab = function (tab) {
  setFieldOnly(false);
  showTab(tab);
};

const toggleOptions = function (open) {
  const menu = $("#optionsMenu");
  open = open === undefined ? menu.prop("hidden") : open;
  menu.prop("hidden", !open);
  $("#optionsButton").attr("aria-expanded", open ? "true" : "false");
};

const setSwap = function (swap) {
  $("body").attr("data-swap", swap ? "true" : "false");
  $("#optSwap").prop("checked", swap);
  saveSetting("ftaSwap", swap);
};

const setFieldOnly = function (fieldOnly) {
  $("body").attr("data-field-only", fieldOnly ? "true" : "false");
  $("#optFieldOnly").prop("checked", fieldOnly);
  saveSetting("ftaFieldOnly", fieldOnly);
};

// Arena status

const handleArenaStatus = function (data) {
  lastArenaStatus = data;
  const previousMatchState = matchState;
  matchState = data.MatchState;
  if (currentMatchId !== data.MatchId) {
    currentMatchId = data.MatchId;
    loadCurrentFaults();
    loadWatchlist();
    if (timelineMatchesLoaded) {
      loadTimelineMatches();
    }
  } else if (matchState === matchStatePostMatch && previousMatchState !== matchStatePostMatch) {
    setTimeout(loadWatchlist, watchlistPostMatchDelayMs);
  }

  setHealth("#apHealth", networkHealth(data.AccessPointStatus), `Access point: ${data.AccessPointStatus}`);
  setHealth("#switchHealth", networkHealth(data.SwitchStatus), `Switch: ${data.SwitchStatus}`);
  if (data.PlcIsEnabled) {
    const plcOk = data.PlcIsHealthy && !data.FieldEStop;
    setHealth("#plcHealth", plcOk ? "ok" : "bad", data.FieldEStop ? "PLC: field E-stop" : "PLC");
  } else {
    setHealth("#plcHealth", "", "PLC: not enabled");
  }

  updateStatus(data);
  updateFtaReady(data.IsFtaReady);
  $.each(stationIds, function (i, station) {
    updateStation(station, data.AllianceStations[station], data.FtaStations[station], data.PlcIsEnabled);
  });
  if (openTeamId !== null) {
    updatePanelLive();
  }
};

const networkHealth = function (status) {
  switch (status) {
    case "ACTIVE":
      return "ok";
    case "CONFIGURING":
      return "warn";
    case "ERROR":
      return "bad";
    default:
      return "";
  }
};

const setHealth = function (selector, status, title) {
  $(selector).attr("data-status", status).attr("title", title);
};

// A few words for each start blocker, for the one-line summary in the status strip.
const blockerSummary = function (blocker) {
  const stations = (blocker.Stations || []).join(" ");
  switch (blocker.Code) {
    case "robotNotConnected":
      return `No robot ${stations}`;
    case "eStop":
      return `E-stop ${stations}`;
    case "aStopNotReset":
      return `A-stop not reset ${stations}`;
    case "matchInProgress":
      return "Previous match not committed";
    case "plcUnhealthy":
      return "PLC down";
    case "fieldEStop":
      return "Field E-stop";
    case "ftaNotReady":
      return "FTA switch off";
    default:
      return blocker.Message;
  }
};

// Updates the strip under the top bar saying whether the match can start and, if not, why.
const updateStatus = function (data) {
  const badFaults = faults.filter((fault) => fault.Severity === "bad").length;
  const problems = faults.filter((fault) => fault.Severity !== "good").length;
  let state;
  let label;
  let detail;
  let blockers = [];

  if (isMatchRunning()) {
    state = "running";
    label = "In match";
    detail = badFaults > 0 ? plural(badFaults, "critical fault") : "No critical faults";
  } else if (matchState === matchStatePostMatch) {
    state = "post";
    label = "Post-match";
    detail = problems > 0 ? `${plural(problems, "fault")} logged` : "No faults logged";
  } else if (data.CanStartMatch && !data.IsFtaReady) {
    // Without a PLC nothing blocks the start, but Match Play will warn that the FTA hasn't said the field is ready.
    state = "waiting";
    label = "Robots ready";
    detail = "Waiting on FTA ready";
  } else if (data.CanStartMatch) {
    state = "ready";
    label = "Ready";
    detail = "Clear to start";
  } else {
    state = "blocked";
    label = "Not ready";
    blockers = data.StartMatchBlockers || [];
    detail = blockers.map(blockerSummary).join(", ");
  }

  $("#status").attr({"data-state": state, "data-faults": badFaults > 0, "data-details": blockers.length > 0});
  $("#statusLabel").text(label);
  $("#statusDetail").text(detail);
  renderBlockers(blockers);
  if (blockers.length === 0) {
    toggleBlockers(false);
  }
};

const renderBlockers = function (blockers) {
  const list = $("#blockerList").empty();
  $.each(blockers, function (i, blocker) {
    const item = $("<li class='blocker'>");
    // The stations are shown as buttons, so leave them out of the message.
    const message = blocker.Stations ? blocker.Message.replace(/\s*\([^)]*\)$/, "") : blocker.Message;
    item.append($("<span class='blocker-message'>").text(message));
    if (blocker.Stations) {
      const stations = $("<span class='blocker-stations'>");
      $.each(blocker.Stations, function (j, station) {
        stations.append(
          $("<button type='button' class='chip'>").attr("data-alliance", allianceOf(station)).text(station)
            .on("click", function () {
              toggleBlockers(false);
              openStation(station);
            }),
        );
      });
      item.append(stations);
    }
    if (blocker.Hint) {
      item.append($("<span class='blocker-hint'>").text(blocker.Hint));
    }
    list.append(item);
  });
};

const toggleBlockers = function (open) {
  const hasDetails = $("#status").attr("data-details") === "true";
  blockersOpen = hasDetails && (open === undefined ? !blockersOpen : open);
  $("#blockerList").prop("hidden", !blockersOpen);
  $("#statusSummary").attr("aria-expanded", blockersOpen ? "true" : "false");
};

const updateFtaReady = function (ready) {
  $("#ftaReadyButton").attr({"data-ready": ready, "aria-pressed": ready}).text(ready ? "FTA Ready" : "FTA Not Ready");
};

const toggleFtaReady = function () {
  websocket.send("toggleFtaReady");
};

// Stations

const updateStation = function (station, stationStatus, ftaStatus, plcEnabled) {
  const card = $(`#station${station}`);
  const team = stationStatus.Team;
  const dsConn = stationStatus.DsConn;
  const wifi = stationStatus.WifiStatus;
  const active = Boolean(team) && !stationStatus.Bypass;

  card.attr("data-team-id", team ? team.Id : "");
  card.attr("aria-label", team ? `${station} ${team.Id}` : `${station} empty`);
  card.find(".st-team").text(team ? team.Id : "-");
  card.find(".st-name").text(team ? team.Nickname || "" : "");
  card.find(".st-note").text(team ? team.FtaNotes || "" : "");

  // The connection chain. Links past the first break can't be reached anyway, so only the break itself is red.
  card.find(".chain [data-link='eth']").prop("hidden", !plcEnabled);
  setLink(card, "eth", active && plcEnabled ? stationStatus.Ethernet : null);
  let broken = false;
  $.each(chainLinks, function (link, field) {
    const ok = Boolean(dsConn && dsConn[field]);
    setLink(card, link, !active || (broken && !ok) ? null : ok);
    broken = broken || !ok;
  });

  const flagTypes = new Set(ftaStatus.Flags.map((flag) => flag.Type));
  const linked = Boolean(dsConn && dsConn.RobotLinked);
  card.find(".st-battery").text(linked && dsConn.BatteryVoltage > 0 ? dsConn.BatteryVoltage.toFixed(1) + "V" : "")
    .attr("data-status", flagTypes.has("Brownout") ? "bad" : flagTypes.has("LowBattery") ? "warn" : "");
  setMetric(card, "trip", linked ? dsConn.DsRobotTripTimeMs + "ms" : "-", flagTypes.has("HighTripTime") ? "warn" : "");
  setMetric(card, "missed", dsConn ? dsConn.MissedPacketCount : "-", "");
  const snr = wifi && wifi.RadioLinked && wifi.SignalNoiseRatio > 0 ? wifi.SignalNoiseRatio : null;
  setMetric(card, "snr", snr === null ? "-" : snr, snr !== null && snr < lowSnrDb ? "warn" : "");
  setMetric(card, "bandwidth", wifi && wifi.MBits >= 0.01 ? wifi.MBits.toFixed(1) : "-", "");

  // The status line shows the worst flag and, before the match, what the station is waiting on and for how long.
  const flags = ftaStatus.Flags.slice().sort((a, b) => (b.Severity === "bad") - (a.Severity === "bad"));
  const worstFlag = flags[0];
  const line = {teamId: team ? team.Id : 0, text: "", severity: "", waitingSince: null};
  if (worstFlag) {
    line.text = worstFlag.Message + (flags.length > 1 ? ` +${flags.length - 1}` : "");
    line.severity = worstFlag.Severity;
  }
  if (active && matchState === matchStatePreMatch && !ftaStatus.Ready) {
    const previous = stationLines[station];
    line.waitingSince = previous && previous.waitingSince && previous.teamId === line.teamId ?
      previous.waitingSince : Date.now();
    const missing = ftaStatus.Checks.find((check) => !check.Ok && !check.Advisory);
    if (missing && line.severity !== "bad") {
      line.text = `Waiting on ${checkShortNames[missing.Name] || missing.Name}`;
      line.severity = "warn";
    }
  }
  stationLines[station] = line;
  renderStationLine(station);

  let health = "ok";
  if (!team) {
    health = "empty";
  } else if (stationStatus.Bypass) {
    health = "bypass";
  } else if (flags.some((flag) => flag.Severity === "bad")) {
    health = "bad";
  } else if (!ftaStatus.Ready || flags.length > 0) {
    health = "warn";
  }
  card.attr("data-health", health);
};

const setLink = function (card, link, ok) {
  const element = card.find(`.chain [data-link="${link}"]`);
  if (ok === null || ok === undefined) {
    element.removeAttr("data-ok");
  } else {
    element.attr("data-ok", ok ? "true" : "false");
  }
};

const setMetric = function (card, metric, text, status) {
  card.find(`.st-metrics [data-metric="${metric}"]`).attr("data-status", status).find("dd").text(text);
};

const renderStationLine = function (station) {
  const line = stationLines[station];
  const element = $(`#station${station} .st-status`);
  if (!line || !line.text) {
    element.text("").removeAttr("data-severity data-long");
    return;
  }
  let text = line.text;
  let long = false;
  if (line.waitingSince) {
    const waitedSec = (Date.now() - line.waitingSince) / 1000;
    text += ` ${formatSeconds(waitedSec)}`;
    long = waitedSec >= longWaitSec;
  }
  element.text(text).attr({"data-severity": line.severity, "data-long": long});
};

const openStation = function (station) {
  const stationStatus = lastArenaStatus && lastArenaStatus.AllianceStations[station];
  if (!stationStatus || !stationStatus.Team) {
    showToast(`No team in ${station}`);
    return;
  }
  openTeamPanel(stationStatus.Team.Id);
};

// Fault log

const loadCurrentFaults = function () {
  const matchId = currentMatchId;
  fetchJson("/api/fta/events/current").then(function (events) {
    if (matchId !== currentMatchId) {
      return;
    }
    faults = events;
    faultIds.clear();
    events.forEach((event) => faultIds.add(event.Id));
    renderFaults();
  }).catch(function (error) {
    showToast("Couldn't load faults: " + error.message, "error");
  });
};

const handleFtaEvent = function (event) {
  if (event.MatchId !== currentMatchId || faultIds.has(event.Id)) {
    return;
  }
  faults.push(event);
  faultIds.add(event.Id);
  renderFaults(event.Id);
  if (lastArenaStatus) {
    updateStatus(lastArenaStatus);
  }

  if (event.Severity === "bad") {
    const card = $(`#station${event.Station}`);
    if (card.length) {
      card.removeClass("flash");
      void card[0].offsetWidth; // Force a reflow so that the animation restarts.
      card.addClass("flash");
    }
    alertFault();
  }
  if (openTeamId !== null && event.TeamId === openTeamId) {
    refreshTeamPanel();
  }
};

const setFaultFilter = function (filter) {
  faultFilter = filter;
  $("[data-filter]").each(function () {
    $(this).attr("aria-pressed", $(this).attr("data-filter") === filter ? "true" : "false");
  });
  renderFaults();
};

const faultMatchesFilter = function (fault) {
  switch (faultFilter) {
    case "bad":
      return fault.Severity === "bad";
    case "R":
    case "B":
      return allianceOf(fault.Station) === faultFilter;
    default:
      return true;
  }
};

const renderFaults = function (newFaultId) {
  const shown = faults.filter(faultMatchesFilter).reverse();
  $("#faultList").empty().append(shown.map((fault) => faultItem(fault, true).toggleClass("new", fault.Id === newFaultId)));
  $("#faultEmpty").prop("hidden", shown.length > 0)
    .text(faults.length > 0 ? "No faults match this filter." : "No faults this match.");

  const problems = faults.filter((fault) => fault.Severity !== "good");
  $("#faultCount").text(problems.length).prop("hidden", problems.length === 0)
    .attr("data-severity", problems.some((fault) => fault.Severity === "bad") ? "bad" : "");
};

const faultItem = function (fault, forCurrentMatch) {
  const item = $("<li class='fault'>").attr("data-severity", fault.Severity);
  item.append($("<span class='fault-time'>").text(formatEventTime(fault)));
  const body = $("<div class='fault-body'>").append(stationChip(fault.Station));
  // In a team's own history, the team number would just repeat on every line.
  if (fault.TeamId && forCurrentMatch) {
    body.append($("<span class='fault-team'>").text(fault.TeamId));
  }
  if (!forCurrentMatch && fault.MatchShortName) {
    body.append($("<span class='fault-match'>").text(fault.MatchShortName));
  }
  body.append($("<span class='fault-message'>").text(fault.Message));
  item.append(body);
  if (forCurrentMatch && fault.TeamId) {
    item.append(
      $("<button type='button' class='icon-button' title='Add a note' aria-label='Add a note'>" +
        "<i class='bi-pencil-square'></i></button>").on("click", function () {
        openTeamPanel(fault.TeamId, {
          tag: faultNoteTags[fault.Type] || "other",
          text: `${formatEventTime(fault)} ${fault.Message}. `,
        });
      }),
    );
  }
  return item;
};

const copyFaultLog = function () {
  const lines = faults.map(function (fault) {
    const who = fault.TeamId ? `${fault.Station} ${fault.TeamId}` : fault.Station || "Field";
    return `${formatEventTime(fault)}  ${who}  ${fault.Message}`;
  });
  const text = [`${currentMatchName} faults`].concat(lines.length ? lines : ["None"]).join("\n");
  navigator.clipboard.writeText(text).then(function () {
    showToast("Copied");
  }).catch(function () {
    showToast("Couldn't copy", "error");
  });
};

// Alerts

const toggleAlerts = function () {
  alertsEnabled = !alertsEnabled;
  applyAlertSetting();
  saveSetting("ftaAlerts", alertsEnabled);
  if (alertsEnabled) {
    // Play the alert once so the browser allows sound from now on, and so the FTA knows what it sounds like.
    alertFault();
  }
};

const applyAlertSetting = function () {
  $("#alertToggle").attr("aria-pressed", alertsEnabled ? "true" : "false")
    .attr("title", alertsEnabled ? "Alerts on: beep and vibrate on new faults" : "Alerts off")
    .find("i").attr("class", alertsEnabled ? "bi-bell-fill" : "bi-bell-slash");
};

const alertFault = function () {
  if (!alertsEnabled) {
    return;
  }
  if (navigator.vibrate) {
    navigator.vibrate([180, 80, 180]);
  }
  try {
    audioContext = audioContext || new (window.AudioContext || window.webkitAudioContext)();
    [0, 0.2].forEach(function (offset) {
      const oscillator = audioContext.createOscillator();
      const gain = audioContext.createGain();
      oscillator.frequency.value = 880;
      gain.gain.setValueAtTime(0.25, audioContext.currentTime + offset);
      gain.gain.exponentialRampToValueAtTime(0.001, audioContext.currentTime + offset + 0.15);
      oscillator.connect(gain).connect(audioContext.destination);
      oscillator.start(audioContext.currentTime + offset);
      oscillator.stop(audioContext.currentTime + offset + 0.16);
    });
  } catch (e) {
    // No audio; the vibration and the station flash still work.
  }
};

// Team panel

const openTeamPanel = function (teamId, noteDraft) {
  if (openTeamId !== teamId) {
    openTeamHistory = null;
    $("#panelTeamNumber").text(teamId);
    $("#panelTeamName").text("");
    $("#panelStation").prop("hidden", true);
    $("#pinnedNotes").val("").removeAttr("data-saved");
    $("#noteText").val("");
    $("#noteTag").val("other");
    $("#panelNotes, #panelSummary, #panelMatches tbody, #panelEvents").empty();
    $("#panelMatches").prop("hidden", true);
    $("#teamPanel .panel-body").scrollTop(0);
  }
  openTeamId = teamId;
  if (noteDraft) {
    $("#noteTag").val(noteDraft.tag);
    $("#noteText").val(noteDraft.text);
  }
  toggleOptions(false);
  toggleBlockers(false);
  $("#scrim").prop("hidden", false);
  $("#teamPanel").attr("aria-hidden", "false");
  renderPanelRisk();
  updatePanelLive();
  refreshTeamPanel();
  if (noteDraft) {
    focusNoteText();
  }
};

const closeTeamPanel = function () {
  openTeamId = null;
  openTeamHistory = null;
  $("#teamPanel").attr("aria-hidden", "true");
  $("#scrim").prop("hidden", true);
};

const focusNoteText = function () {
  const textArea = $("#noteText")[0];
  textArea.focus();
  textArea.setSelectionRange(textArea.value.length, textArea.value.length);
};

const refreshTeamPanel = function () {
  const teamId = openTeamId;
  fetchJson(`/api/fta/teams/${teamId}`).then(function (history) {
    if (teamId !== openTeamId) {
      return;
    }
    const firstLoad = openTeamHistory === null;
    openTeamHistory = history;
    renderTeamPanel(history, firstLoad);
  }).catch(function (error) {
    showToast("Couldn't load team history: " + error.message, "error");
  });
};

const renderTeamPanel = function (history, firstLoad) {
  $("#panelTeamName").text(history.Team.Nickname || history.Team.Name || "");
  $("#panelStation").text(history.Station).attr("data-alliance", allianceOf(history.Station))
    .prop("hidden", !history.Station);

  // Don't overwrite a pinned note that's being edited.
  const pinned = $("#pinnedNotes");
  if (firstLoad || pinned.val() === pinned.attr("data-saved")) {
    pinned.val(history.Team.FtaNotes || "");
  }
  pinned.attr("data-saved", history.Team.FtaNotes || "");
  $("#savePinnedNotes").prop("disabled", pinned.val() === pinned.attr("data-saved"));

  const stats = history.Stats;
  const totalFaults = stats.reduce((sum, stat) => sum + stat.FaultCount, 0);
  const totalDown = stats.reduce((sum, stat) => sum + stat.RobotDownSec, 0);
  const brownouts = stats.reduce((sum, stat) => sum + (stat.BrownoutCount || 0), 0);
  const voltages = stats.map((stat) => stat.MinBatteryVoltage).filter((voltage) => voltage > 0);
  const minVoltage = voltages.length ? Math.min(...voltages) : null;
  $("#panelSummary").empty().append(
    tile(stats.length, "Matches", ""),
    tile(totalFaults, "Faults", totalFaults > 0 ? "warn" : ""),
    tile(minVoltage === null ? "-" : minVoltage.toFixed(1) + "V", "Low batt",
      minVoltage === null ? "" : minVoltage < 6.8 ? "bad" : minVoltage < 7.5 ? "warn" : ""),
    brownouts > 0 ? tile(brownouts, "Brownouts", "bad") :
      tile(`${Math.round(totalDown)}s`, "Down", totalDown >= 5 ? "bad" : ""),
  );

  const tbody = $("#panelMatches tbody").empty();
  $.each(stats, function (i, stat) {
    const row = $("<tr>").attr("title", "Show timeline").on("click", function () {
      closeTeamPanel();
      openSideTab("timeline");
      loadTimeline(stat.MatchId);
    });
    row.append($("<td>").text(stat.MatchShortName || "Test"));
    row.append($("<td>").append(stationChip(stat.Station)));
    row.append(statCell(stat.FaultCount, stat.FaultCount > 0 ? "warn" : ""));
    row.append(statCell(stat.MinBatteryVoltage > 0 ? stat.MinBatteryVoltage.toFixed(1) : "-",
      stat.MinBatteryVoltage > 0 && stat.MinBatteryVoltage < 6.8 ? "bad" : ""));
    row.append(statCell(`${Math.round(stat.RobotDownSec)}s`, stat.RobotDownSec >= 5 ? "bad" : ""));
    row.append(statCell(`${stat.MaxTripTimeMs}ms`, stat.MaxTripTimeMs >= 20 ? "warn" : ""));
    tbody.append(row);
  });
  $("#panelMatches").prop("hidden", stats.length === 0);

  $("#panelNotes").empty().append(history.Notes.map(noteItem));

  $("#panelEvents").empty().append(history.Events.slice(0, 30).map((event) => faultItem(event, false)));
  $("#panelEventsEmpty").prop("hidden", history.Events.length > 0);
};

const tile = function (value, label, status) {
  return $("<div class='tile'>").attr("data-status", status).append($("<strong>").text(value), $("<span>").text(label));
};

const statCell = function (text, status) {
  return $("<td>").attr("data-status", status).text(text);
};

const noteItem = function (note) {
  const meta = $("<div class='note-meta'>").append($("<span class='note-tag'>").text(noteTagLabels[note.Tag] || note.Tag));
  if (note.MatchShortName) {
    meta.append($("<span>").text(note.MatchShortName));
  }
  meta.append($("<span>").text(new Date(note.Time).toLocaleString([], {
    weekday: "short", hour: "numeric", minute: "2-digit",
  })));
  const deleteButton = $("<button type='button' class='icon-button' title='Delete note'><i class='bi-trash'></i></button>")
    .on("click", function () {
      if (confirm("Delete this note?")) {
        websocket.send("deleteNote", {id: note.Id});
      }
    });
  return $("<li class='note'>").append(meta, $("<div class='note-text'>").text(note.Text), deleteButton);
};

// Shows what's wrong right now with the open team, if it's in the current match.
const updatePanelLive = function () {
  const section = $("#panelLive");
  const station = lastArenaStatus && stationIds.find(function (id) {
    const team = lastArenaStatus.AllianceStations[id].Team;
    return team && team.Id === openTeamId;
  });
  section.prop("hidden", !station);
  if (!station) {
    return;
  }

  const stationStatus = lastArenaStatus.AllianceStations[station];
  const ftaStatus = lastArenaStatus.FtaStations[station];
  const problems = $("#panelChecks").empty();
  $.each(ftaStatus.Flags, function (i, flag) {
    problems.append($("<li>").attr("data-severity", flag.Severity).text(flag.Message));
  });
  const lowBatteryFlagged = ftaStatus.Flags.some((flag) => flag.Type === "LowBattery");
  const passing = [];
  $.each(ftaStatus.Checks, function (i, check) {
    if (check.Ok) {
      passing.push(check.Name === "Battery" && check.Detail ? `Battery ${check.Detail}` : check.Name);
      return;
    }
    if (check.Name === "Battery" && lowBatteryFlagged) {
      // Already listed as a flag.
      return;
    }
    const item = $("<li>").attr("data-severity", check.Advisory ? "warn" : "bad").text(check.Name);
    if (check.Detail) {
      item.append($("<small>").text(check.Detail));
    }
    problems.append(item);
  });

  let summary = "";
  if (stationStatus.Bypass) {
    summary = "Bypassed. The robot stays disabled for the match.";
  } else if (passing.length > 0) {
    summary = "OK: " + passing.join(", ");
  }
  $("#panelChecksOk").text(summary);

  const bypassed = stationStatus.Bypass;
  $("#bypassButton").prop("hidden", matchState !== matchStatePreMatch)
    .attr({"data-station": station, "data-bypassed": bypassed})
    .text(bypassed ? `Un-bypass ${station}` : `Bypass ${station}`);
};

const toggleBypass = function () {
  const button = $("#bypassButton");
  const station = button.attr("data-station");
  if (button.attr("data-bypassed") !== "true" && !confirm(`Bypass ${station}? The robot will be disabled all match.`)) {
    return;
  }
  websocket.send("toggleBypass", {station: station});
};

const renderPanelRisk = function () {
  const summary = teamSummaries.get(openTeamId);
  const reasons = summary ? summary.Reasons : [];
  $("#panelRisk").prop("hidden", reasons.length === 0);
  $("#panelReasons").empty().append(reasons.map(reasonItem));
};

// Fills in the note from one of the common fixes, leaving room to add detail before saving.
const quickNote = function (button) {
  const text = $("#noteText");
  const current = text.val().trim();
  text.val(current ? `${current} ${$(button).text()}.` : `${$(button).text()}.`);
  $("#noteTag").val($(button).attr("data-tag"));
  focusNoteText();
};

const submitNote = function (event) {
  event.preventDefault();
  const text = $("#noteText").val().trim();
  if (!text || openTeamId === null) {
    return;
  }
  websocket.send("addNote", {teamId: openTeamId, tag: $("#noteTag").val() || "other", text: text});
  $("#noteText").val("");
  $("#noteTag").val("other");
  showToast(`Note saved for ${openTeamId}`);
};

const savePinnedNotes = function () {
  const pinned = $("#pinnedNotes");
  websocket.send("updateTeamNotes", {teamId: openTeamId, notes: pinned.val()});
  pinned.attr("data-saved", pinned.val());
  $("#savePinnedNotes").prop("disabled", true);
  showToast("Pinned note saved");
};

// Timeline

const loadTimelineMatches = function () {
  timelineMatchesLoaded = true;
  fetchJson("/api/fta/matches").then(function (matches) {
    const select = $("#timelineMatch");
    const previous = select.val();
    select.empty();
    select.append($("<option>").val(currentMatchId).text(`${currentMatchName || "Current match"} (current)`));
    $.each(matches, function (i, match) {
      if (match.Id === currentMatchId) {
        return;
      }
      const faultText = match.FaultCount > 0 ? `, ${plural(match.FaultCount, "fault")}` : "";
      select.append($("<option>").val(match.Id).text(match.ShortName + faultText));
    });
    if (previous && select.find(`option[value="${previous}"]`).length) {
      select.val(previous);
    }
    loadTimeline(select.val());
  }).catch(function (error) {
    showToast("Couldn't load matches: " + error.message, "error");
  });
};

const loadTimeline = function (matchId) {
  if (matchId === undefined || matchId === null || matchId === "") {
    return;
  }
  const select = $("#timelineMatch");
  if (select.find(`option[value="${matchId}"]`).length === 0) {
    select.append($("<option>").val(matchId).text(`Match ${matchId}`));
  }
  select.val(String(matchId));
  fetchJson(`/api/fta/matches/${matchId}/timeline`).then(function (timeline) {
    if (String(select.val()) === String(matchId)) {
      renderTimeline(timeline);
    }
  }).catch(function (error) {
    $("#timeline").empty().append($("<p class='empty'>").text("Couldn't load the timeline: " + error.message));
  });
  scheduleTimelineRefresh();
};

// Keeps the timeline of the match in progress up to date while it's on screen.
const scheduleTimelineRefresh = function () {
  clearTimeout(timelineRefreshTimer);
  timelineRefreshTimer = setTimeout(function () {
    const watchingLive = $("body").attr("data-tab") === "timeline" &&
      String($("#timelineMatch").val()) === String(currentMatchId) && isMatchRunning() && !document.hidden;
    if (watchingLive) {
      loadTimeline(currentMatchId);
    } else {
      scheduleTimelineRefresh();
    }
  }, timelineRefreshMs);
};

const renderTimeline = function (timeline) {
  const container = $("#timeline").empty();
  const duration = timeline.DurationSec;
  const percent = (seconds) => `${Math.max(0, Math.min(100, (seconds / duration) * 100))}%`;

  const ticks = $("<div class='tl-ticks'>");
  for (let seconds = 0; seconds <= duration; seconds += 30) {
    ticks.append($("<span>").css("left", percent(seconds)).text(formatSeconds(seconds)));
  }
  container.append($("<div class='tl-axis'>").append($("<span>"), ticks));

  const fieldEvents = timeline.Events.filter((event) => !event.Station && event.MatchTimeSec > 0);
  $.each(timeline.Stations, function (i, station) {
    const lane = $("<div class='tl-lane'>");
    lane.append($("<div class='tl-label'>").append(stationChip(station.Station),
      $("<span class='tl-team'>").text(station.TeamId || "-")));

    const track = $("<div class='tl-track'>");
    const samples = station.Samples;
    if (samples.length === 0) {
      track.append($("<span class='tl-nodata'>").text(station.TeamId ? "No log" : "Empty"));
    } else {
      // Merge consecutive samples at the same connection level into one segment.
      let start = 0;
      for (let j = 1; j <= samples.length; j++) {
        const done = j === samples.length || samples[j].Level !== samples[start].Level ||
          samples[j].Enabled !== samples[start].Enabled;
        if (done) {
          const startSec = samples[start].T;
          const endSec = j < samples.length ? samples[j].T : samples[j - 1].T + 0.5;
          track.append($("<span class='tl-segment'>").attr({
            "data-level": samples[start].Level,
            "data-enabled": samples[start].Enabled,
          }).css({left: percent(startSec), width: `${Math.max(0.2, (endSec - startSec) / duration * 100)}%`}));
          start = j;
        }
      }
      track.append(batterySparkline(samples, duration));
    }
    $.each([timeline.AutoEndSec, timeline.TeleopStart], function (j, seconds) {
      track.append($("<span class='tl-period'>").css("left", percent(seconds)));
    });

    const stationEvents = timeline.Events.filter((event) => event.Station === station.Station && event.MatchTimeSec > 0);
    $.each(stationEvents.concat(fieldEvents), function (j, event) {
      const description = `${formatEventTime(event)} ${event.Message}`;
      track.append(
        $("<button type='button' class='tl-marker'>").attr({
          "data-severity": event.Severity,
          "title": description,
          "aria-label": description,
        }).css("left", percent(event.MatchTimeSec)).on("click", function () {
          const who = event.TeamId ? `${event.Station} ${event.TeamId}` : "Field";
          $("#timelineDetail").text(`${who} at ${formatEventTime(event)}: ${event.Message}`);
        }),
      );
    });
    lane.append(track);

    if (station.TeamId) {
      const parts = [];
      if (station.Stats) {
        if (station.Stats.MinBatteryVoltage > 0) {
          parts.push(`min ${station.Stats.MinBatteryVoltage.toFixed(1)}V`);
        }
        parts.push(`down ${Math.round(station.Stats.RobotDownSec)}s`);
        parts.push(plural(station.Stats.FaultCount, "fault"));
      }
      const stats = $("<div class='tl-stats'>").text(parts.join(", "));
      if (samples.length > 0) {
        stats.append(parts.length ? " " : "", $("<a target='_blank'>").attr("href", station.LogUrl).text("Log"));
      }
      lane.append($("<span>"), stats);
    }
    container.append(lane);
  });
  $("#timelineDetail").text(timeline.Events.length ? "Select a marker for details." : "No faults logged.");
};

// Draws the battery voltage (6-13V) across the lane, with gaps where the robot wasn't reporting.
const batterySparkline = function (samples, duration) {
  const svgNs = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNs, "svg");
  svg.setAttribute("class", "tl-battery");
  svg.setAttribute("viewBox", "0 0 1000 100");
  svg.setAttribute("preserveAspectRatio", "none");
  let points = [];
  const flush = function () {
    if (points.length > 1) {
      const polyline = document.createElementNS(svgNs, "polyline");
      polyline.setAttribute("points", points.join(" "));
      svg.appendChild(polyline);
    }
    points = [];
  };
  $.each(samples, function (i, sample) {
    if (sample.Battery <= 0) {
      flush();
      return;
    }
    const x = (sample.T / duration) * 1000;
    const y = 100 - ((Math.min(13, Math.max(6, sample.Battery)) - 6) / 7) * 100;
    points.push(`${x.toFixed(1)},${y.toFixed(1)}`);
  });
  flush();
  return svg;
};

// Watchlist: up next and teams

const loadWatchlist = function () {
  fetchJson("/api/fta/watchlist").then(function (data) {
    watchlist = data;
    teamSummaries.clear();
    $.each(data.Teams, function (i, summary) {
      teamSummaries.set(summary.TeamId, summary);
    });
    renderUpnext();
    renderTeams();
    if (openTeamId !== null) {
      renderPanelRisk();
    }
  }).catch(function (error) {
    showToast("Couldn't load the watchlist: " + error.message, "error");
  });
};

const reasonItem = function (reason) {
  return $("<li class='reason'>").attr("data-severity", reason.Severity).text(reason.Message);
};

const renderUpnext = function () {
  const list = $("#upnextList").empty();
  // Count each flagged team once, even if it's in more than one of the upcoming matches.
  const flaggedTeams = new Set();
  $.each(watchlist.Upcoming, function (i, match) {
    const header = $("<header>").text(match.ShortName);
    const time = new Date(match.Time);
    if (time.getFullYear() > 1) {
      header.append($("<span>").text(time.toLocaleTimeString([], {hour: "numeric", minute: "2-digit"})));
    }
    const grid = $("<div class='upnext-teams slots'>");
    $.each(match.Stations, function (j, station) {
      const summary = teamSummaries.get(station.TeamId);
      if (summary && summary.Risk === "bad") {
        flaggedTeams.add(station.TeamId);
      }
      grid.append(upnextTeam(station, summary));
    });
    list.append($("<li class='upnext-match'>").append(header, grid));
  });
  $("#upnextEmpty").prop("hidden", watchlist.Upcoming.length > 0);
  $("#upnextCount").text(flaggedTeams.size).prop("hidden", flaggedTeams.size === 0)
    .attr("data-severity", flaggedTeams.size > 0 ? "bad" : "");
};

const upnextTeam = function (station, summary) {
  const button = $("<button type='button' class='upnext-team'>").attr("data-station", station.Station)
    .append(stationChip(station.Station));
  if (!station.TeamId) {
    return button.prop("disabled", true).append($("<span class='team-id'>").text("TBD"));
  }
  button.attr("data-risk", summary ? summary.Risk : "good").append($("<span class='team-id'>").text(station.TeamId));
  const detail = $("<span class='upnext-detail'>");
  if (summary && summary.Reasons.length > 0) {
    detail.text(summary.Reasons.map((reason) => reason.Message).join("; "));
  } else if (summary && summary.FtaNotes) {
    detail.text(summary.FtaNotes);
  }
  return button.append(detail).on("click", function () {
    openTeamPanel(station.TeamId);
  });
};

const setTeamFilter = function (filter) {
  teamFilter = filter;
  $("[data-team-filter]").each(function () {
    $(this).attr("aria-pressed", $(this).attr("data-team-filter") === filter ? "true" : "false");
  });
  renderTeams();
};

// Returns the teams matching the search or, with no search, the current filter.
const filteredTeams = function () {
  if (watchlist === null) {
    return [];
  }
  const query = $("#teamSearch").val().trim().toLowerCase();
  return watchlist.Teams.filter(function (summary) {
    if (query) {
      return String(summary.TeamId).startsWith(query) || (summary.Nickname || "").toLowerCase().includes(query);
    }
    return teamFilter === "all" || summary.Reasons.length > 0 || Boolean(summary.FtaNotes);
  });
};

const renderTeams = function () {
  const shown = filteredTeams();
  $("#teamList").empty().append(shown.map((summary) => $("<li>").append(teamRow(summary))));
  const searching = $("#teamSearch").val().trim() !== "";
  $("#teamsEmpty").text(searching ? "No matching teams." : "No flagged teams.")
    .prop("hidden", shown.length > 0);
};

const teamRow = function (summary) {
  const row = $("<button type='button' class='team-row'>").attr("data-risk", summary.Risk);
  const stats = [`${summary.MatchesPlayed} played`];
  if (summary.FaultCount > 0) {
    stats.push(plural(summary.FaultCount, "fault"));
  }
  if (summary.MinBatteryVoltage > 0) {
    stats.push(`min ${summary.MinBatteryVoltage.toFixed(1)}V`);
  }
  row.append($("<div class='team-row-head'>").append(
    $("<strong>").text(summary.TeamId),
    $("<span class='team-row-name'>").text(summary.Nickname || ""),
    $("<span class='team-row-stats'>").text(stats.join(", ")),
  ));
  if (summary.Reasons.length > 0) {
    row.append($("<ul class='reasons'>").append(summary.Reasons.map(reasonItem)));
  }
  if (summary.FtaNotes) {
    row.append($("<p class='team-row-pinned'>").text(summary.FtaNotes));
  }
  if (summary.LastNote) {
    const note = summary.LastNote;
    const prefix = [note.MatchShortName, noteTagLabels[note.Tag] || note.Tag].filter(Boolean).join(" ");
    row.append($("<p class='team-row-note'>").text(`${prefix}: ${note.Text}`));
  }
  return row.on("click", function () {
    openTeamPanel(summary.TeamId);
  });
};

// Enter opens the top result, or any full team number even if it isn't listed.
const teamSearchKeydown = function (event) {
  if (event.key !== "Enter") {
    return;
  }
  event.preventDefault();
  const shown = filteredTeams();
  const typed = parseInt($("#teamSearch").val().trim());
  if (shown.length > 0) {
    openTeamPanel(shown[0].TeamId);
  } else if (typed > 0) {
    openTeamPanel(typed);
  }
};

// Match info

const handleMatchLoad = function (data) {
  currentMatchName = data.Match.LongName;
  $("#matchName").text(currentMatchName);
  document.title = `FTA - ${data.Match.ShortName || currentMatchName}`;
};

const handleMatchTime = function (data) {
  translateMatchTime(data, function (state, stateText, countdownSec) {
    $("#matchState").text(stateText);
    $("#matchClock").text(getCountdownString(countdownSec));
  });
};

$(function () {
  alertsEnabled = readSetting("ftaAlerts") === "true";
  applyAlertSetting();
  setSwap(readSetting("ftaSwap") === "true");
  setFieldOnly(readSetting("ftaFieldOnly") === "true" && wideLayout.matches);
  const savedTab = readSetting("ftaTab");
  showTab(sideTabs.includes(savedTab) ? savedTab : "field");
  wideLayout.addEventListener("change", function () {
    showTab($("body").attr("data-tab"));
  });

  $("#noteTag option").each(function () {
    $(this).text(noteTagLabels[$(this).val()] || $(this).val());
  });
  $("#noteTag").val("other");
  $(".quick-notes button").on("click", function () {
    quickNote(this);
  });

  $(".station").on("click", function () {
    openStation($(this).attr("data-station"));
  }).on("keydown", function (event) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      openStation($(this).attr("data-station"));
    }
  });

  $("#pinnedNotes").on("input", function () {
    $("#savePinnedNotes").prop("disabled", $(this).val() === $(this).attr("data-saved"));
  });

  // Close the options menu and blocker list when tapping anywhere else.
  $(document).on("click", function (event) {
    if (!$(event.target).closest("#optionsMenu, #optionsButton").length) {
      toggleOptions(false);
    }
    if (!$(event.target).closest("#status").length) {
      toggleBlockers(false);
    }
  });

  $(document).on("keydown", function (event) {
    if (event.key === "Escape") {
      if (openTeamId !== null) {
        closeTeamPanel();
      }
      toggleOptions(false);
      toggleBlockers(false);
      return;
    }
    if ($(event.target).is("textarea, input, select") || event.ctrlKey || event.metaKey || event.altKey) {
      return;
    }
    const stationIndex = parseInt(event.key) - 1;
    if (stationIndex >= 0 && stationIndex < stationIds.length) {
      openStation(stationIds[stationIndex]);
    } else if (event.key === "f") {
      openSideTab("faults");
    } else if (event.key === "u") {
      openSideTab("upnext");
    } else if (event.key === "t") {
      openSideTab("timeline");
    } else if (event.key === "r") {
      toggleFtaReady();
    } else if (event.key === "/") {
      event.preventDefault();
      openSideTab("teams");
      $("#teamSearch").trigger("focus").trigger("select");
    }
  });

  setInterval(function () {
    stationIds.forEach(renderStationLine);
  }, 1000);

  websocket = new CheesyWebsocket("/fta/websocket", {
    arenaStatus: function (event) {
      handleArenaStatus(event.data);
    },
    error: function (event) {
      showToast(event.data, "error");
    },
    ftaEvent: function (event) {
      handleFtaEvent(event.data);
    },
    ftaNote: function (event) {
      if (event.data === openTeamId) {
        refreshTeamPanel();
      }
      if (watchlist !== null) {
        loadWatchlist();
      }
    },
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
  });
});
