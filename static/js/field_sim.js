// Copyright 2026 Team 254. All Rights Reserved.
//
// Client-side logic for the field simulator: a CSS 3D model of the field whose Hubs, signs, lights, robots, and
// displays all come from the server (the real displays are embedded as-is), plus the timeline, controls, and status
// panels.
//
// World coordinates are in centimeters: x toward the blue driver stations, y up, z toward the audience. Dimensions
// are approximate and only meant to place things recognizably.

const FIELD_LENGTH = 1654;
const FIELD_WIDTH = 807;
const HUB_SIZE = 119;
const HUB_HEIGHT = 183;
const HUB_DISTANCE_FROM_WALL = 403;
const WALL_HEIGHT = 198;
const STATION_SPACING = 250;
const MONITOR_WIDTH = 120;
const MONITOR_HEIGHT_ABOVE_WALL = 62;
const SCREEN_WIDTH = 640;
const ROBOT_SIZE = 76;
const ROBOT_HEIGHT = 36;
const ROBOT_DISTANCE_FROM_WALL = 110;
const DISPLAY_WIDTH_PX = 1920;
const DISPLAY_HEIGHT_PX = 1080;
const PERSPECTIVE_PX = 1600;
const HIDDEN_DISPLAY_UNLOAD_MS = 15000;
// Robot states from dssim.RobotState: the signal light needs the roboRIO, and the robot is linked once code runs.
const RIO_LINKED_STATE = 3;
const CODE_RUNNING_STATE = 4;
const STATION_IDS = ["R1", "R2", "R3", "B1", "B2", "B3"];

// Where each driver station is along its wall; station 1 is on the drivers' left.
const STATION_Z = { R1: -STATION_SPACING, R2: 0, R3: STATION_SPACING, B1: STATION_SPACING, B2: 0, B3: -STATION_SPACING };

const CAMERA_PRESETS = [
  { name: "Audience", yaw: 0, tilt: -24, target: [0, 190, -160], zoom: 1.15 },
  { name: "Overhead", yaw: 0, tilt: -89, target: [0, 0, 0], zoom: 1.1 },
  { name: "Red wall", yaw: -90, tilt: -10, target: [-640, 200, 0], zoom: 0.34 },
  { name: "Blue wall", yaw: 90, tilt: -10, target: [640, 200, 0], zoom: 0.34 },
  { name: "Big screen", yaw: 0, tilt: -6, target: [0, 360, -560], zoom: 0.62 },
  { name: "Scoring table", yaw: 180, tilt: -28, target: [0, 80, -60], zoom: 1 },
  { name: "Red hub", yaw: 28, tilt: -16, target: [-(FIELD_LENGTH / 2 - HUB_DISTANCE_FROM_WALL), 90, 0], zoom: 0.36 },
  { name: "Blue hub", yaw: -28, tilt: -16, target: [FIELD_LENGTH / 2 - HUB_DISTANCE_FROM_WALL, 90, 0], zoom: 0.36 },
];

const sim = new SimState();
const hubs = {};
const signs = {};
const stopButtons = {};
const stationRows = {};
const robots = {};
const lights = {};
const screens = [];
let world;
let viewport;
let connection;
let timelineView;
let controls;
let camera = null;
let lastCoils = null;
let soundOn = loadPreference("fieldSound", true);
let displaysOn = loadPreference("fieldDisplays", true);
let saveCameraTimer = null;

// Converts world coordinates to a CSS translation.
const at = function (x, y, z) {
  return "translate3d(" + x + "px," + -y + "px," + z + "px)";
};

// Adds the element to the world at the given position, with optional extra transforms applied first.
const place = function (element, x, y, z, extra) {
  element.style.transform = at(x, y, z) + (extra ? " " + extra : "");
  world.appendChild(element);
  return element;
};

// A flat panel centered on its origin, facing the audience (+z) until rotated.
const panel = function (className, width, height) {
  const node = el("div", "w-panel " + (className || ""));
  node.style.width = width + "px";
  node.style.height = height + "px";
  node.style.marginLeft = -width / 2 + "px";
  node.style.marginTop = -height / 2 + "px";
  return node;
};

// A solid box centered on its origin. Returns the box with its top face as box.top.
const box = function (className, width, height, depth) {
  const node = el("div", "w-box " + (className || ""));
  const faces = [
    [width, height, "translateZ(" + depth / 2 + "px)", "front"],
    [width, height, "rotateY(180deg) translateZ(" + depth / 2 + "px)", "back"],
    [depth, height, "rotateY(90deg) translateZ(" + width / 2 + "px)", "side"],
    [depth, height, "rotateY(-90deg) translateZ(" + width / 2 + "px)", "side"],
    [width, depth, "rotateX(90deg) translateZ(" + height / 2 + "px)", "top"],
  ];
  faces.forEach(function (face) {
    const faceNode = panel("w-face w-face-" + face[3], face[0], face[1]);
    faceNode.style.transform = face[2];
    node.appendChild(faceNode);
    if (face[3] === "top") {
      node.top = faceNode;
    }
  });
  return node;
};

const stationUrl = function (stationId) {
  return (
    "/displays/alliance_station?displayId=fieldsim-" + stationId + "&nickname=" +
    encodeURIComponent("Field Sim " + stationId) + "&station=" + stationId
  );
};

const AUDIENCE_URL =
  "/displays/audience?displayId=fieldsim-audience&nickname=" + encodeURIComponent("Field Sim Audience") +
  "&background=%23000&reversed=false&overlayLocation=bottom";

// Builds the carpet with its markings and orientation labels.
const buildCarpet = function () {
  const carpet = panel("carpet", FIELD_LENGTH, FIELD_WIDTH);
  const zone = HUB_DISTANCE_FROM_WALL;
  const label = function (text, x, y, rotate, color) {
    return (
      '<text x="' + x + '" y="' + y + '" text-anchor="middle" font-size="44" font-weight="700" letter-spacing="6" ' +
      'fill="' + color + '"' + (rotate ? ' transform="rotate(' + rotate + " " + x + " " + y + ')"' : "") + ">" +
      text + "</text>"
    );
  };
  carpet.innerHTML =
    '<svg viewBox="0 0 ' + FIELD_LENGTH + " " + FIELD_WIDTH + '" preserveAspectRatio="none">' +
    '<rect x="0" y="0" width="' + zone + '" height="' + FIELD_WIDTH + '" fill="rgba(229,50,45,0.10)"/>' +
    '<rect x="' + (FIELD_LENGTH - zone) + '" y="0" width="' + zone + '" height="' + FIELD_WIDTH +
    '" fill="rgba(47,111,222,0.10)"/>' +
    '<line x1="' + zone + '" y1="0" x2="' + zone + '" y2="' + FIELD_WIDTH + '" stroke="#e5322d" stroke-width="5"/>' +
    '<line x1="' + (FIELD_LENGTH - zone) + '" y1="0" x2="' + (FIELD_LENGTH - zone) + '" y2="' + FIELD_WIDTH +
    '" stroke="#2f6fde" stroke-width="5"/>' +
    '<line x1="' + FIELD_LENGTH / 2 + '" y1="0" x2="' + FIELD_LENGTH / 2 + '" y2="' + FIELD_WIDTH +
    '" stroke="rgba(255,255,255,0.35)" stroke-width="5"/>' +
    label("SCORING TABLE SIDE", FIELD_LENGTH / 2, 70, 0, "rgba(255,255,255,0.22)") +
    label("AUDIENCE SIDE", FIELD_LENGTH / 2, FIELD_WIDTH - 40, 0, "rgba(255,255,255,0.22)") +
    label("RED ALLIANCE", 150, FIELD_WIDTH / 2, -90, "rgba(229,50,45,0.45)") +
    label("BLUE ALLIANCE", FIELD_LENGTH - 150, FIELD_WIDTH / 2, 90, "rgba(47,111,222,0.55)") +
    "</svg>";
  place(carpet, 0, 0, 0, "rotateX(90deg)");

  // Guardrails along both long sides.
  [-1, 1].forEach(function (side) {
    place(panel("guardrail", FIELD_LENGTH, 50), 0, 25, side * (FIELD_WIDTH / 2));
  });
};

// Builds one alliance wall with its station monitors, team signs, timer, stop buttons, and robots.
const buildAllianceWall = function (alliance) {
  const isRed = alliance === "red";
  const wallX = (isRed ? -1 : 1) * (FIELD_LENGTH / 2);
  const facing = isRed ? "rotateY(90deg)" : "rotateY(-90deg)";
  const behind = isRed ? -1 : 1;

  const wall = panel("alliance-wall alliance-wall-" + alliance, FIELD_WIDTH, WALL_HEIGHT);
  for (let i = 0; i < 3; i++) {
    wall.appendChild(el("div", "wall-station"));
  }
  place(wall, wallX, WALL_HEIGHT / 2, 0, facing);

  ["1", "2", "3"].forEach(function (number) {
    const stationId = (isRed ? "R" : "B") + number;
    const z = STATION_Z[stationId];

    // The monitor above the station shows the real alliance station display.
    place(
      buildScreen(stationUrl(stationId), "Alliance station display " + stationId, "monitor"),
      wallX - behind * 4,
      WALL_HEIGHT + MONITOR_HEIGHT_ABOVE_WALL,
      z,
      facing + " scale(" + MONITOR_WIDTH / DISPLAY_WIDTH_PX + ")"
    );
    place(panel("monitor-post", 6, MONITOR_HEIGHT_ABOVE_WALL - 20), wallX + behind * 2, WALL_HEIGHT + 10, z, facing);

    // The team number sign on top of the wall: front faces the field, rear faces the drivers.
    signs[stationId] = buildSign(wallX, WALL_HEIGHT + 12, z, facing);
    place(el("div", "station-label", stationId), wallX - behind * 3, WALL_HEIGHT - 30, z, facing);

    // The driver station shelf with its E-stop and A-stop buttons.
    place(box("shelf", 150, 6, 50), wallX + behind * 30, 95, z);
    stopButtons[stationId] = {
      eStop: buildStopButton(stationId, false, wallX + behind * 30, 102, z - 25),
      aStop: buildStopButton(stationId, true, wallX + behind * 30, 102, z + 25),
    };

    robots[stationId] = buildRobot(alliance, stationId, wallX - behind * ROBOT_DISTANCE_FROM_WALL, z);
  });

  // The timer sign at the audience end of the wall.
  signs[alliance + "Timer"] = buildSign(wallX, WALL_HEIGHT + 12, FIELD_WIDTH / 2 - 60, facing);
};

// Builds a two-sided team number / timer sign.
const buildSign = function (x, y, z, facing) {
  const sign = el("div", "w-sign");
  const front = panel("sign-face sign-front", 70, 18);
  const rear = panel("sign-face sign-rear", 70, 18);
  rear.style.transform = "rotateY(180deg)";
  sign.appendChild(front);
  sign.appendChild(rear);
  place(sign, x, y, z, facing);
  return { front: front, rear: rear };
};

const buildStopButton = function (stationId, aStop, x, y, z) {
  const button = el("div", "w-stop " + (aStop ? "a-stop" : "e-stop"));
  button.dataset.action = "stop";
  button.dataset.station = stationId;
  button.dataset.astop = aStop ? "true" : "false";
  button.title = stationId + (aStop ? " A-stop" : " E-stop") + " (click to press or release)";
  button.appendChild(el("span", "", aStop ? "A" : "E"));
  place(button, x, y, z, "rotateX(90deg)");
  return button;
};

// Builds a simulated robot: bumpers in the alliance color, the team number on top, and its signal light.
const buildRobot = function (alliance, stationId, x, z) {
  const robot = box("robot robot-" + alliance, ROBOT_SIZE, ROBOT_HEIGHT, ROBOT_SIZE);
  robot.dataset.state = "off";
  const number = el("div", "robot-number");
  const signalLight = el("div", "robot-rsl");
  robot.top.appendChild(number);
  robot.top.appendChild(signalLight);
  robot.title = stationId + " simulated robot";
  place(robot, x, ROBOT_HEIGHT / 2, z);
  return { element: robot, number: number };
};

// Builds an embedded display: the real page at full size, scaled down by the caller.
const buildScreen = function (url, title, className) {
  const screen = el("div", "w-screen " + className);
  screen.style.width = DISPLAY_WIDTH_PX + "px";
  screen.style.height = DISPLAY_HEIGHT_PX + "px";
  screen.style.marginLeft = -DISPLAY_WIDTH_PX / 2 + "px";
  screen.style.marginTop = -DISPLAY_HEIGHT_PX / 2 + "px";
  screen.dataset.action = "screen";
  screen.dataset.url = url;
  screen.dataset.title = title;
  screen.title = title + " (click to enlarge)";
  screen.appendChild(el("div", "screen-off", "Display off"));
  screens.push(screen);
  setScreenLive(screen, displaysOn);
  return screen;
};

// Loads or unloads a screen's display page.
const setScreenLive = function (screen, live) {
  let frame = screen.querySelector("iframe");
  if (live && !frame) {
    frame = el("iframe");
    frame.setAttribute("tabindex", "-1");
    frame.addEventListener("load", function () {
      applySound(frame);
    });
    frame.src = screen.dataset.url;
    screen.appendChild(frame);
  } else if (!live && frame) {
    frame.remove();
  }
  screen.classList.toggle("live", live);
};

// Mutes or unmutes the sounds the displays play (the audience display plays the match sounds).
const applySound = function (frame) {
  try {
    frame.contentDocument.querySelectorAll("audio, video").forEach(function (media) {
      media.muted = !soundOn;
    });
  } catch (e) {
    // The display hasn't loaded yet; it is muted when it finishes loading.
  }
};

const applySoundEverywhere = function () {
  document.querySelectorAll("iframe").forEach(applySound);
};

// Builds the scoring table with the field stack light and field reset light.
const buildScoringTable = function () {
  const tableZ = -(FIELD_WIDTH / 2 + 150);
  place(box("scoring-table", 460, 90, 80), 0, 45, tableZ);

  const stack = el("div", "w-stack");
  ["stackLightRed", "stackLightBlue", "stackLightOrange", "stackLightGreen"].forEach(function (coilName) {
    const light = el("div", "stack-light " + coilName);
    light.title = coilName;
    stack.appendChild(light);
    lights[coilName] = light;
  });
  const buzzer = el("div", "stack-buzzer", "BUZZER");
  buzzer.title = "stackLightBuzzer";
  stack.appendChild(buzzer);
  lights.stackLightBuzzer = buzzer;
  place(stack, 190, 160, tableZ);
  place(panel("stack-pole", 4, 60), 190, 120, tableZ);

  const resetLight = el("div", "field-reset-light");
  resetLight.title = "fieldResetLight";
  lights.fieldResetLight = resetLight;
  place(resetLight, -190, 120, tableZ);
};

// Builds the big audience screen behind the scoring table.
const buildAudienceScreen = function () {
  const z = -(FIELD_WIDTH / 2 + 330);
  const y = 430;
  place(panel("screen-truss", SCREEN_WIDTH + 30, SCREEN_WIDTH * 0.5625 + 30), 0, y, z - 3);
  place(buildScreen(AUDIENCE_URL, "Audience display", "big-screen"), 0, y, z, "scale(" + SCREEN_WIDTH / DISPLAY_WIDTH_PX + ")");
  [-1, 1].forEach(function (side) {
    place(panel("screen-leg", 8, y - 150), side * (SCREEN_WIDTH / 2 - 40), (y - 150) / 2, z - 3);
  });
};

const buildHubs = function () {
  ["red", "blue"].forEach(function (alliance) {
    const model = new HubModel(alliance, false);
    const holder = el("div", "w-hub");
    holder.style.setProperty("--hub-w", HUB_SIZE + "px");
    holder.style.setProperty("--hub-h", HUB_HEIGHT + "px");
    holder.dataset.action = "hub";
    holder.dataset.alliance = alliance;
    holder.title = capitalize(alliance) + " hub (click to zoom in)";
    holder.appendChild(model.element);
    const x = (alliance === "red" ? -1 : 1) * (FIELD_LENGTH / 2 - HUB_DISTANCE_FROM_WALL);
    place(holder, x, HUB_HEIGHT / 2, 0);
    hubs[alliance] = { model: model, holder: holder, lastHex: null };
  });
};

// Camera handling.
const defaultDistance = function () {
  const width = viewport.clientWidth || 1200;
  return Math.max(250, PERSPECTIVE_PX * (2000 / (0.92 * width)) - PERSPECTIVE_PX);
};

const applyCamera = function () {
  const distance = defaultDistance() * camera.zoom;
  world.style.transform =
    "translateZ(" + -distance + "px) rotateX(" + camera.tilt + "deg) rotateY(" + camera.yaw + "deg) " +
    at(-camera.target[0], -camera.target[1], -camera.target[2]);
  clearTimeout(saveCameraTimer);
  saveCameraTimer = setTimeout(function () {
    savePreference("fieldCamera", camera);
  }, 500);
};

const setCamera = function (preset, animate) {
  const yaw = camera ? preset.yaw + 360 * Math.round((camera.yaw - preset.yaw) / 360) : preset.yaw;
  camera = { yaw: yaw, tilt: preset.tilt, target: preset.target.slice(), zoom: preset.zoom };
  world.classList.toggle("animating", animate !== false);
  applyCamera();
};

// Returns the saved camera if it looks valid.
const savedCamera = function () {
  const saved = loadPreference("fieldCamera", null);
  const isNumber = function (value) {
    return typeof value === "number" && isFinite(value);
  };
  if (
    saved && isNumber(saved.yaw) && isNumber(saved.tilt) && isNumber(saved.zoom) && Array.isArray(saved.target) &&
    saved.target.length === 3 && saved.target.every(isNumber)
  ) {
    return {
      yaw: saved.yaw,
      tilt: Math.max(-89, Math.min(-2, saved.tilt)),
      target: saved.target,
      zoom: Math.max(0.15, Math.min(3, saved.zoom)),
    };
  }
  return null;
};

const bindCamera = function () {
  let drag = null;
  viewport.addEventListener("pointerdown", function (event) {
    if (event.target.closest(".viewport-overlay")) {
      return;
    }
    world.classList.remove("animating");
    drag = {
      x: event.clientX,
      y: event.clientY,
      startX: event.clientX,
      startY: event.clientY,
      pan: event.button === 2 || event.shiftKey,
      target: event.target,
    };
    viewport.setPointerCapture(event.pointerId);
  });
  viewport.addEventListener("pointermove", function (event) {
    if (!drag) {
      return;
    }
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    drag.x = event.clientX;
    drag.y = event.clientY;
    if (drag.pan) {
      // Pan along the floor relative to the current view direction.
      const scale = (defaultDistance() * camera.zoom + PERSPECTIVE_PX) / PERSPECTIVE_PX;
      const yawRad = (camera.yaw * Math.PI) / 180;
      camera.target[0] -= (dx * Math.cos(yawRad) - dy * Math.sin(yawRad)) * scale;
      camera.target[2] -= (dx * Math.sin(yawRad) + dy * Math.cos(yawRad)) * scale;
      camera.target[0] = Math.max(-FIELD_LENGTH, Math.min(FIELD_LENGTH, camera.target[0]));
      camera.target[2] = Math.max(-FIELD_LENGTH, Math.min(FIELD_LENGTH, camera.target[2]));
    } else {
      camera.yaw += dx * 0.35;
      camera.tilt = Math.max(-89, Math.min(-2, camera.tilt - dy * 0.25));
    }
    applyCamera();
  });
  viewport.addEventListener("pointerup", function (event) {
    if (drag && Math.abs(event.clientX - drag.startX) + Math.abs(event.clientY - drag.startY) < 5) {
      activate(drag.target);
    }
    drag = null;
  });
  viewport.addEventListener("pointercancel", function () {
    drag = null;
  });
  viewport.addEventListener("contextmenu", function (event) {
    event.preventDefault();
  });
  viewport.addEventListener(
    "wheel",
    function (event) {
      event.preventDefault();
      world.classList.remove("animating");
      camera.zoom = Math.max(0.15, Math.min(3, camera.zoom * Math.exp(event.deltaY * 0.0012)));
      applyCamera();
    },
    { passive: false }
  );
  viewport.addEventListener("dblclick", function (event) {
    if (!event.target.closest("[data-action]") && !event.target.closest(".viewport-overlay")) {
      setCamera(CAMERA_PRESETS[0]);
    }
  });
  window.addEventListener("resize", applyCamera);

  const buttons = document.getElementById("cameraButtons");
  CAMERA_PRESETS.forEach(function (preset, index) {
    const button = el("button", "", index + 1 + " " + preset.name);
    button.type = "button";
    button.addEventListener("click", function () {
      setCamera(preset);
    });
    buttons.appendChild(button);
  });
};

// Handles a click on something in the world.
const activate = function (target) {
  const element = target && target.closest ? target.closest("[data-action]") : null;
  if (!element) {
    return;
  }
  switch (element.dataset.action) {
    case "screen":
      openScreen(element.dataset.url, element.dataset.title);
      break;
    case "hub":
      setCamera(
        CAMERA_PRESETS.find(function (preset) {
          return preset.name === capitalize(element.dataset.alliance) + " hub";
        })
      );
      break;
    case "stop":
      toggleStop(element.dataset.station, element.dataset.astop === "true");
      break;
  }
};

const findStation = function (stationId) {
  if (!sim.fieldStatus) {
    return null;
  }
  return (
    sim.fieldStatus.Stations.find(function (station) {
      return station.Id === stationId;
    }) || null
  );
};

const toggleStop = function (stationId, aStop) {
  const station = findStation(stationId);
  if (!station || !sim.fieldStatus.SimulatedPlc) {
    showToast("Stop buttons can only be pressed here with the simulated PLC.");
    return;
  }
  const pressed = aStop ? station.AStopPressed : station.EStopPressed;
  if (!pressed && !confirmRealMatch(sim, "Pressing this stop button affects that match's robot.")) {
    return;
  }
  connection.send("setStationStop", { Station: stationId, AStop: aStop, Pressed: !pressed });
};

const setRobotState = function (stationId, state) {
  if (state !== 0 && !confirmRealMatch(sim, "Simulated driver stations take over from any real ones for its teams.")) {
    return;
  }
  connection.send("setRobotState", { Station: stationId, State: state });
};

// Shows a display large in an overlay (a second connection to the same display).
const openScreen = function (url, title) {
  const overlay = document.getElementById("screenOverlay");
  const holder = document.getElementById("screenOverlayFrame");
  holder.textContent = "";
  const frame = el("iframe");
  frame.addEventListener("load", function () {
    applySound(frame);
  });
  frame.src = url;
  holder.appendChild(frame);
  setText(document.getElementById("screenOverlayTitle"), title);
  overlay.hidden = false;
  fitOverlay();
};

const closeScreen = function () {
  document.getElementById("screenOverlay").hidden = true;
  document.getElementById("screenOverlayFrame").textContent = "";
};

const fitOverlay = function () {
  const holder = document.getElementById("screenOverlayFrame");
  const frame = holder.querySelector("iframe");
  if (!frame) {
    return;
  }
  const scale = Math.min((window.innerWidth * 0.9) / DISPLAY_WIDTH_PX, (window.innerHeight * 0.8) / DISPLAY_HEIGHT_PX);
  holder.style.width = DISPLAY_WIDTH_PX * scale + "px";
  holder.style.height = DISPLAY_HEIGHT_PX * scale + "px";
  frame.style.transform = "scale(" + scale + ")";
};

// Builds the driver station table in the drawer.
const buildStationTable = function () {
  const table = document.getElementById("stationTable");
  const stateIds = Object.keys(simConfig.RobotStates)
    .map(Number)
    .sort(function (a, b) {
      return a - b;
    });
  STATION_IDS.forEach(function (stationId) {
    const row = el("div", "station-row");
    row.dataset.alliance = stationId[0] === "R" ? "red" : "blue";
    const top = el("div", "station-row-main");
    top.appendChild(el("span", "station-id", stationId));
    const team = el("span", "station-team", "-");
    top.appendChild(team);

    const robotSelect = el("select", "robot-select");
    robotSelect.title = "Simulated robot at " + stationId;
    stateIds.forEach(function (state) {
      const option = el("option", "", simConfig.RobotStates[state]);
      option.value = state;
      robotSelect.appendChild(option);
    });
    robotSelect.addEventListener("change", function () {
      setRobotState(stationId, parseInt(robotSelect.value, 10));
      robotSelect.blur();
    });
    top.appendChild(robotSelect);

    const robotStatus = el("span", "robot-status");
    top.appendChild(robotStatus);
    const eStop = el("button", "estop-button", "E-stop");
    eStop.type = "button";
    eStop.addEventListener("click", function () {
      toggleStop(stationId, false);
    });
    const aStop = el("button", "estop-button a-stop", "A-stop");
    aStop.type = "button";
    aStop.addEventListener("click", function () {
      toggleStop(stationId, true);
    });
    const view = el("button", "view-button", "View");
    view.type = "button";
    view.title = "Open " + stationId + "'s alliance station display";
    view.addEventListener("click", function () {
      openScreen(stationUrl(stationId), "Alliance station display " + stationId);
    });
    [eStop, aStop, view].forEach(function (node) {
      top.appendChild(node);
    });
    row.appendChild(top);

    const detail = el("div", "station-row-detail");
    const sign = el("span", "station-sign");
    sign.title = "Front of the team sign";
    const rear = el("span", "station-rear");
    rear.title = "Back of the team sign (facing the drivers)";
    const flags = el("span", "station-flags");
    [sign, rear, flags].forEach(function (node) {
      detail.appendChild(node);
    });
    row.appendChild(detail);
    table.appendChild(row);
    stationRows[stationId] = {
      team: team,
      robotSelect: robotSelect,
      robotStatus: robotStatus,
      sign: sign,
      rear: rear,
      flags: flags,
      eStop: eStop,
      aStop: aStop,
    };
  });

  // Bulk robot and team controls.
  const bulk = document.getElementById("robotBulk");
  const bulkButton = function (text, title, onClick) {
    const button = el("button", "btn btn-small", text);
    button.type = "button";
    button.title = title;
    button.addEventListener("click", onClick);
    bulk.appendChild(button);
    return button;
  };
  const allOn = stateIds[stateIds.length - 1];
  bulkButton("All robots on", "Bring every station's robot up with code running", function () {
    setRobotState("all", allOn);
  });
  bulkButton("All off", "Turn every simulated robot off", function () {
    setRobotState("all", 0);
  });
  stationRows.useTeams = bulkButton(
    "Use event teams",
    "Put the first six event teams into test matches so robots have teams to connect as",
    function () {
      connection.send("useEventTeams");
    }
  );
  stationRows.clearTeams = bulkButton("Clear teams", "Go back to empty test matches", function () {
    connection.send("clearSimTeams");
  });
  if (simConfig.NetworkSecurity) {
    const warning = el(
      "div",
      "network-warning",
      "Network security is on in Settings, so loading teams here configures the field access point and switch " +
        "for them, just like loading a real match."
    );
    bulk.parentElement.insertBefore(warning, bulk.nextSibling);
  }

  document.getElementById("fieldEStop").addEventListener("click", function () {
    const status = sim.fieldStatus;
    if (!status || !status.SimulatedPlc) {
      showToast("The field E-stop can only be pressed here with the simulated PLC.");
      return;
    }
    if (!status.FieldEStopPressed && !confirmRealMatch(sim, "The field E-stop aborts it.")) {
      return;
    }
    connection.send("setFieldEStop", !status.FieldEStopPressed);
  });
  document.getElementById("viewAudience").addEventListener("click", function () {
    openScreen(AUDIENCE_URL, "Audience display");
  });
};

const buildCoilList = function () {
  const list = document.getElementById("coilList");
  simConfig.CoilNames.forEach(function (name, index) {
    const coil = el("span", "coil", name);
    coil.dataset.index = index;
    list.appendChild(coil);
  });
};

// Builds the sound and display toggles and the quick match controls over the 3D view.
const buildToggles = function () {
  const soundButton = document.getElementById("soundToggle");
  const displaysButton = document.getElementById("displaysToggle");
  const paint = function () {
    setText(soundButton, soundOn ? "Sound on" : "Sound off");
    soundButton.setAttribute("aria-pressed", soundOn ? "true" : "false");
    setText(displaysButton, displaysOn ? "Displays live" : "Displays off");
    displaysButton.setAttribute("aria-pressed", displaysOn ? "true" : "false");
  };
  soundButton.addEventListener("click", function () {
    soundOn = !soundOn;
    savePreference("fieldSound", soundOn);
    applySoundEverywhere();
    paint();
  });
  displaysButton.addEventListener("click", function () {
    displaysOn = !displaysOn;
    savePreference("fieldDisplays", displaysOn);
    screens.forEach(function (screen) {
      setScreenLive(screen, displaysOn);
    });
    paint();
  });
  paint();

  const quick = document.getElementById("quickControls");
  if (!simConfig.CanControl) {
    quick.remove();
    return;
  }
  const quickButton = function (text, source, onClick) {
    const button = el("button", "btn", text);
    button.type = "button";
    button.addEventListener("click", onClick);
    button.dataset.source = source;
    quick.appendChild(button);
  };
  quickButton("▶ Start", "start", function () {
    controls.startMatch();
  });
  quickButton("■ Abort", "abort", function () {
    connection.send("abortMatch");
  });
  quickButton("+5 s", "skip", function () {
    connection.send("skipAhead", 5);
  });
  quickButton("Next change ▸", "skip", function () {
    controls.skipToNextChange();
  });
};

// Mirrors the enabled state and reason of the main controls onto the quick controls.
const paintQuickControls = function () {
  const quick = document.getElementById("quickControls");
  if (!quick || !controls.controls.start) {
    return;
  }
  quick.querySelectorAll("button").forEach(function (button) {
    const source = button.dataset.source === "skip" ? controls.controls.skips[0] : controls.controls[button.dataset.source];
    button.disabled = source.disabled;
    button.title = source.title;
  });
};

const coilOn = function (name) {
  if (!sim.fieldStatus) {
    return false;
  }
  const index = simConfig.CoilNames.indexOf(name);
  return index >= 0 && sim.fieldStatus.Coils.charAt(index) === "1";
};

const paintSign = function (sign, data) {
  setText(sign.front, data.FrontText.replace(/ /g, " "));
  if (sign.front.dataset.color !== data.FrontColor) {
    sign.front.dataset.color = data.FrontColor;
    sign.front.style.color = data.FrontColor;
    sign.front.style.textShadow = data.FrontColor === "#000000" ? "none" : "0 0 6px " + data.FrontColor;
  }
  setText(sign.rear, data.RearText.replace(/ /g, " "));
};

// Describes what a simulated robot was last told by the arena.
const robotStatusText = function (robot) {
  switch (robot.Connection) {
    case "off":
      return robot.State === 0 ? "" : "Waiting for a team";
    case "connecting":
      return "Connecting…";
    case "rejected":
      return "Not in this match";
  }
  let text;
  if (robot.EStop) {
    text = "E-stopped";
  } else if (robot.AStop) {
    text = "A-stopped";
  } else if (robot.Enabled) {
    text = robot.Auto ? "Enabled (auto)" : "Enabled (teleop)";
  } else {
    text = "Disabled";
  }
  if (robot.GameData) {
    text += " · data " + robot.GameData;
  }
  // The field enables the Driver Station whether or not the robot behind it is linked.
  if (robot.State < CODE_RUNNING_STATE) {
    text += " · robot not linked";
  }
  return text;
};

// Paints everything that comes from the field status: signs, buttons, lights, robots, and the drawer panels.
const paintFieldStatus = function () {
  const status = sim.fieldStatus;
  if (!status) {
    return;
  }
  setText(document.getElementById("matchName"), status.MatchName);

  status.Stations.forEach(function (station, index) {
    paintSign(signs[station.Id], station.Sign);
    stopButtons[station.Id].eStop.dataset.pressed = station.EStopPressed ? "true" : "false";
    stopButtons[station.Id].aStop.dataset.pressed = station.AStopPressed ? "true" : "false";

    const robot = status.Robots[index];
    const row = stationRows[station.Id];
    setText(row.team, station.TeamId ? String(station.TeamId) : "-");
    setText(row.sign, station.Sign.FrontText.trim() || "-");
    if (row.sign.dataset.color !== station.Sign.FrontColor) {
      row.sign.dataset.color = station.Sign.FrontColor;
      row.sign.style.color = station.Sign.FrontColor === "#000000" ? "" : station.Sign.FrontColor;
    }
    setText(row.rear, station.Sign.RearText.trim());
    const flags = [];
    if (station.Bypass) {
      flags.push("BYPASSED");
    }
    if (station.EStop) {
      flags.push("E-STOP");
    }
    if (station.AStop) {
      flags.push("A-STOP");
    }
    setText(row.flags, flags.join(" "));
    row.eStop.dataset.pressed = station.EStopPressed ? "true" : "false";
    row.aStop.dataset.pressed = station.AStopPressed ? "true" : "false";
    row.eStop.disabled = !status.SimulatedPlc || !simConfig.CanControl;
    row.aStop.disabled = !status.SimulatedPlc || !simConfig.CanControl;
    row.robotSelect.disabled = !simConfig.CanControl;
    if (document.activeElement !== row.robotSelect) {
      row.robotSelect.value = String(robot.State);
    }
    setText(row.robotStatus, robotStatusText(robot));
    row.robotStatus.dataset.connection = robot.Connection;
    row.robotStatus.dataset.enabled = robot.Enabled ? "true" : "false";

    // The 3D robot shows up once its Driver Station is running; its signal light is powered by the roboRIO and
    // blinks while the robot is enabled.
    const model = robots[station.Id];
    let robotState = "off";
    if (robot.State > 0 && robot.Connection !== "off") {
      robotState = "on";
      if (robot.Connection === "connected" && robot.State >= RIO_LINKED_STATE) {
        robotState = robot.Enabled ? "enabled" : "disabled";
      }
    }
    model.element.dataset.state = robotState;
    setText(model.number, robot.TeamId ? String(robot.TeamId) : "");
  });
  paintSign(signs.redTimer, status.RedTimer);
  paintSign(signs.blueTimer, status.BlueTimer);

  const fieldEStop = document.getElementById("fieldEStop");
  fieldEStop.dataset.pressed = status.FieldEStopPressed ? "true" : "false";
  setText(fieldEStop, status.FieldEStopPressed ? "Field E-stop PRESSED" : "Field E-stop");
  fieldEStop.title = status.FieldEStopPressed ? "Click to release the field E-stop" : "Click to press the field E-stop";
  fieldEStop.disabled = !status.SimulatedPlc || !simConfig.CanControl;
  if (stationRows.useTeams) {
    stationRows.useTeams.disabled = !simConfig.CanControl;
    stationRows.clearTeams.disabled = !simConfig.CanControl || !status.SimTeams;
  }

  const summary = [];
  summary.push(status.SimulatedPlc ? "Simulated PLC" : status.PlcEnabled ? "Real PLC" : "No PLC (outputs not driven)");
  summary.push("FTA ready: " + (status.FtaReady ? "yes" : "no"));
  setText(document.getElementById("plcSummary"), summary.join(" · "));
  const conditions = document.getElementById("startConditions");
  setText(conditions, status.CanStartMatch ? "The arena would start a match now." : "Arena start check: " + status.StartConditions);
  conditions.dataset.ok = status.CanStartMatch ? "true" : "false";

  if (status.Coils !== lastCoils) {
    lastCoils = status.Coils;
    document.querySelectorAll("#coilList .coil").forEach(function (coil) {
      coil.dataset.on = status.Coils.charAt(parseInt(coil.dataset.index, 10)) === "1" ? "true" : "false";
    });
    for (const name in lights) {
      lights[name].dataset.on = coilOn(name) ? "true" : "false";
    }
    ["red", "blue"].forEach(function (alliance) {
      hubs[alliance].model.setBeacon(coilOn(alliance + "HubLight"));
      hubs[alliance].model.element.dataset.motor = coilOn(alliance + "HubMotor") ? "true" : "false";
    });
  }
};

// Shows each Hub's mode, active state, Fuel, and motor in the header.
const paintHubChips = function () {
  const container = document.getElementById("hubChips");
  const frame = sim.frame;
  if (!frame) {
    return;
  }
  if (!container.childElementCount) {
    ["red", "blue"].forEach(function (alliance) {
      const chip = el("a", "hub-chip");
      chip.href = "/hub_sim?alliance=" + alliance;
      chip.target = "_blank";
      chip.title = "Open the " + alliance + " hub in its own window";
      chip.dataset.alliance = alliance;
      chip.appendChild(el("b", "", capitalize(alliance) + " hub"));
      chip.appendChild(el("span", "hub-chip-mode"));
      chip.appendChild(el("span", "hub-chip-detail"));
      container.appendChild(chip);
    });
  }
  const state = matchStates[frame.MatchState];
  const inMatch = state === "AUTO_PERIOD" || state === "TELEOP_PERIOD";
  container.querySelectorAll(".hub-chip").forEach(function (chip) {
    const alliance = chip.dataset.alliance;
    const isRed = alliance === "red";
    setText(chip.querySelector(".hub-chip-mode"), modeName(isRed ? frame.RedMode : frame.BlueMode));
    const parts = [];
    if (inMatch) {
      parts.push((isRed ? frame.RedActive : frame.BlueActive) ? "active" : "inactive");
    }
    if (sim.fieldStatus) {
      parts.push("fuel " + (isRed ? sim.fieldStatus.RedFuel : sim.fieldStatus.BlueFuel));
      if (coilOn(alliance + "HubMotor")) {
        parts.push("motor on");
      }
    }
    if (frame.AutoWinner === alliance) {
      parts.push("won auto");
    }
    setText(chip.querySelector(".hub-chip-detail"), parts.join(" · "));
  });
};

const render = function () {
  const frame = sim.frame;
  if (frame) {
    const hexes = { red: frame.Red, blue: frame.Blue };
    ["red", "blue"].forEach(function (alliance) {
      const hub = hubs[alliance];
      if (hub.lastHex !== hexes[alliance]) {
        hub.lastHex = hexes[alliance];
        hub.model.paint(parsePixels(hexes[alliance]));
      }
    });
    paintHubChips();
    controls.update();
    paintQuickControls();
  }
  paintFieldStatus();
  timelineView.render();
  const clock = sim.clock();
  setText(document.getElementById("matchStateText"), clock.state);
  setText(document.getElementById("matchClock"), clock.text);
};

const toggleDrawer = function () {
  const hidden = document.body.classList.toggle("drawer-hidden");
  savePreference("fieldDrawerHidden", hidden);
  setText(document.getElementById("drawerToggle"), hidden ? "Show panel (H)" : "Hide panel (H)");
  applyCamera();
};

const toggleHelp = function (show) {
  const help = document.getElementById("helpPanel");
  help.hidden = show === undefined ? !help.hidden : !show;
};

const handleKey = function (event) {
  if (!isShortcutEvent(event)) {
    return;
  }
  const presetIndex = parseInt(event.key, 10) - 1;
  if (presetIndex >= 0 && presetIndex < CAMERA_PRESETS.length) {
    setCamera(CAMERA_PRESETS[presetIndex]);
  } else if (event.key === "Escape") {
    closeScreen();
    toggleHelp(false);
  } else if (event.key === "h" || event.key === "H") {
    toggleDrawer();
  } else if (event.key === "?") {
    toggleHelp();
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
  world = document.getElementById("world");
  viewport = document.getElementById("viewport");
  viewport.style.perspective = PERSPECTIVE_PX + "px";

  buildCarpet();
  buildAllianceWall("red");
  buildAllianceWall("blue");
  buildScoringTable();
  buildAudienceScreen();
  buildHubs();
  buildStationTable();
  buildCoilList();
  bindCamera();
  const saved = savedCamera();
  if (saved) {
    camera = saved;
    applyCamera();
  } else {
    setCamera(CAMERA_PRESETS[0], false);
  }
  if (loadPreference("fieldDrawerHidden", false)) {
    toggleDrawer();
  }

  timelineView = new SimTimelineView(
    sim,
    ["red", "blue"],
    {
      shifts: document.getElementById("timelineShifts"),
      rows: document.getElementById("timelineRows"),
      playhead: document.getElementById("timelinePlayhead"),
      winnerButton: document.getElementById("timelineWinner"),
      legend: document.getElementById("timelineLegend"),
    },
    simConfig.CanControl
      ? function (seconds) {
          connection.send("skipAhead", seconds);
        }
      : null
  );
  connection = connectSim("/field_sim/websocket", sim, render);
  controls = new SimControls(sim, document.getElementById("controlsBody"), connection.send, ["red", "blue"]);
  buildToggles();

  document.getElementById("screenOverlayClose").addEventListener("click", closeScreen);
  document.getElementById("screenOverlay").addEventListener("click", function (event) {
    if (event.target.id === "screenOverlay") {
      closeScreen();
    }
  });
  document.getElementById("drawerToggle").addEventListener("click", toggleDrawer);
  document.getElementById("helpToggle").addEventListener("click", function () {
    toggleHelp();
  });
  document.getElementById("helpClose").addEventListener("click", function () {
    toggleHelp(false);
  });
  window.addEventListener("resize", fitOverlay);
  document.addEventListener("keydown", handleKey);

  // Unload the embedded displays while the page stays hidden, since a hidden page can stop reading their updates and
  // hold up the server's notifications to them; bring them back when the page is shown again.
  let hiddenTimer = null;
  document.addEventListener("visibilitychange", function () {
    clearTimeout(hiddenTimer);
    if (document.visibilityState === "hidden") {
      hiddenTimer = setTimeout(function () {
        screens.forEach(function (screen) {
          setScreenLive(screen, false);
        });
      }, HIDDEN_DISPLAY_UNLOAD_MS);
    } else if (displaysOn) {
      screens.forEach(function (screen) {
        setScreenLive(screen, true);
      });
    }
  });
});
