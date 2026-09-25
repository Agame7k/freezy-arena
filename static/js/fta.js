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
let selectedNoteTag = "";
let alertsEnabled = false;
let audioContext = null;
let timelineMatchesLoaded = false;
let timelineRefreshTimer = null;
let blockersExpanded = true;
let watchlist = null;
const teamSummaries = new Map();
let teamFilter = "flagged";
// When each station started waiting on something before the match, so the FTA can see who is holding up the field.
const stationWaits = {};

const stationIds = ["R1", "R2", "R3", "B1", "B2", "B3"];
const matchStatePreMatch = 0;
const matchStatePostMatch = 5;
const timelineRefreshMs = 5000;
const tabIds = ["field", "faults", "upnext", "teams", "timeline"];
// Waiting longer than this is called out, since it's likely holding up the schedule.
const longWaitSec = 120;
// Below this the radio link is weak enough to be worth watching; field radios typically sit well above it.
const lowSnrDb = 20;
// Stats are saved in the background as the match ends, so give them a moment before refreshing the watchlist.
const watchlistPostMatchDelayMs = 2000;

// Suggests a note category for each kind of fault, so that "+ note" from the fault log needs as few taps as possible.
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
  EStop: "other",
  AStop: "other",
  Bypass: "other",
};

const isMatchRunning = function () {
  return matchState > matchStatePreMatch && matchState < matchStatePostMatch;
};

const allianceOf = function (station) {
  return station ? station[0] : "";
};

const stationChip = function (station) {
  return $("<span class='station-chip'>").attr("data-alliance", allianceOf(station)).text(station || "FIELD");
};

const formatMatchTime = function (event) {
  if (event.MatchTimeSec > 0) {
    const seconds = Math.floor(event.MatchTimeSec);
    return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }
  return new Date(event.Time).toLocaleTimeString([], {hour: "numeric", minute: "2-digit"});
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

// ---------- Tabs ----------

const showTab = function (tab) {
  $("body").attr("data-tab", tab);
  $("#tabs button").each(function () {
    $(this).attr("aria-selected", $(this).attr("data-tab") === tab ? "true" : "false");
  });
  try {
    localStorage.setItem("ftaTab", tab);
  } catch (e) {
    // Storage unavailable; the tab just won't be remembered.
  }
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

// ---------- Arena status ----------

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
  setHealth("#switchHealth", networkHealth(data.SwitchStatus), `Network switch: ${data.SwitchStatus}`);
  if (data.PlcIsEnabled) {
    const plcOk = data.PlcIsHealthy && !data.FieldEStop;
    setHealth("#plcHealth", plcOk ? "ok" : "bad", data.FieldEStop ? "Field E-stop active" : "PLC");
  } else {
    setHealth("#plcHealth", "", "PLC not enabled");
  }

  updateReadiness(data);
  updateFtaReady(data.IsFtaReady);
  $.each(stationIds, function (i, station) {
    updateStation(station, data.AllianceStations[station], data.FtaStations[station], data.PlcIsEnabled);
  });
  if (openTeamId !== null) {
    updatePanelChecks();
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

// Shows whether the match can start and, if not, exactly what is blocking it and how to fix it.
const updateReadiness = function (data) {
  const readiness = $("#readiness");
  const list = $("#blockerList");
  const badFaults = faults.filter((fault) => fault.Severity === "bad").length;
  list.empty();

  if (isMatchRunning()) {
    readiness.attr("data-state", "running").attr("data-faults", badFaults > 0 ? "true" : "false");
    $("#readinessIcon").attr("class", "bi-broadcast");
    $("#readinessText").text(
      badFaults > 0 ? `Match running · ${badFaults} critical fault${badFaults === 1 ? "" : "s"}` : "Match running",
    );
    return;
  }
  if (data.MatchState === matchStatePostMatch) {
    readiness.attr("data-state", "running").attr("data-faults", "false");
    $("#readinessIcon").attr("class", "bi-flag");
    $("#readinessText").text(`Post-match · ${faults.length} event${faults.length === 1 ? "" : "s"} logged`);
    return;
  }

  readiness.attr("data-faults", "false");
  if (data.CanStartMatch && !data.IsFtaReady) {
    // Nothing is blocking the start, but the scorekeeper will be warned that the FTA hasn't said the field is ready.
    readiness.attr("data-state", "waiting");
    $("#readinessIcon").attr("class", "bi-hand-index");
    $("#readinessText").text("Robots ready · waiting on FTA Ready");
    return;
  }
  if (data.CanStartMatch) {
    readiness.attr("data-state", "ready");
    $("#readinessIcon").attr("class", "bi-check-circle-fill");
    $("#readinessText").text("Ready to start");
    return;
  }

  const blockers = data.StartMatchBlockers || [];
  readiness.attr("data-state", "blocked");
  $("#readinessIcon").attr("class", "bi-exclamation-octagon");
  $("#readinessText").text(`Can't start: ${blockers.length} issue${blockers.length === 1 ? "" : "s"}`);
  $.each(blockers, function (i, blocker) {
    const item = $("<li class='blocker'>");
    // Show the message without the station list, since the stations are shown as tappable chips instead.
    const message = blocker.Stations ? blocker.Message.replace(/\s*\([^)]*\)$/, "") : blocker.Message;
    item.append($("<span class='blocker-message'>").text(message));
    const stations = $("<span class='blocker-stations'>");
    $.each(blocker.Stations || [], function (j, station) {
      stations.append(
        $("<button type='button' class='station-chip'>").attr("data-alliance", allianceOf(station))
          .attr("title", `Open ${station}`).text(station)
          .on("click", function () {
            openStation(station);
          }),
      );
    });
    item.append(stations);
    if (blocker.Hint) {
      item.append($("<span class='blocker-hint'>").text(blocker.Hint));
    }
    list.append(item);
  });
};

const toggleBlockers = function () {
  blockersExpanded = !blockersExpanded;
  $("#readinessSummary").attr("aria-expanded", blockersExpanded ? "true" : "false");
  $("#readiness").attr("data-expanded", blockersExpanded ? "true" : "false");
};

const updateFtaReady = function (ready) {
  $("#ftaReadyButton").attr("data-ready", ready ? "true" : "false").attr("aria-pressed", ready ? "true" : "false");
  $("#ftaReadyButton i").attr("class", ready ? "bi-check-circle-fill" : "bi-x-octagon");
  $("#ftaReadyButton span").text(ready ? "FTA Ready" : "FTA Not Ready");
};

const toggleFtaReady = function () {
  websocket.send("toggleFtaReady");
};

const updateStation = function (station, stationStatus, ftaStatus, plcEnabled) {
  const card = $(`#station${station}`);
  const team = stationStatus.Team;
  const dsConn = stationStatus.DsConn;
  const wifi = stationStatus.WifiStatus;

  card.find(".team-number").text(team ? team.Id : "—");
  card.find(".team-name").text(team && team.Nickname ? team.Nickname : "");
  card.find(".pinned-note").text(team && team.FtaNotes ? team.FtaNotes : "");

  // Connection chain, from the cable up to robot code; the first broken link is where to start looking.
  const links = {
    eth: plcEnabled ? stationStatus.Ethernet : null,
    ds: Boolean(dsConn && dsConn.DsLinked),
    radio: Boolean(dsConn && dsConn.RadioLinked),
    rio: Boolean(dsConn && dsConn.RioLinked),
    code: Boolean(dsConn && dsConn.RobotLinked),
  };
  let foundBreak = false;
  $.each(links, function (link, ok) {
    const element = card.find(`.chain [data-link="${link}"]`);
    if (!team || stationStatus.Bypass || ok === null) {
      element.removeAttr("data-ok").removeAttr("data-first-break");
      return;
    }
    element.attr("data-ok", ok ? "true" : "false");
    const firstBreak = !ok && !foundBreak && link !== "eth";
    element.attr("data-first-break", firstBreak ? "true" : "false");
    foundBreak = foundBreak || firstBreak;
  });

  // Metrics, colored by whether the server has flagged them.
  const flagTypes = new Set(ftaStatus.Flags.map((flag) => flag.Type));
  const linked = dsConn && dsConn.RobotLinked;
  setMetric(card, "battery", linked && dsConn.BatteryVoltage > 0 ? dsConn.BatteryVoltage.toFixed(1) + "V" : "—",
    flagTypes.has("Brownout") ? "bad" : flagTypes.has("LowBattery") ? "warn" : "");
  setMetric(card, "trip", linked ? dsConn.DsRobotTripTimeMs + "ms" : "—", flagTypes.has("HighTripTime") ? "warn" : "");
  setMetric(card, "missed", dsConn ? dsConn.MissedPacketCount : "—", "");
  const snr = wifi && wifi.RadioLinked && wifi.SignalNoiseRatio > 0 ? wifi.SignalNoiseRatio : null;
  setMetric(card, "snr", snr === null ? "—" : snr, snr !== null && snr < lowSnrDb ? "warn" : "");
  setMetric(card, "bandwidth", wifi && wifi.MBits >= 0.01 ? wifi.MBits.toFixed(1) : "—", "");

  const flags = card.find(".flags").empty();
  $.each(ftaStatus.Flags, function (i, flag) {
    flags.append($("<li class='flag'>").attr("data-severity", flag.Severity).text(flag.Message));
  });

  // Before the match, say exactly what the station is still waiting on, and for how long.
  if (team && !stationStatus.Bypass && matchState === matchStatePreMatch && !ftaStatus.Ready) {
    const missing = ftaStatus.Checks.filter((check) => !check.Ok && !check.Advisory).map((check) => check.Name);
    const wait = stationWaits[station];
    if (!wait || wait.teamId !== team.Id) {
      stationWaits[station] = {teamId: team.Id, since: Date.now(), text: ""};
    }
    stationWaits[station].text = "Waiting on " + missing.join(", ");
  } else {
    delete stationWaits[station];
  }
  renderWait(station);

  let health = "ok";
  if (!team) {
    health = "empty";
  } else if (stationStatus.Bypass) {
    health = "bypass";
  } else if (ftaStatus.Flags.some((flag) => flag.Severity === "bad")) {
    health = "bad";
  } else if (!ftaStatus.Ready || ftaStatus.Flags.length > 0) {
    health = "warn";
  }
  card.attr("data-health", health);
  card.attr("data-team-id", team ? team.Id : "");
};

const renderWait = function (station) {
  const element = $(`#station${station} .waiting`);
  const wait = stationWaits[station];
  if (!wait) {
    element.text("").removeAttr("data-long");
    return;
  }
  const seconds = Math.floor((Date.now() - wait.since) / 1000);
  element.text(`${wait.text} · ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`)
    .attr("data-long", seconds >= longWaitSec ? "true" : "false");
};

const setMetric = function (card, metric, text, status) {
  const element = card.find(`.metrics [data-metric="${metric}"]`);
  element.attr("data-status", status);
  element.find("dd").text(text);
};

const openStation = function (station) {
  const stationStatus = lastArenaStatus && lastArenaStatus.AllianceStations[station];
  if (!stationStatus || !stationStatus.Team) {
    showToast(`No team in ${station}`);
    return;
  }
  openTeamPanel(stationStatus.Team.Id);
};

// ---------- Fault log ----------

const loadCurrentFaults = function () {
  const matchId = currentMatchId;
  fetchJson("/api/fta/events/current").then(function (events) {
    if (matchId !== currentMatchId) {
      return;
    }
    faults = [];
    faultIds.clear();
    $.each(events, function (i, event) {
      faults.push(event);
      faultIds.add(event.Id);
    });
    renderFaults();
  }).catch(function (error) {
    showToast("Couldn't load the fault log: " + error.message, "error");
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
    updateReadiness(lastArenaStatus);
  }

  if (event.Severity === "bad") {
    const card = $(`#station${event.Station}`);
    card.removeClass("flash");
    void card[0]?.offsetWidth; // Force a reflow so that the animation restarts.
    card.addClass("flash");
    alertFault();
  }
  if (openTeamId !== null && event.TeamId === openTeamId) {
    refreshTeamPanel();
  }
};

const setFaultFilter = function (filter) {
  faultFilter = filter;
  $(".filter-chips button").each(function () {
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
  const list = $("#faultList").empty();
  const shown = faults.filter(faultMatchesFilter).slice().reverse();
  $.each(shown, function (i, fault) {
    list.append(faultItem(fault, true).toggleClass("new", fault.Id === newFaultId));
  });
  $("#faultEmpty").toggle(shown.length === 0);

  const problems = faults.filter((fault) => fault.Severity !== "good");
  $("#faultCount").text(problems.length).attr("data-count", problems.length)
    .attr("data-severity", problems.some((fault) => fault.Severity === "bad") ? "bad" : "");
};

const faultItem = function (fault, withNoteButton) {
  const item = $("<li class='fault'>").attr("data-severity", fault.Severity);
  item.append($("<span class='fault-time'>").text(formatMatchTime(fault)));
  const body = $("<div class='fault-body'>");
  const who = $("<div class='fault-who'>").append(stationChip(fault.Station));
  if (fault.TeamId) {
    who.append($("<span>").text(fault.TeamId));
  }
  if (!withNoteButton && fault.MatchShortName) {
    who.append($("<span>").text(fault.MatchShortName));
  }
  body.append(who).append($("<div class='fault-message'>").text(fault.Message));
  item.append(body);
  if (withNoteButton && fault.TeamId) {
    item.append(
      $("<button type='button' class='fault-note'><i class='bi-pencil'></i> Note</button>").on("click", function () {
        openTeamPanel(fault.TeamId, {
          tag: faultNoteTags[fault.Type] || "other",
          text: `${formatMatchTime(fault)} ${fault.Message}. `,
        });
      }),
    );
  }
  return item;
};

const copyFaultLog = function (button) {
  const lines = faults.map(function (fault) {
    const who = fault.TeamId ? `${fault.Station} ${fault.TeamId}` : fault.Station || "FIELD";
    return `${formatMatchTime(fault)}  ${who}  ${fault.Message}`;
  });
  const text = `${currentMatchName} fault log\n${lines.join("\n")}`;
  navigator.clipboard.writeText(text).then(function () {
    showToast("Fault log copied");
  }).catch(function () {
    showToast("Copy failed", "error");
  });
};

// ---------- Alerts ----------

const toggleAlerts = function () {
  alertsEnabled = !alertsEnabled;
  applyAlertSetting();
  try {
    localStorage.setItem("ftaAlerts", alertsEnabled ? "true" : "false");
  } catch (e) {
    // Storage unavailable; the setting just won't be remembered.
  }
  if (alertsEnabled) {
    alertFault();
  }
};

const applyAlertSetting = function () {
  $("#alertToggle").attr("aria-pressed", alertsEnabled ? "true" : "false")
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
    // Audio unavailable; vibration and the visual flash still work.
  }
};

// ---------- Team panel ----------

const openTeamPanel = function (teamId, noteDraft) {
  const switchingTeams = openTeamId !== teamId;
  openTeamId = teamId;
  if (switchingTeams) {
    openTeamHistory = null;
    $("#panelTeamNumber").text(teamId);
    $("#panelTeamName").text("");
    $("#panelStation").text("").hide();
    $("#pinnedNotes").val("");
    $("#noteText").val("");
    selectNoteTag("");
    $("#panelMatches tbody, #panelNotes, #panelEvents, #panelSummary").empty();
  }
  if (noteDraft) {
    selectNoteTag(noteDraft.tag);
    $("#noteText").val(noteDraft.text);
  }
  $("#scrim").prop("hidden", false);
  $("#teamPanel").attr("aria-hidden", "false");
  renderPanelRisk();
  updatePanelChecks();
  refreshTeamPanel();
  if (noteDraft) {
    const textArea = $("#noteText")[0];
    textArea.focus();
    textArea.setSelectionRange(textArea.value.length, textArea.value.length);
  }
};

const closeTeamPanel = function () {
  openTeamId = null;
  openTeamHistory = null;
  $("#teamPanel").attr("aria-hidden", "true");
  $("#scrim").prop("hidden", true);
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
  if (history.Station) {
    $("#panelStation").text(history.Station).attr("data-alliance", allianceOf(history.Station)).show();
  } else {
    $("#panelStation").hide();
  }

  // Don't clobber standing notes the FTA is in the middle of editing.
  const pinned = $("#pinnedNotes");
  if (firstLoad || pinned.val() === pinned.attr("data-saved")) {
    pinned.val(history.Team.FtaNotes || "");
  }
  pinned.attr("data-saved", history.Team.FtaNotes || "");
  $("#savePinnedNotes").prop("disabled", pinned.val() === pinned.attr("data-saved"));

  // Summary of every match on record.
  const stats = history.Stats;
  const totalFaults = stats.reduce((sum, stat) => sum + stat.FaultCount, 0);
  const totalDown = stats.reduce((sum, stat) => sum + stat.RobotDownSec, 0);
  const voltages = stats.map((stat) => stat.MinBatteryVoltage).filter((voltage) => voltage > 0);
  const minVoltage = voltages.length ? Math.min(...voltages) : null;
  const brownouts = stats.reduce((sum, stat) => sum + (stat.BrownoutCount || 0), 0);
  $("#panelSummary").empty().append(
    summaryTile(stats.length, "Matches", ""),
    summaryTile(totalFaults, "Faults", totalFaults > 0 ? "warn" : ""),
    summaryTile(minVoltage === null ? "—" : minVoltage.toFixed(1) + "V", "Lowest",
      minVoltage !== null && minVoltage < 6.8 ? "bad" : minVoltage !== null && minVoltage < 7.5 ? "warn" : ""),
    summaryTile(brownouts > 0 ? brownouts : `${Math.round(totalDown)}s`, brownouts > 0 ? "Brownouts" : "Down",
      brownouts > 0 || totalDown >= 5 ? "bad" : ""),
  );

  const tbody = $("#panelMatches tbody").empty();
  $.each(stats, function (i, stat) {
    const row = $("<tr>").attr("title", "Show timeline").on("click", function () {
      closeTeamPanel();
      showTab("timeline");
      loadTimeline(stat.MatchId);
    });
    row.append($("<td>").text(stat.MatchShortName || "Test"));
    row.append($("<td>").append(stationChip(stat.Station)));
    row.append(statCell(stat.FaultCount, stat.FaultCount > 0 ? "warn" : ""));
    row.append(statCell(stat.MinBatteryVoltage > 0 ? stat.MinBatteryVoltage.toFixed(1) : "—",
      stat.MinBatteryVoltage > 0 && stat.MinBatteryVoltage < 6.8 ? "bad" : ""));
    row.append(statCell(`${Math.round(stat.RobotDownSec)}s`, stat.RobotDownSec >= 5 ? "bad" : ""));
    row.append(statCell(`${stat.MaxTripTimeMs}ms`, stat.MaxTripTimeMs >= 20 ? "warn" : ""));
    tbody.append(row);
  });
  $("#panelMatchesEmpty").toggle(stats.length === 0);

  const notes = $("#panelNotes").empty();
  $.each(history.Notes, function (i, note) {
    const item = $("<li class='note'>");
    const meta = $("<div class='note-meta'>").append($("<span class='note-tag'>").text(note.Tag));
    if (note.MatchShortName) {
      meta.append($("<span>").text(note.MatchShortName));
    }
    meta.append($("<span>").text(new Date(note.Time).toLocaleString([], {
      weekday: "short", hour: "numeric", minute: "2-digit",
    })));
    item.append(meta).append($("<div class='note-text'>").text(note.Text));
    item.append(
      $("<button type='button' class='icon-button' aria-label='Delete note'><i class='bi-trash'></i></button>")
        .on("click", function () {
          if (confirm("Delete this note?")) {
            websocket.send("deleteNote", {id: note.Id});
          }
        }),
    );
    notes.append(item);
  });
  $("#panelNotesEmpty").toggle(history.Notes.length === 0);

  const events = $("#panelEvents").empty();
  $.each(history.Events.slice(0, 30), function (i, event) {
    events.append(faultItem(event, false));
  });
  $("#panelEventsEmpty").toggle(history.Events.length === 0);
};

const summaryTile = function (value, label, status) {
  return $("<div class='summary-tile'>").attr("data-status", status)
    .append($("<strong>").text(value), $("<span>").text(label));
};

const statCell = function (text, status) {
  return $("<td>").attr("data-status", status).text(text);
};

// Shows the live readiness checklist for the open team, if it's in the current match.
const updatePanelChecks = function () {
  const section = $("#panelLive");
  if (!lastArenaStatus || openTeamId === null) {
    section.hide();
    return;
  }
  const station = stationIds.find(function (id) {
    const team = lastArenaStatus.AllianceStations[id].Team;
    return team && team.Id === openTeamId;
  });
  const bypassButton = $("#bypassButton");
  if (!station) {
    section.hide();
    bypassButton.prop("hidden", true);
    return;
  }
  section.show();
  const bypassed = lastArenaStatus.AllianceStations[station].Bypass;
  bypassButton.prop("hidden", matchState !== matchStatePreMatch).attr("data-station", station)
    .attr("data-bypassed", bypassed ? "true" : "false")
    .html(bypassed ? "<i class='bi-arrow-counterclockwise'></i> Remove bypass" :
      `<i class='bi-slash-circle'></i> Bypass ${station}`);
  const list = $("#panelChecks").empty();
  const ftaStatus = lastArenaStatus.FtaStations[station];
  $.each(ftaStatus.Flags, function (i, flag) {
    list.append($("<li data-ok='false'>").attr("data-advisory", flag.Severity === "warn").text(flag.Message));
  });
  $.each(ftaStatus.Checks, function (i, check) {
    const item = $("<li>").attr("data-ok", check.Ok).attr("data-advisory", check.Advisory).text(check.Name);
    if (check.Detail && (!check.Ok || check.Name === "Battery")) {
      item.append($("<small>").text(check.Detail));
    }
    list.append(item);
  });
  if (list.children().length === 0) {
    list.append($("<li data-ok='true'>").text("Bypassed"));
  }
};

const toggleBypass = function () {
  const button = $("#bypassButton");
  const station = button.attr("data-station");
  const bypassing = button.attr("data-bypassed") !== "true";
  if (bypassing && !confirm(`Bypass ${station}? The robot will stay disabled for the whole match.`)) {
    return;
  }
  websocket.send("toggleBypass", {station: station});
};

// Shows why the open team is on the watchlist, if it is.
const renderPanelRisk = function () {
  const summary = teamSummaries.get(openTeamId);
  const reasons = summary ? summary.Reasons : [];
  $("#panelRisk").prop("hidden", reasons.length === 0);
  $("#panelReasons").empty().append(reasons.map(reasonItem));
};

const selectNoteTag = function (tag) {
  selectedNoteTag = tag;
  $("#noteTags button").each(function () {
    $(this).attr("aria-checked", $(this).attr("data-tag") === tag ? "true" : "false");
  });
};

const submitNote = function (event) {
  event.preventDefault();
  const text = $("#noteText").val().trim();
  if (!text || openTeamId === null) {
    return;
  }
  websocket.send("addNote", {teamId: openTeamId, tag: selectedNoteTag || "other", text: text});
  $("#noteText").val("");
  selectNoteTag("");
  showToast(`Note added for ${openTeamId}`);
};

// Adds one of the common fixes as a note in a single tap.
const quickNote = function (button) {
  if (openTeamId === null) {
    return;
  }
  websocket.send("addNote", {teamId: openTeamId, tag: $(button).attr("data-tag"), text: $(button).text()});
  showToast(`Noted: ${$(button).text()}`);
};

const savePinnedNotes = function () {
  websocket.send("updateTeamNotes", {teamId: openTeamId, notes: $("#pinnedNotes").val()});
  $("#pinnedNotes").attr("data-saved", $("#pinnedNotes").val());
  $("#savePinnedNotes").prop("disabled", true);
  showToast("Standing notes saved");
};

// ---------- Timeline ----------

const loadTimelineMatches = function () {
  timelineMatchesLoaded = true;
  fetchJson("/api/fta/matches").then(function (matches) {
    const select = $("#timelineMatch");
    const previous = select.val();
    select.empty();
    select.append($("<option>").val(currentMatchId).text(`Current: ${currentMatchName || "match"}`));
    $.each(matches, function (i, match) {
      if (match.Id === currentMatchId) {
        return;
      }
      const faultText = match.FaultCount > 0 ? ` · ${match.FaultCount} fault${match.FaultCount === 1 ? "" : "s"}` : "";
      select.append($("<option>").val(match.Id).text(`${match.ShortName}${faultText}`));
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
    $("#timeline").empty().append($("<p class='empty'>").text("Couldn't load timeline: " + error.message));
  });
  scheduleTimelineRefresh();
};

// Keeps the timeline for the match in progress up to date while it's being watched.
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

  const axis = $("<div class='tl-axis'>").append($("<span>"));
  const ticks = $("<div class='tl-ticks'>");
  for (let seconds = 0; seconds <= duration; seconds += 30) {
    ticks.append($("<span>").css("left", percent(seconds)).text(`${Math.floor(seconds / 60)}:${
      String(seconds % 60).padStart(2, "0")}`));
  }
  axis.append(ticks);
  container.append(axis);

  const fieldEvents = timeline.Events.filter((event) => !event.Station && event.MatchTimeSec > 0);
  $.each(timeline.Stations, function (i, station) {
    const lane = $("<div class='tl-lane'>");
    const label = $("<div class='tl-label'>").append(stationChip(station.Station));
    label.append($("<span class='tl-team'>").text(station.TeamId || "—"));
    lane.append(label);

    const track = $("<div class='tl-track'>");
    const samples = station.Samples;
    if (samples.length === 0) {
      track.append($("<span class='tl-nodata'>").text(station.TeamId ? "No log recorded" : "Empty"));
    } else {
      // Merge consecutive samples at the same connection level into a single segment.
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
      track.append(
        $("<button type='button' class='tl-marker'>").attr({
          "data-severity": event.Severity,
          title: `${formatMatchTime(event)} ${event.Message}`,
          "aria-label": `${formatMatchTime(event)} ${event.Message}`,
        }).css("left", percent(event.MatchTimeSec)).on("click", function () {
          const who = event.TeamId ? `${event.Station} ${event.TeamId}` : "Field";
          $("#timelineDetail").text(`${who} · ${formatMatchTime(event)} · ${event.Message}`);
        }),
      );
    });
    lane.append(track);

    if (station.TeamId) {
      const stats = $("<div class='tl-stats'>");
      const parts = [];
      if (station.Stats) {
        if (station.Stats.MinBatteryVoltage > 0) {
          parts.push(`min ${station.Stats.MinBatteryVoltage.toFixed(1)}V`);
        }
        parts.push(`down ${Math.round(station.Stats.RobotDownSec)}s`);
        parts.push(`${station.Stats.FaultCount} fault${station.Stats.FaultCount === 1 ? "" : "s"}`);
      }
      stats.text(parts.join(" · ") + (parts.length ? " · " : ""));
      if (samples.length > 0) {
        stats.append($("<a target='_blank'>").attr("href", station.LogUrl).text("full log"));
      }
      lane.append($("<span>"), stats);
    }
    container.append(lane);
  });
  $("#timelineDetail").text(timeline.Events.length ? "Tap a marker for details." : "No faults logged in this match.");
};

// Draws the battery voltage (6-13V) across the lane, breaking the line wherever the robot wasn't reporting.
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

// ---------- Watchlist ----------

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
  // Count each flagged team once, even if it's queued for more than one of the upcoming matches.
  const flaggedTeams = new Set();
  $.each(watchlist.Upcoming, function (i, match) {
    const item = $("<li class='upnext-match'>");
    const header = $("<header>").append($("<strong>").text(match.ShortName));
    const time = new Date(match.Time);
    if (time.getFullYear() > 1) {
      header.append($("<span>").text(time.toLocaleTimeString([], {hour: "numeric", minute: "2-digit"})));
    }
    item.append(header);
    const grid = $("<div class='upnext-teams'>");
    $.each(match.Stations, function (j, station) {
      const summary = teamSummaries.get(station.TeamId);
      if (summary && summary.Risk === "bad") {
        flaggedTeams.add(station.TeamId);
      }
      grid.append(upnextTeam(station, summary));
    });
    list.append(item.append(grid));
  });
  $("#upnextEmpty").toggle(watchlist.Upcoming.length === 0);
  const flagged = flaggedTeams.size;
  $("#upnextCount").text(flagged).attr("data-count", flagged).attr("data-severity", flagged > 0 ? "bad" : "");
};

const upnextTeam = function (station, summary) {
  const button = $("<button type='button' class='upnext-team'>").attr("data-alliance", allianceOf(station.Station));
  button.append(stationChip(station.Station));
  if (!station.TeamId) {
    return button.prop("disabled", true).append($("<span class='team-id'>").text("TBD"));
  }
  button.attr("data-risk", summary ? summary.Risk : "good")
    .append($("<span class='team-id'>").text(station.TeamId));
  const detail = $("<span class='upnext-detail'>");
  if (summary && summary.Reasons.length > 0) {
    detail.text(summary.Reasons[0].Message + (summary.Reasons.length > 1 ? ` +${summary.Reasons.length - 1}` : ""));
  } else if (summary && summary.FtaNotes) {
    detail.addClass("has-note").text(summary.FtaNotes);
  }
  button.append(detail);
  return button.on("click", function () {
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

// Returns the teams matching the search, or the flagged teams if there's no search.
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
  const list = $("#teamList").empty();
  $.each(shown, function (i, summary) {
    list.append($("<li>").append(teamRow(summary)));
  });
  const searching = $("#teamSearch").val().trim() !== "";
  $("#teamsEmpty").text(searching ? "No teams match." : "No teams flagged. Nice.").toggle(shown.length === 0);
};

const teamRow = function (summary) {
  const row = $("<button type='button' class='team-row'>").attr("data-risk", summary.Risk);
  const head = $("<div class='team-row-head'>")
    .append($("<strong>").text(summary.TeamId), $("<span class='team-row-name'>").text(summary.Nickname || ""));
  const stats = [`${summary.MatchesPlayed} played`];
  if (summary.FaultCount > 0) {
    stats.push(`${summary.FaultCount} fault${summary.FaultCount === 1 ? "" : "s"}`);
  }
  if (summary.MinBatteryVoltage > 0) {
    stats.push(`min ${summary.MinBatteryVoltage.toFixed(1)}V`);
  }
  head.append($("<span class='team-row-stats'>").text(stats.join(" · ")));
  row.append(head);
  if (summary.Reasons.length > 0) {
    row.append($("<ul class='reason-list'>").append(summary.Reasons.map(reasonItem)));
  }
  if (summary.FtaNotes) {
    row.append($("<p class='pinned-note'>").text(summary.FtaNotes));
  }
  if (summary.LastNote) {
    const where = summary.LastNote.MatchShortName ? `${summary.LastNote.MatchShortName} ` : "";
    row.append($("<p class='team-row-note'>").text(`${where}${summary.LastNote.Tag}: ${summary.LastNote.Text}`));
  }
  return row.on("click", function () {
    openTeamPanel(summary.TeamId);
  });
};

// Enter in the search box opens the top result, or any team number typed in full even if it isn't listed.
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

// ---------- Match info ----------

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
  try {
    alertsEnabled = localStorage.getItem("ftaAlerts") === "true";
    const savedTab = localStorage.getItem("ftaTab");
    showTab(tabIds.includes(savedTab) ? savedTab : "field");
  } catch (e) {
    showTab("field");
  }
  applyAlertSetting();

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

  $(document).on("keydown", function (event) {
    if (event.key === "Escape" && openTeamId !== null) {
      closeTeamPanel();
      return;
    }
    if ($(event.target).is("textarea, input, select") || event.ctrlKey || event.metaKey || event.altKey) {
      return;
    }
    const stationIndex = parseInt(event.key) - 1;
    if (stationIndex >= 0 && stationIndex < stationIds.length) {
      openStation(stationIds[stationIndex]);
    } else if (event.key === "f") {
      showTab("faults");
    } else if (event.key === "t") {
      showTab("timeline");
    } else if (event.key === "r") {
      toggleFtaReady();
    } else if (event.key === "u") {
      showTab("upnext");
    } else if (event.key === "/") {
      event.preventDefault();
      showTab("teams");
      $("#teamSearch").trigger("focus").trigger("select");
    }
  });

  setInterval(function () {
    stationIds.forEach(renderWait);
  }, 1000);

  // Set up the websocket back to the server.
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
