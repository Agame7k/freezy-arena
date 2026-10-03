// Copyright 2026 Team 254. All Rights Reserved.
//
// Shared client-side code for the hub lighting simulator and the field simulator: the 3D Hub model, the match
// timeline, and the test match controls. Everything shown comes from the server (pixels, mode names, mode swatches,
// layout, timeline), so the simulators follow changes to the Go code without changes here.

const simConfig = window.hubSimConfig;
const SIDE_NAMES = simConfig.SideNames;
const FIXTURE_NAMES = simConfig.FixtureNames;
const PIXELS_PER_FIXTURE = simConfig.PixelsPerFixture;
const PIXEL_COUNT = SIDE_NAMES.length * FIXTURE_NAMES.length * PIXELS_PER_FIXTURE;
const TOP_FIXTURE = FIXTURE_NAMES.indexOf("Top");
const BOTTOM_FIXTURE = FIXTURE_NAMES.indexOf("Bottom");
const LED_OFF_RGB = [35, 38, 44];
const ALLIANCE_COLORS = { red: "#e5322d", blue: "#2f6fde" };

// Which way each side faces in a top-down view from the audience, as [x, y]: the red driver station is at -x and the
// scoring table at +y. Keyed by the side names from led.SideNames.
const SIDE_DIRECTIONS = {
  red: { "Driver Station": [-1, 0], Audience: [0, -1], Center: [1, 0], "Scoring Table": [0, 1] },
  blue: { "Driver Station": [1, 0], Audience: [0, -1], Center: [-1, 0], "Scoring Table": [0, 1] },
};

const STALE_FRAME_MS = 1500;
const NEXT_CHANGE_LEAD_SEC = 2;

// Creates an element with the given class and optional text.
const el = function (tag, className, text) {
  const element = document.createElement(tag);
  if (className) {
    element.className = className;
  }
  if (text !== undefined) {
    element.textContent = text;
  }
  return element;
};

const svgEl = function (tag, attributes) {
  const element = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const name in attributes) {
    element.setAttribute(name, attributes[name]);
  }
  return element;
};

// Sets an element's text only when it changes, to avoid needless relayout.
const setText = function (element, text) {
  if (element.textContent !== text) {
    element.textContent = text;
  }
};

const capitalize = function (text) {
  return text.charAt(0).toUpperCase() + text.slice(1);
};

const modeName = function (mode) {
  return simConfig.ModeNames[mode] || "Mode " + mode;
};

const pixelIndex = function (side, fixture, pixel) {
  return (side * FIXTURE_NAMES.length + fixture) * PIXELS_PER_FIXTURE + pixel;
};

const parsePixels = function (hex) {
  const pixels = new Array(PIXEL_COUNT);
  for (let i = 0; i < PIXEL_COUNT; i++) {
    const offset = i * 6;
    pixels[i] = [
      parseInt(hex.substr(offset, 2), 16) || 0,
      parseInt(hex.substr(offset + 2, 2), 16) || 0,
      parseInt(hex.substr(offset + 4, 2), 16) || 0,
    ];
  }
  return pixels;
};

// Returns the average color of the given pixels along with how bright it is (0-1).
const averageColor = function (pixels) {
  const sum = [0, 0, 0];
  pixels.forEach(function (pixel) {
    sum[0] += pixel[0];
    sum[1] += pixel[1];
    sum[2] += pixel[2];
  });
  const average = sum.map(function (value) {
    return value / Math.max(pixels.length, 1);
  });
  return { rgb: average, brightness: Math.max(average[0], average[1], average[2]) / 255 };
};

// Returns the color scaled up to full brightness, for glows whose strength is set separately.
const fullBrightness = function (rgb) {
  const max = Math.max(rgb[0], rgb[1], rgb[2]);
  if (max === 0) {
    return [0, 0, 0];
  }
  return rgb.map(function (value) {
    return Math.round((value * 255) / max);
  });
};

const rgba = function (rgb, alpha) {
  return "rgba(" + Math.round(rgb[0]) + "," + Math.round(rgb[1]) + "," + Math.round(rgb[2]) + "," + alpha + ")";
};

// Returns the CSS color for an LED showing the given color through an unlit diffuser.
const litColor = function (rgb) {
  return (
    "rgb(" +
    rgb
      .map(function (value, i) {
        return Math.round(LED_OFF_RGB[i] + (value * (255 - LED_OFF_RGB[i])) / 255);
      })
      .join(",") +
    ")"
  );
};

// Paints one LED cell, skipping the work if it hasn't changed.
const paintCell = function (cell, rgb) {
  const key = (rgb[0] << 16) | (rgb[1] << 8) | rgb[2];
  if (cell.ledKey === key) {
    return;
  }
  cell.ledKey = key;
  cell.style.backgroundColor = litColor(rgb);
  const brightness = Math.max(rgb[0], rgb[1], rgb[2]) / 255;
  cell.style.boxShadow =
    brightness > 0.02
      ? "0 0 " + (3 + 9 * brightness).toFixed(1) + "px " + rgba(rgb, (0.35 + 0.6 * brightness).toFixed(2))
      : "none";
};

const formatClock = function (seconds) {
  const total = Math.max(0, Math.ceil(seconds));
  const minutes = Math.floor(total / 60);
  const secs = total % 60;
  return minutes + ":" + (secs < 10 ? "0" : "") + secs;
};

// Returns true if dark text reads better than light text on the given hex color.
const isLightColor = function (hex) {
  const value = parseInt(hex.slice(1), 16);
  const luminance = 0.299 * ((value >> 16) & 255) + 0.587 * ((value >> 8) & 255) + 0.114 * (value & 255);
  return luminance > 150;
};

// Builds one fixture's LED bar and records its cells by pixel index.
const buildLedBar = function (alliance, side, fixture, cellList) {
  const bar = el("div", "led-bar");
  bar.style.gridTemplateColumns = "repeat(" + PIXELS_PER_FIXTURE + ", 1fr)";
  for (let pixel = 0; pixel < PIXELS_PER_FIXTURE; pixel++) {
    const cell = el("div", "led-cell");
    cell.title =
      capitalize(alliance) + " hub, facing " + SIDE_NAMES[side].toLowerCase() + ", " +
      FIXTURE_NAMES[fixture].toLowerCase() + " fixture, pixel " + pixel;
    cellList[pixelIndex(side, fixture, pixel)] = cell;
    bar.appendChild(cell);
  }
  return bar;
};

// Returns the rotation (about the vertical axis, in degrees) that turns a face toward the given top-down direction,
// where 0 faces the audience.
const faceAngle = function (direction) {
  return (Math.atan2(direction[0], -direction[1]) * 180) / Math.PI;
};

// A 3D model of one Hub: a face per side with its fixtures' LED bars, an open top, and the light it casts on the floor.
// It is centered on its own origin; size it with the --hub-w and --hub-h CSS variables.
const HubModel = function (alliance, showLabels) {
  this.alliance = alliance;
  this.faceAngles = SIDE_NAMES.map(function (name) {
    return faceAngle(SIDE_DIRECTIONS[alliance][name] || [0, -1]);
  });
  this.element = el("div", "hub-model");
  this.floor = el("div", "hub-floor");
  this.floor.appendChild(el("div", "hub-floor-shadow"));
  this.element.appendChild(this.floor);
  this.cells = new Array(PIXEL_COUNT);
  this.faceSpills = [];
  for (let side = 0; side < SIDE_NAMES.length; side++) {
    const face = el("div", "hub-face");
    face.style.transform = "rotateY(" + this.faceAngles[side] + "deg) translateZ(calc(var(--hub-w) / 2))";
    const spill = el("div", "hub-face-spill");
    face.appendChild(spill);
    this.faceSpills.push(spill);
    // Lay the fixtures out from the top of the face down.
    for (let fixture = FIXTURE_NAMES.length - 1; fixture >= 0; fixture--) {
      face.appendChild(buildLedBar(alliance, side, fixture, this.cells));
    }
    if (showLabels) {
      face.appendChild(el("div", "hub-face-label", SIDE_NAMES[side]));
    }
    this.element.appendChild(face);
  }
  this.top = el("div", "hub-top");
  this.beacon = el("div", "hub-beacon");
  this.top.appendChild(this.beacon);
  this.element.appendChild(this.top);
};

// Paints the pixels onto the faces, the light spilling onto each face, and the glow on the floor. Returns the overall
// average color.
HubModel.prototype.paint = function (pixels) {
  for (let i = 0; i < PIXEL_COUNT; i++) {
    paintCell(this.cells[i], pixels[i]);
  }
  const fixtureAverage = function (side, fixture) {
    const start = pixelIndex(side, fixture, 0);
    return averageColor(pixels.slice(start, start + PIXELS_PER_FIXTURE));
  };
  for (let side = 0; side < SIDE_NAMES.length; side++) {
    const top = fixtureAverage(side, TOP_FIXTURE >= 0 ? TOP_FIXTURE : FIXTURE_NAMES.length - 1);
    const bottom = fixtureAverage(side, BOTTOM_FIXTURE >= 0 ? BOTTOM_FIXTURE : 0);
    this.faceSpills[side].style.backgroundImage =
      "linear-gradient(180deg, " + rgba(fullBrightness(top.rgb), (0.28 * top.brightness).toFixed(3)) + " 0%, " +
      "transparent 40%, transparent 60%, " + rgba(fullBrightness(bottom.rgb), (0.28 * bottom.brightness).toFixed(3)) +
      " 100%)";
  }
  const overall = averageColor(pixels);
  const glow = fullBrightness(overall.rgb);
  this.floor.style.backgroundImage =
    "radial-gradient(circle at center, " + rgba(glow, (0.55 * overall.brightness).toFixed(3)) + " 0%, " +
    rgba(glow, (0.2 * overall.brightness).toFixed(3)) + " 30%, transparent 62%)";
  return overall;
};

// Shows the PLC-driven light on top of the Hub.
HubModel.prototype.setBeacon = function (on) {
  this.beacon.dataset.on = on ? "true" : "false";
};

// Tracks the latest state from the server and answers questions about the match timeline.
const SimState = function () {
  this.timelines = null;
  this.frame = null;
  this.fieldStatus = null;
  this.matchTime = null;
  this.previewWinner = "red";
  this.previousMatchState = null;
  this.postMatchStartedAt = 0;
  this.postMatchFromTeleop = false;
  this.lastFrameAt = 0;
  this.timelinesVersion = 0;
};

SimState.prototype.handleFrame = function (frame) {
  if (frame.MatchState === matchStateId("POST_MATCH") && this.previousMatchState !== frame.MatchState) {
    this.postMatchStartedAt = performance.now();
    this.postMatchFromTeleop = this.previousMatchState === matchStateId("TELEOP_PERIOD");
  }
  this.previousMatchState = frame.MatchState;
  this.frame = frame;
  this.lastFrameAt = performance.now();
};

SimState.prototype.isLive = function () {
  return this.lastFrameAt > 0 && performance.now() - this.lastFrameAt < STALE_FRAME_MS;
};

// Returns which auto winner's timeline to show and why.
SimState.prototype.timelineChoice = function () {
  if (this.frame && this.frame.AutoWinner) {
    return { winner: this.frame.AutoWinner, reason: "actual" };
  }
  // The arena only applies the simulator's forced winner to test matches.
  if (this.frame && this.frame.SimAutoWinner && this.frame.TestMatch) {
    return { winner: this.frame.SimAutoWinner, reason: "forced" };
  }
  return { winner: this.previewWinner, reason: "preview" };
};

SimState.prototype.timeline = function () {
  if (!this.timelines) {
    return null;
  }
  return this.timelineChoice().winner === "red" ? this.timelines.RedWonAuto : this.timelines.BlueWonAuto;
};

SimState.prototype.findShift = function (name) {
  const timeline = this.timeline();
  if (!timeline) {
    return null;
  }
  return (
    timeline.Shifts.find(function (shift) {
      return shift.Name === name;
    }) || null
  );
};

// Returns the time into the match for the timeline playhead, or null when no match is running.
SimState.prototype.playheadTime = function () {
  const frame = this.frame;
  if (!frame) {
    return null;
  }
  const state = matchStates[frame.MatchState];
  if (state === "AUTO_PERIOD" || state === "PAUSE_PERIOD" || state === "TELEOP_PERIOD") {
    return frame.MatchTimeSec;
  }
  // After a match runs to the end the lights follow the post-match part of the timeline; an aborted match doesn't.
  if (state === "POST_MATCH" && this.postMatchFromTeleop && !frame.Aborted) {
    const endgame = this.findShift("Endgame");
    const timeline = this.timeline();
    if (endgame && timeline) {
      return Math.min(timeline.EndSec - 0.01, endgame.EndSec + (performance.now() - this.postMatchStartedAt) / 1000);
    }
  }
  return null;
};

// Returns the next point after the given match time at which the alliance's Hub LED mode changes.
SimState.prototype.nextModeChange = function (alliance, matchTimeSec) {
  const timeline = this.timeline();
  if (!timeline) {
    return null;
  }
  const modeKey = alliance === "red" ? "RedMode" : "BlueMode";
  let currentMode = null;
  for (let i = 0; i < timeline.Segments.length; i++) {
    const segment = timeline.Segments[i];
    if (matchTimeSec >= segment.StartSec && matchTimeSec < segment.EndSec) {
      currentMode = segment[modeKey];
    } else if (segment.StartSec > matchTimeSec && currentMode !== null && segment[modeKey] !== currentMode) {
      return { atSec: segment.StartSec, mode: segment[modeKey] };
    }
  }
  return null;
};

// Returns the swatch the server rendered for the mode on the alliance's Hub.
SimState.prototype.swatch = function (alliance, mode) {
  if (!this.timelines || !this.timelines.Swatches[alliance]) {
    return { Color: "#000000", Animated: false };
  }
  return this.timelines.Swatches[alliance][mode] || { Color: "#000000", Animated: false };
};

// Returns the match state text and countdown the same way the displays do (see match_timing.js).
SimState.prototype.clock = function () {
  let result = { state: "", text: "-:--" };
  if (this.matchTime && typeof matchTiming !== "undefined" && matchTiming) {
    translateMatchTime(this.matchTime, function (state, stateText, countdown) {
      result = { state: stateText, text: getCountdownString(Math.max(0, countdown)) };
    });
  }
  return result;
};

const matchStateId = function (name) {
  for (const id in matchStates) {
    if (matchStates[id] === name) {
      return parseInt(id, 10);
    }
  }
  return -1;
};

// Draws the shift header and one LED row per Hub for the auto winner being shown.
// If onSeek is given, clicking ahead of the playhead calls it with the number of seconds to skip.
const SimTimelineView = function (sim, alliances, elements, onSeek) {
  this.sim = sim;
  this.alliances = alliances;
  this.elements = elements;
  this.renderedKey = null;
  const that = this;
  elements.winnerButton.addEventListener("click", function () {
    sim.previewWinner = sim.previewWinner === "red" ? "blue" : "red";
    that.render();
  });

  if (onSeek) {
    const area = elements.rows.parentElement;
    const seekTarget = function (event) {
      const timeline = sim.timeline();
      const now = sim.playheadTime();
      const state = sim.frame ? matchStates[sim.frame.MatchState] : "";
      if (!timeline || now === null || state === "POST_MATCH") {
        return null;
      }
      const rect = elements.rows.getBoundingClientRect();
      const target = ((event.clientX - rect.left) / rect.width) * timeline.EndSec;
      return target > now + 0.2 ? target : null;
    };
    area.addEventListener("mousemove", function (event) {
      const target = seekTarget(event);
      area.classList.toggle("seekable", target !== null);
      area.title = target !== null ? "Click to jump to " + formatClock(target) + " into the match" : "";
    });
    area.addEventListener("mouseleave", function () {
      area.classList.remove("seekable");
    });
    area.addEventListener("click", function (event) {
      const target = seekTarget(event);
      if (target !== null) {
        onSeek(target - sim.playheadTime());
      }
    });
  }
};

SimTimelineView.prototype.render = function () {
  const sim = this.sim;
  const timeline = sim.timeline();
  if (!timeline) {
    return;
  }
  const choice = sim.timelineChoice();
  const key = choice.winner + ":" + choice.reason + ":" + sim.timelinesVersion;
  if (key !== this.renderedKey) {
    this.renderedKey = key;
    this.renderStatic(timeline, choice);
  }
  this.renderPlayhead(timeline);
};

SimTimelineView.prototype.renderStatic = function (timeline, choice) {
  const sim = this.sim;
  const elements = this.elements;
  const percent = function (seconds) {
    return (100 * seconds) / timeline.EndSec;
  };

  const winnerName = capitalize(choice.winner);
  if (choice.reason === "actual") {
    setText(elements.winnerButton, winnerName + " won auto");
  } else if (choice.reason === "forced") {
    setText(elements.winnerButton, winnerName + " will win auto (forced)");
  } else {
    setText(elements.winnerButton, "Showing: if " + choice.winner + " wins auto (click to flip)");
  }
  elements.winnerButton.disabled = choice.reason !== "preview";

  elements.shifts.textContent = "";
  timeline.Shifts.forEach(function (shift) {
    const node = el("div", "timeline-shift");
    node.style.left = percent(shift.StartSec) + "%";
    node.style.width = percent(shift.EndSec - shift.StartSec) + "%";
    node.title = shift.Name + ": " + shift.StartSec + "–" + shift.EndSec + " s";
    node.appendChild(el("b", "", shift.Name));
    node.appendChild(document.createTextNode(formatClock(shift.EndSec - shift.StartSec)));
    elements.shifts.appendChild(node);
  });

  const modesShown = {};
  elements.rows.textContent = "";
  this.alliances.forEach(function (alliance) {
    const modeKey = alliance === "red" ? "RedMode" : "BlueMode";
    const track = el("div", "timeline-row-track");
    track.title = capitalize(alliance) + " hub";
    timeline.Segments.forEach(function (segment) {
      const mode = segment[modeKey];
      const swatch = sim.swatch(alliance, mode);
      modesShown[modeName(mode) + ":" + swatch.Color + ":" + swatch.Animated] = { mode: mode, swatch: swatch };
      const node = el("div", "timeline-segment" + (swatch.Animated ? " animated" : ""));
      node.style.left = percent(segment.StartSec) + "%";
      node.style.width = percent(segment.EndSec - segment.StartSec) + "%";
      node.style.setProperty("--c", swatch.Color);
      if (isLightColor(swatch.Color)) {
        node.classList.add("light");
      }
      node.title =
        capitalize(alliance) + " hub: " + modeName(mode) + ", " + segment.StartSec.toFixed(1) + "–" +
        segment.EndSec.toFixed(1) + " s";
      if (percent(segment.EndSec - segment.StartSec) > 4) {
        node.appendChild(el("span", "", modeName(mode)));
      }
      track.appendChild(node);
    });
    const row = el("div", "timeline-row");
    row.appendChild(track);
    elements.rows.appendChild(row);
  });

  // The legend lists the modes that appear, with the swatches the server rendered for them.
  if (elements.legend) {
    elements.legend.textContent = "";
    Object.keys(modesShown)
      .sort()
      .forEach(function (key) {
        const entry = modesShown[key];
        const item = el("span");
        const swatchElement = el("i", "swatch" + (entry.swatch.Animated ? " animated" : ""));
        swatchElement.style.setProperty("--c", entry.swatch.Color);
        item.appendChild(swatchElement);
        item.appendChild(document.createTextNode(modeName(entry.mode)));
        elements.legend.appendChild(item);
      });
  }
};

SimTimelineView.prototype.renderPlayhead = function (timeline) {
  const playhead = this.elements.playhead;
  const matchTimeSec = this.sim.playheadTime();
  if (matchTimeSec === null) {
    playhead.hidden = true;
    return;
  }
  playhead.hidden = false;
  const left = Math.min(100, (100 * matchTimeSec) / timeline.EndSec).toFixed(2) + "%";
  if (playhead.style.left !== left) {
    playhead.style.left = left;
  }
};


// Returns true if the match loaded in the arena is a test match (or nothing is known yet).
const loadedTestMatch = function (sim) {
  return !sim.frame || sim.frame.TestMatch;
};

// Asks before a simulator action would replace or affect a real (non-test) match.
const confirmRealMatch = function (sim, action) {
  if (loadedTestMatch(sim)) {
    return true;
  }
  const name = sim.fieldStatus && sim.fieldStatus.MatchName ? sim.fieldStatus.MatchName : "a real match";
  return window.confirm(name + " is loaded, not a test match. " + action + " Continue?");
};

// The test match controls, or an explanation of how to enable them.
const SimControls = function (sim, container, send, previewAlliances) {
  this.sim = sim;
  this.send = send;
  this.previewAlliances = previewAlliances;
  this.controls = {};
  this.build(container);
};

SimControls.prototype.build = function (container) {
  const sim = this.sim;
  const send = this.send;
  const controls = this.controls;
  const that = this;
  if (!simConfig.CanControl) {
    const message = el("div", "controls-off");
    if (!simConfig.SimEnabled) {
      message.innerHTML =
        "Viewing only. Start Cheesy Arena with <code>-hubsim</code> or <code>-simfield</code> to run robot-free " +
        "test matches from here, or run a match from Match Play and watch it here.";
    } else {
      message.innerHTML =
        'Log in as admin to use the simulator controls. <a href="/login?redirect=' +
        encodeURIComponent(location.pathname + location.search) + '">Log in</a>';
    }
    container.appendChild(message);
    return;
  }

  const group = function (labelText) {
    const node = el("div", "control-group");
    node.appendChild(el("span", "control-label", labelText));
    const row = el("div", "control-row");
    node.appendChild(row);
    container.appendChild(node);
    return row;
  };
  const button = function (row, text, className, onClick, title) {
    const node = el("button", "btn " + (className || ""), text);
    node.type = "button";
    node.dataset.title = title || "";
    node.title = title || "";
    node.addEventListener("click", onClick);
    row.appendChild(node);
    return node;
  };

  const matchRow = group("Test match");
  controls.start = button(matchRow, "Start test match", "btn-primary", function () {
    that.startMatch();
  }, "Bypasses the stations without a linked robot, arms FTA ready, and starts a test match");
  controls.abort = button(matchRow, "Abort", "", function () {
    send("abortMatch");
  }, "Abort the running test match");
  controls.reset = button(matchRow, "Reset", "", function () {
    if (confirmRealMatch(sim, "Reset loads a test match in its place.")) {
      send("resetMatch");
    }
  }, "Back to pre-match with a fresh test match");

  const winnerRow = group("Auto winner (test matches)");
  const segmented = el("div", "segmented");
  winnerRow.appendChild(segmented);
  controls.winners = {};
  [["", "By fuel"], ["red", "Red"], ["blue", "Blue"]].forEach(function (option) {
    controls.winners[option[0]] = button(segmented, option[1], "", function () {
      send("setAutoWinner", option[0]);
    }, option[0] === "" ? "Decide by the auto Fuel counts, as the arena does (a tie is random)" : "Force " +
      option[1] + " to win auto");
  });

  const skipRow = group("Skip ahead");
  controls.skips = [
    button(skipRow, "+5 s", "", function () {
      send("skipAhead", 5);
    }, "Move the match clock 5 seconds ahead"),
    button(skipRow, "+15 s", "", function () {
      send("skipAhead", 15);
    }, "Move the match clock 15 seconds ahead"),
    button(skipRow, "Next change", "", function () {
      that.skipToNextChange();
    }, "Jump to " + NEXT_CHANGE_LEAD_SEC + " s before the next LED change (or click ahead on the timeline)"),
  ];

  const fuelRow = group("Score fuel");
  controls.fuel = [];
  ["red", "blue"].forEach(function (alliance) {
    [1, 5].forEach(function (count) {
      controls.fuel.push(
        button(fuelRow, capitalize(alliance) + " +" + count, "btn-" + alliance, function () {
          send("addFuel", { Alliance: alliance, Count: count });
        }, "Count " + count + " Fuel through the " + alliance + " hub sensors")
      );
    });
  });

  const previewRow = group("Preview LED mode");
  controls.previews = {};
  this.previewAlliances.forEach(function (alliance) {
    const select = el("select");
    select.title = capitalize(alliance) + " hub LED mode (only between matches)";
    Object.keys(simConfig.ModeNames)
      .map(Number)
      .sort(function (a, b) {
        return a - b;
      })
      .forEach(function (mode) {
        const prefix = that.previewAlliances.length === 1 ? "" : capitalize(alliance) + ": ";
        const option = el("option", "", prefix + modeName(mode));
        option.value = mode;
        select.appendChild(option);
      });
    select.addEventListener("change", function () {
      send("setLedMode", { Alliance: alliance, Mode: parseInt(select.value, 10) });
      select.blur();
    });
    previewRow.appendChild(select);
    controls.previews[alliance] = select;
  });

  controls.hint = el("div", "controls-hint");
  container.appendChild(controls.hint);
};

SimControls.prototype.startMatch = function () {
  if (confirmRealMatch(this.sim, "Starting replaces it with a test match.")) {
    this.send("startMatch");
  }
};

// Enables a control, or disables it and says why in its tooltip.
const setEnabled = function (node, enabled, reason) {
  node.disabled = !enabled;
  const title = enabled ? node.dataset.title : reason;
  if (node.title !== title) {
    node.title = title;
  }
};

// Enables the controls that apply to the current match state.
SimControls.prototype.update = function () {
  const sim = this.sim;
  const frame = sim.frame;
  if (!simConfig.CanControl || !frame) {
    return;
  }
  const controls = this.controls;
  const fieldStatus = sim.fieldStatus;
  const state = matchStates[frame.MatchState];
  const running = ["START_MATCH", "AUTO_PERIOD", "PAUSE_PERIOD", "TELEOP_PERIOD"].indexOf(state) >= 0;
  const idle = ["PRE_MATCH", "POST_MATCH", "TIMEOUT_ACTIVE", "POST_TIMEOUT"].indexOf(state) >= 0;
  const betweenMatches = state === "PRE_MATCH" || state === "POST_MATCH";
  const simulatedPlc = !fieldStatus || fieldStatus.SimulatedPlc;

  setEnabled(controls.start, betweenMatches, "A match is already running");
  setEnabled(controls.abort, running && frame.TestMatch, running ? "Only test matches can be aborted here" : "No match is running");
  setEnabled(controls.reset, betweenMatches, "Wait for the match to end");
  controls.skips.forEach(function (node) {
    setEnabled(node, running && state !== "START_MATCH" && frame.TestMatch, "Only while a test match is running");
  });
  // Fuel only counts during the match and the scoring grace period after it.
  const fuelCounts = running || state === "POST_MATCH";
  controls.fuel.forEach(function (node) {
    setEnabled(
      node,
      simulatedPlc && fuelCounts,
      simulatedPlc ? "Fuel only counts while a match is running" : "A real PLC is counting Fuel"
    );
  });
  for (const winner in controls.winners) {
    controls.winners[winner].setAttribute("aria-pressed", frame.SimAutoWinner === winner ? "true" : "false");
  }
  for (const alliance in controls.previews) {
    const select = controls.previews[alliance];
    select.disabled = !idle;
    if (document.activeElement !== select) {
      select.value = String(alliance === "red" ? frame.RedMode : frame.BlueMode);
    }
  }

  // Explain anything the simulator can't fix by itself that would keep a match from starting.
  let hint = "";
  if (fieldStatus && betweenMatches) {
    const pressed = [];
    if (fieldStatus.FieldEStopPressed) {
      pressed.push("the field E-stop");
    }
    fieldStatus.Stations.forEach(function (station) {
      if (station.EStopPressed) {
        pressed.push(station.Id + " E-stop");
      }
      if (station.AStopPressed) {
        pressed.push(station.Id + " A-stop");
      }
    });
    if (pressed.length) {
      hint = "Release " + pressed.join(", ") + " before starting a match.";
    } else if (!frame.TestMatch) {
      hint = fieldStatus.MatchName + " is loaded; Start test match will replace it with a test match.";
    }
  }
  setText(controls.hint, hint);
  controls.hint.hidden = hint === "";
};

// Skips to a little before the next LED change on either Hub.
SimControls.prototype.skipToNextChange = function () {
  const sim = this.sim;
  const matchTimeSec = sim.playheadTime();
  if (matchTimeSec === null) {
    return;
  }
  let target = null;
  ["red", "blue"].forEach(function (alliance) {
    const next = sim.nextModeChange(alliance, matchTimeSec + NEXT_CHANGE_LEAD_SEC + 0.3);
    if (next && (target === null || next.atSec < target)) {
      target = next.atSec;
    }
  });
  if (target === null) {
    const endgame = sim.findShift("Endgame");
    target = endgame ? endgame.EndSec : matchTimeSec + 5;
  }
  const seconds = target - NEXT_CHANGE_LEAD_SEC - matchTimeSec;
  if (seconds > 0) {
    this.send("skipAhead", seconds);
  }
};

// Shows a message at the bottom of the page until it times out or is clicked.
const showToast = (function () {
  let timer = null;
  return function (message, kind) {
    const toast = document.getElementById("toast");
    if (!toast.dataset.bound) {
      toast.dataset.bound = "true";
      toast.title = "Click to dismiss";
      toast.addEventListener("click", function () {
        toast.hidden = true;
      });
    }
    toast.textContent = message;
    toast.dataset.kind = kind || "error";
    toast.hidden = false;
    clearTimeout(timer);
    timer = setTimeout(function () {
      toast.hidden = true;
    }, kind === "info" ? 4000 : 9000);
  };
})();

// Ignores keyboard shortcuts while typing in a form field or with a modifier held.
const isShortcutEvent = function (event) {
  const tag = event.target.tagName;
  return !(tag === "SELECT" || tag === "INPUT" || tag === "TEXTAREA" || event.ctrlKey || event.metaKey || event.altKey);
};

// Tells the server whether the page is visible, so that it only streams updates to pages someone can see.
let simWebsocket = null;
const reportVisibility = function () {
  if (simWebsocket) {
    try {
      simWebsocket.send("setVisible", document.visibilityState !== "hidden");
    } catch (e) {
      // Not connected; the next connection reports it again.
    }
  }
};
document.addEventListener("visibilitychange", reportVisibility);

// Reads and writes per-viewer preferences, which may be unavailable (e.g. in a private window).
const loadPreference = function (key, fallback) {
  try {
    const value = window.localStorage.getItem("cheesySim." + key);
    return value === null ? fallback : JSON.parse(value);
  } catch (e) {
    return fallback;
  }
};

const savePreference = function (key, value) {
  try {
    window.localStorage.setItem("cheesySim." + key, JSON.stringify(value));
  } catch (e) {
    // Preferences just won't be remembered.
  }
};

// Connects to the given simulator websocket, feeding the state and calling onUpdate (at most once per animation
// frame) whenever something changes.
const connectSim = function (path, sim, onUpdate, extraEvents) {
  let scheduled = false;
  let lastErrorAt = 0;
  const schedule = function () {
    if (!scheduled) {
      scheduled = true;
      requestAnimationFrame(function () {
        scheduled = false;
        try {
          onUpdate();
        } catch (e) {
          // Keep going on the next update, but don't flood the console.
          if (performance.now() - lastErrorAt > 5000) {
            lastErrorAt = performance.now();
            console.error(e);
          }
        }
      });
    }
  };
  const events = {
    hubSimTimeline: function (event) {
      // The server sends the timeline first on every (re)connection.
      sim.timelines = event.data;
      sim.timelinesVersion++;
      reportVisibility();
      schedule();
    },
    hubSimFrame: function (event) {
      sim.handleFrame(event.data);
      schedule();
    },
    fieldSimStatus: function (event) {
      sim.fieldStatus = event.data;
      schedule();
    },
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
    matchTime: function (event) {
      sim.matchTime = event.data;
      schedule();
    },
    error: function (event) {
      showToast(event.data);
    },
  };
  for (const name in extraEvents || {}) {
    events[name] = extraEvents[name];
  }

  // Flag the connection as lost if frames stop arriving (the server sends one at least every second).
  const connStatus = document.getElementById("connStatus");
  const banner = el("div", "conn-banner", "Lost connection to Cheesy Arena. Reconnecting… (is it still running?)");
  banner.hidden = true;
  document.body.appendChild(banner);
  setInterval(function () {
    const state = sim.isLive() ? "live" : sim.lastFrameAt > 0 ? "lost" : "waiting";
    if (connStatus.dataset.state !== state) {
      connStatus.dataset.state = state;
      connStatus.textContent = state === "live" ? "Live" : state === "lost" ? "Disconnected" : "Connecting";
      banner.hidden = state !== "lost";
      document.body.classList.toggle("sim-disconnected", state === "lost");
    }
  }, 300);

  simWebsocket = new CheesyWebsocket(path, events);
  return {
    send: function (type, data) {
      if (!sim.isLive()) {
        showToast("Not connected to Cheesy Arena right now; try again in a moment.");
        return;
      }
      try {
        simWebsocket.send(type, data);
      } catch (e) {
        showToast("Couldn't send that to Cheesy Arena: " + e.message);
      }
    },
    schedule: schedule,
  };
};
