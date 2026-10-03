// Copyright 2026 Team 254. All Rights Reserved.
//
// Client-side logic for the hub lighting simulator page (see hub_sim_shared.js for the shared pieces).

const DEFAULT_TILT = -18;
const DEFAULT_YAW_OFFSET = 35;

const alliances = simConfig.Alliance === "both" ? ["red", "blue"] : [simConfig.Alliance];
const sim = new SimState();
const hubViews = {};
let connection;
let timelineView;
let controls;

// One Hub's 3D model, unfolded faces, field map, and status readout.
const HubView = function (alliance, container) {
  const that = this;
  this.alliance = alliance;
  this.lastHex = null;
  this.otherHex = null;
  this.yaw = 0;
  this.tilt = DEFAULT_TILT;
  this.spinning = false;

  const panel = el("section", "sim-card hub-panel");
  panel.dataset.alliance = alliance;
  container.appendChild(panel);

  // Status readout.
  const status = el("div", "hub-status");
  const modeLine = el("div", "hub-mode");
  this.modeSwatch = el("span", "hub-mode-swatch");
  this.modeName = el("span", "hub-mode-name", "-");
  modeLine.appendChild(el("span", "hub-label", capitalize(alliance) + " hub:"));
  modeLine.appendChild(this.modeSwatch);
  modeLine.appendChild(this.modeName);
  this.detail = el("div", "hub-mode-desc");
  const state = el("div", "hub-state");
  this.activeBadge = el("span", "hub-active", "NO MATCH");
  this.shiftText = el("span", "hub-shift");
  this.nextText = el("span", "hub-next");
  state.appendChild(this.activeBadge);
  state.appendChild(this.shiftText);
  state.appendChild(this.nextText);
  status.appendChild(modeLine);
  status.appendChild(state);
  status.appendChild(this.detail);
  panel.appendChild(status);

  // 3D model.
  const body = el("div", "hub-body");
  panel.appendChild(body);
  this.stage = el("div", "hub-stage");
  this.stage.title = "Drag to orbit, double-click to reset the view";
  this.rotator = el("div", "hub-3d");
  this.model = new HubModel(alliance, true);
  this.rotator.appendChild(this.model.element);
  this.stage.appendChild(this.rotator);
  body.appendChild(this.stage);

  // View buttons and field map.
  const side = el("div", "hub-side");
  const viewButtons = el("div", "view-buttons");
  SIDE_NAMES.forEach(function (name, index) {
    const button = el("button", "", index + 1 + " " + name);
    button.type = "button";
    button.title = "View the side facing the " + name.toLowerCase();
    button.addEventListener("click", function () {
      that.viewSide(index);
    });
    viewButtons.appendChild(button);
  });
  const resetButton = el("button", "", "0 Default view");
  resetButton.type = "button";
  resetButton.addEventListener("click", function () {
    that.resetView();
  });
  this.spinButton = el("button", "", "S Spin");
  this.spinButton.type = "button";
  this.spinButton.setAttribute("aria-pressed", "false");
  this.spinButton.addEventListener("click", function () {
    that.setSpinning(!that.spinning);
  });
  viewButtons.appendChild(resetButton);
  viewButtons.appendChild(this.spinButton);
  side.appendChild(viewButtons);
  side.appendChild(this.buildFieldMap());
  body.appendChild(side);

  // Unfolded faces.
  const faces = el("div", "faces");
  this.flatCells = new Array(PIXEL_COUNT);
  for (let sideIndex = 0; sideIndex < SIDE_NAMES.length; sideIndex++) {
    const face = el("div", "face-flat");
    face.appendChild(el("div", "face-flat-label", "Facing " + SIDE_NAMES[sideIndex]));
    for (let fixture = FIXTURE_NAMES.length - 1; fixture >= 0; fixture--) {
      face.appendChild(buildLedBar(alliance, sideIndex, fixture, this.flatCells));
    }
    const tag = el("div", "face-flat-tag");
    tag.appendChild(el("span", "", "px 0"));
    tag.appendChild(el("span", "", FIXTURE_NAMES.slice().reverse().join(" / ").toLowerCase()));
    tag.appendChild(el("span", "", "px " + (PIXELS_PER_FIXTURE - 1)));
    face.appendChild(tag);
    faces.appendChild(face);
  }
  panel.appendChild(faces);

  this.bindOrbit();
  this.resetView(false);
};

// Builds the top-down field map (as seen from the audience) showing both Hubs, with this one outlined.
HubView.prototype.buildFieldMap = function () {
  const wrapper = el("div", "field-map");
  const svg = svgEl("svg", { viewBox: "0 0 260 150", role: "img" });
  svg.setAttribute("aria-label", "Top-down field map showing which way each hub side faces");
  svg.appendChild(svgEl("rect", { x: 20, y: 22, width: 220, height: 106, fill: "#101215", stroke: "#3a3f48" }));
  svg.appendChild(svgEl("line", { x1: 20, y1: 22, x2: 20, y2: 128, stroke: ALLIANCE_COLORS.red, "stroke-width": 4 }));
  svg.appendChild(svgEl("line", { x1: 240, y1: 22, x2: 240, y2: 128, stroke: ALLIANCE_COLORS.blue, "stroke-width": 4 }));
  svg.appendChild(svgEl("line", { x1: 130, y1: 22, x2: 130, y2: 128, stroke: "#272c34", "stroke-dasharray": "3 3" }));
  const label = function (text, x, y, rotate) {
    const node = svgEl("text", { x: x, y: y, "text-anchor": "middle" });
    if (rotate) {
      node.setAttribute("transform", "rotate(" + rotate + " " + x + " " + y + ")");
    }
    node.textContent = text;
    svg.appendChild(node);
  };
  label("SCORING TABLE", 130, 14);
  label("AUDIENCE", 130, 144);
  label("RED DS", 10, 75, -90);
  label("BLUE DS", 250, 75, 90);

  const that = this;
  const halfSize = 15;
  const strip = 4;
  this.mapCells = {};
  [["red", 86], ["blue", 174]].forEach(function (hub) {
    const hubAlliance = hub[0];
    const centerX = hub[1];
    const centerY = 75;
    that.mapCells[hubAlliance] = new Array(SIDE_NAMES.length * PIXELS_PER_FIXTURE);
    svg.appendChild(
      svgEl("rect", {
        x: centerX - halfSize,
        y: centerY - halfSize,
        width: halfSize * 2,
        height: halfSize * 2,
        class: "map-hub-outline" + (hubAlliance === that.alliance ? " current" : ""),
      })
    );
    SIDE_NAMES.forEach(function (name, side) {
      // Walk the edge from pixel 0 (left as seen from outside) to the last pixel. SVG y grows downward.
      const direction = SIDE_DIRECTIONS[hubAlliance][name] || [0, -1];
      const leftX = direction[1];
      const leftY = -direction[0];
      const pixelLength = (halfSize * 2) / PIXELS_PER_FIXTURE;
      for (let pixel = 0; pixel < PIXELS_PER_FIXTURE; pixel++) {
        const along = halfSize - (pixel + 0.5) * pixelLength;
        const x = centerX + direction[0] * (halfSize + strip / 2 + 1) + leftX * along;
        const y = centerY - (direction[1] * (halfSize + strip / 2 + 1) + leftY * along);
        const horizontal = direction[1] !== 0;
        const width = horizontal ? pixelLength - 0.6 : strip;
        const height = horizontal ? strip : pixelLength - 0.6;
        const cell = svgEl("rect", { x: x - width / 2, y: y - height / 2, width: width, height: height, fill: "#23262c" });
        that.mapCells[hubAlliance][side * PIXELS_PER_FIXTURE + pixel] = cell;
        svg.appendChild(cell);
      }
    });
    label(hubAlliance.toUpperCase(), centerX, centerY + 3);
  });
  wrapper.appendChild(svg);
  return wrapper;
};

// Paints the map strips for one Hub (the brightest of its fixtures at each position).
HubView.prototype.paintMap = function (hubAlliance, pixels) {
  const cells = this.mapCells[hubAlliance];
  for (let side = 0; side < SIDE_NAMES.length; side++) {
    for (let pixel = 0; pixel < PIXELS_PER_FIXTURE; pixel++) {
      const rgb = [0, 0, 0];
      for (let fixture = 0; fixture < FIXTURE_NAMES.length; fixture++) {
        const value = pixels[pixelIndex(side, fixture, pixel)];
        rgb[0] = Math.max(rgb[0], value[0]);
        rgb[1] = Math.max(rgb[1], value[1]);
        rgb[2] = Math.max(rgb[2], value[2]);
      }
      const key = (rgb[0] << 16) | (rgb[1] << 8) | rgb[2];
      const cell = cells[side * PIXELS_PER_FIXTURE + pixel];
      if (cell.ledKey !== key) {
        cell.ledKey = key;
        cell.setAttribute("fill", litColor(rgb));
      }
    }
  }
};

HubView.prototype.paintPixels = function (pixels) {
  for (let i = 0; i < PIXEL_COUNT; i++) {
    paintCell(this.flatCells[i], pixels[i]);
  }
  const overall = this.model.paint(pixels);
  const glow = fullBrightness(overall.rgb);
  this.modeSwatch.style.backgroundColor = overall.brightness > 0.02 ? rgba(glow, Math.min(1, 0.25 + overall.brightness)) : "";
};

// Updates the mode name, active badge, shift readout, and next change for this Hub.
HubView.prototype.paintStatus = function () {
  const frame = sim.frame;
  const mode = this.alliance === "red" ? frame.RedMode : frame.BlueMode;
  setText(this.modeName, modeName(mode));

  const state = matchStates[frame.MatchState];
  if (state === "AUTO_PERIOD" || state === "TELEOP_PERIOD") {
    const active = this.alliance === "red" ? frame.RedActive : frame.BlueActive;
    setText(this.activeBadge, active ? "ACTIVE" : "INACTIVE");
    this.activeBadge.dataset.active = active ? "true" : "false";
  } else {
    setText(this.activeBadge, state === "PAUSE_PERIOD" ? "PAUSE" : "NO MATCH");
    this.activeBadge.dataset.active = "";
  }
  setText(this.shiftText, frame.Shift ? frame.Shift + " · " + formatClock(frame.ShiftRemainingSec) + " left" : "");

  const matchTimeSec = sim.playheadTime();
  const next = matchTimeSec === null ? null : sim.nextModeChange(this.alliance, matchTimeSec);
  setText(
    this.nextText,
    next ? "Next: " + modeName(next.mode) + " in " + (next.atSec - matchTimeSec).toFixed(1) + " s" : ""
  );

  // Auto winner and Fuel, straight from the arena's scoring.
  const parts = [];
  if (frame.AutoWinner) {
    parts.push(capitalize(frame.AutoWinner) + " won auto");
  }
  const fieldStatus = sim.fieldStatus;
  if (fieldStatus) {
    const fuel = this.alliance === "red" ? fieldStatus.RedFuel : fieldStatus.BlueFuel;
    const counted = this.alliance === "red" ? fieldStatus.RedActiveFuel : fieldStatus.BlueActiveFuel;
    parts.push("Fuel " + fuel + " (" + counted + " while active)");
  }
  setText(this.detail, parts.join(" · "));
};

HubView.prototype.applyTransform = function () {
  this.rotator.style.transform = "rotateX(" + this.tilt + "deg) rotateY(" + this.yaw + "deg)";
};

// Turns the model so the given side faces the viewer, taking the short way around.
HubView.prototype.viewSide = function (side) {
  this.setSpinning(false);
  const target = -this.model.faceAngles[side];
  this.yaw = target + 360 * Math.round((this.yaw - target) / 360);
  this.tilt = DEFAULT_TILT;
  this.applyTransform();
};

// Shows a three-quarter view from the audience side.
HubView.prototype.resetView = function (animate) {
  this.setSpinning(false);
  const target = DEFAULT_YAW_OFFSET;
  this.yaw = animate === false ? target : target + 360 * Math.round((this.yaw - target) / 360);
  this.tilt = DEFAULT_TILT;
  this.applyTransform();
};

HubView.prototype.setSpinning = function (spinning) {
  if (spinning === this.spinning) {
    return;
  }
  this.spinning = spinning;
  this.stage.classList.toggle("spinning", spinning);
  if (this.spinButton) {
    this.spinButton.setAttribute("aria-pressed", spinning ? "true" : "false");
  }
  if (spinning) {
    const that = this;
    let lastTime = performance.now();
    const step = function (now) {
      if (!that.spinning) {
        return;
      }
      that.yaw -= ((now - lastTime) / 1000) * 20;
      lastTime = now;
      that.applyTransform();
      requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  }
};

// Lets the user drag to orbit the model.
HubView.prototype.bindOrbit = function () {
  const that = this;
  let drag = null;
  this.stage.addEventListener("pointerdown", function (event) {
    that.setSpinning(false);
    drag = { x: event.clientX, y: event.clientY, yaw: that.yaw, tilt: that.tilt };
    that.stage.setPointerCapture(event.pointerId);
    that.stage.classList.add("dragging");
  });
  this.stage.addEventListener("pointermove", function (event) {
    if (!drag) {
      return;
    }
    that.yaw = drag.yaw + (event.clientX - drag.x) * 0.5;
    that.tilt = Math.max(-80, Math.min(8, drag.tilt - (event.clientY - drag.y) * 0.3));
    that.applyTransform();
  });
  const endDrag = function () {
    drag = null;
    that.stage.classList.remove("dragging");
  };
  this.stage.addEventListener("pointerup", endDrag);
  this.stage.addEventListener("pointercancel", endDrag);
  this.stage.addEventListener("dblclick", function () {
    that.resetView();
  });
};

// Redraws whatever changed since the last update.
const render = function () {
  const frame = sim.frame;
  if (frame) {
    const hexes = { red: frame.Red, blue: frame.Blue };
    alliances.forEach(function (alliance) {
      const view = hubViews[alliance];
      if (view.lastHex !== hexes[alliance]) {
        view.lastHex = hexes[alliance];
        const pixels = parsePixels(hexes[alliance]);
        view.paintPixels(pixels);
        alliances.forEach(function (target) {
          hubViews[target].paintMap(alliance, pixels);
        });
      }
      view.paintStatus();
    });
    // Keep the other Hub on the map live in single-hub windows.
    if (alliances.length === 1) {
      const view = hubViews[alliances[0]];
      const other = alliances[0] === "red" ? "blue" : "red";
      if (view.otherHex !== hexes[other]) {
        view.otherHex = hexes[other];
        view.paintMap(other, parsePixels(hexes[other]));
      }
    }
    controls.update();
  }
  timelineView.render();
  const clock = sim.clock();
  setText(document.getElementById("matchStateText"), clock.state);
  setText(document.getElementById("matchClock"), clock.text);
};

const handleKey = function (event) {
  if (!isShortcutEvent(event)) {
    return;
  }
  const views = alliances.map(function (alliance) {
    return hubViews[alliance];
  });
  const sideKey = parseInt(event.key, 10);
  if (sideKey >= 1 && sideKey <= SIDE_NAMES.length) {
    views.forEach(function (view) {
      view.viewSide(sideKey - 1);
    });
  } else if (event.key === "0") {
    views.forEach(function (view) {
      view.resetView();
    });
  } else if (event.key === "s" || event.key === "S") {
    const spin = !views[0].spinning;
    views.forEach(function (view) {
      view.setSpinning(spin);
    });
  } else if (simConfig.CanControl && event.key === "ArrowRight") {
    connection.send("skipAhead", 5);
  } else if (simConfig.CanControl && (event.key === "n" || event.key === "N")) {
    controls.skipToNextChange();
  } else {
    return;
  }
  event.preventDefault();
};

$(function () {
  const container = document.getElementById("hubPanels");
  alliances.forEach(function (alliance) {
    hubViews[alliance] = new HubView(alliance, container);
  });
  timelineView = new SimTimelineView(sim, alliances, {
    shifts: document.getElementById("timelineShifts"),
    rows: document.getElementById("timelineRows"),
    playhead: document.getElementById("timelinePlayhead"),
    winnerButton: document.getElementById("timelineWinner"),
    legend: document.getElementById("timelineLegend"),
  }, simConfig.CanControl
    ? function (seconds) {
        connection.send("skipAhead", seconds);
      }
    : null);
  connection = connectSim("/hub_sim/websocket", sim, render);
  controls = new SimControls(sim, document.getElementById("controlsBody"), connection.send, alliances);
  document.addEventListener("keydown", handleKey);
});
