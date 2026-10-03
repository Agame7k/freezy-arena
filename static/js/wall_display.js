// Copyright 2023 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side methods for the wall display.

if (typeof DisplayShared === "undefined") {
  $.ajax({async: false, cache: true, dataType: "script", url: "/static/js/display_shared.js"});
}

var websocket;
let transitionMap;
const transitionQueue = [];
let transitionInProgress = false;
let currentScreen = "blank";
let redSide;
let blueSide;
let currentMatch;
let messageText = "";
let hasMessage = false;
const hubActiveController = DisplayShared.createHubActiveController(function () {
  return currentScreen;
});

// Constants for overlay positioning. The CSS is the source of truth for the values that represent initial state.
const eventMatchInfoDown = "30px";
const eventMatchInfoUp = $("#eventMatchInfo").css("height");
const logoUp = "35px";
const logoDown = $("#logo").css("top");
const scoreIn = $(".score").css("width");
const scoreMid = "185px";
const scoreOut = "250px";
const scoreFieldsOut = "25px";
const overlayTopOffset = 110;
const timeoutDetailsIn = $("#timeoutDetails").css("width");
const timeoutDetailsOut = "570px";

// Entrance flourishes layered on top of the jQuery transitions below. They use a backwards fill only, so they hold their
// starting frame through any delay and then leave no inline style behind once finished.
const flourishEase = "cubic-bezier(0.16, 1, 0.3, 1)";
const flourishPop = "cubic-bezier(0.34, 1.56, 0.64, 1)";
const flourish = function (selector, keyframes, duration, delay = 0, stagger = 0, easing = flourishEase) {
  $(selector).each(function (i) {
    this.animate(keyframes, {duration: duration, delay: delay + i * stagger, easing: easing, fill: "backwards"});
  });
};

// Team numbers glide in from each side's outer edge, one after another.
const cascadeTeams = function (delay) {
  flourish("#leftTeams > div", [
    {opacity: 0, transform: "translateX(-24px)"},
    {opacity: 1, transform: "translateX(0px)"},
  ], 600, delay, 80);
  flourish("#rightTeams > div", [
    {opacity: 0, transform: "translateX(24px)"},
    {opacity: 1, transform: "translateX(0px)"},
  ], 600, delay, 80);
};

const popAvatars = function (delay) {
  flourish(".avatar", [
    {opacity: 0, transform: "scale(0.3)"},
    {opacity: 1, transform: "scale(1)"},
  ], 600, delay, 60, flourishPop);
};

// The center circle punches whenever the overlay changes layout.
const punchCircle = function () {
  flourish("#matchCircle", [
    {transform: "scale(0.6)"},
    {transform: "scale(1)"},
  ], 700, 0, 0, flourishPop);
};

// The auxiliary fuel/hub boxes rise up from behind the overlay.
const riseScoreAux = function (delay) {
  flourish("#leftScoreAux, #rightScoreAux", [
    {transform: "translateY(60px) scale(0.85)"},
    {transform: "translateY(0px) scale(1)"},
  ], 800, delay, 100);
};

// The score numbers and timer rise in, and the auxiliary boxes rise up behind them.
const revealScores = function () {
  flourish(".score-number", [
    {transform: "translateY(35%)"},
    {transform: "none"},
  ], 600, 0, 90);
  flourish("#matchTime", [{transform: "translateY(12px)"}, {transform: "none"}], 600, 150);
  riseScoreAux(100);
};

const revealTimeoutDetails = function (delay) {
  flourish("#timeoutBreakDescription", [
    {transform: "translateX(40px)"},
    {transform: "none"},
  ], 700, delay);
  flourish("#timeoutNextMatch", [
    {transform: "translateX(-40px)"},
    {transform: "none"},
  ], 700, delay + 80);
};

// Calls out climbs and foul points as they land during the match (see DisplayShared.createScorePopper). The first
// update for a match just sets the baseline.
const scorePopper = DisplayShared.createScorePopper(true);
let lastCalloutPoints = null;
const animateScoreChanges = function (data) {
  const previous = lastCalloutPoints;
  lastCalloutPoints = {
    red: DisplayShared.getCalloutPoints(data.Red.ScoreSummary),
    blue: DisplayShared.getCalloutPoints(data.Blue.ScoreSummary),
  };
  if (previous === null || currentScreen !== "match") {
    return;
  }
  [[redSide, "red"], [blueSide, "blue"]].forEach(function ([side, color]) {
    $.each(lastCalloutPoints[color], function (label, points) {
      scorePopper(side, points - previous[color][label], label);
    });
  });
};

const setIntroMode = function (enabled) {
  $("#matchOverlay").attr("data-mode", enabled ? "intro" : null);
};

// How long the audience display takes, from being switched to the final score, to get from the screen it was on to the
// start of its winner reveal. The wall starts the same reveal at the same moment, so the two play in step from there.
// Blank and match were measured; the rest are worked out from the audience display's transitions.
const audienceRevealStartMs = {
  blank: 2690,
  match: 4170,
  logo: 1000,
  logoLuma: 2300,
  bracket: 1000,
  standings: 1000,
  sponsor: 1950,
};
const audienceRevealStartDefaultMs = 4000;
// Null until the first screen arrives, which is just the current state on connecting rather than a switch to time.
let lastAudienceScreen = null;
let revealStartAt = 0;
let revealData = null;

// Handles a websocket message to change which screen is displayed.
const handleAudienceDisplayMode = function (targetScreen) {
  if (targetScreen === "score" && lastAudienceScreen !== null && lastAudienceScreen !== "score") {
    revealStartAt = Date.now() + (audienceRevealStartMs[lastAudienceScreen] ?? audienceRevealStartDefaultMs);
  } else if (targetScreen === "score") {
    // Connecting while the score is already up: just show it, without replaying a reveal that already happened.
    revealStartAt = null;
  }
  lastAudienceScreen = targetScreen;
  if (targetScreen === "logoLuma") {
    targetScreen = "logo";
  }
  if (
    targetScreen !== "intro" &&
    targetScreen !== "teamIntro" &&
    targetScreen !== "score" &&
    targetScreen !== "match" &&
    targetScreen !== "timeout" &&
    targetScreen !== "logo" &&
    targetScreen !== "standings"
  ) {
    targetScreen = "blank";
  }

  transitionQueue.push(targetScreen);
  executeTransitionQueue();
};

// Sequentially executes all transitions in the queue. Returns without doing anything if another invocation is already
// in progress.
const executeTransitionQueue = function () {
  if (transitionInProgress) {
    // There is an existing invocation of this method which will execute all transitions in the queue.
    return;
  }

  if (transitionQueue.length > 0) {
    transitionInProgress = true;
    const targetScreen = transitionQueue.shift();
    const callback = function () {
      // When the current transition is complete, call this method again to invoke the next one in the queue.
      currentScreen = targetScreen;
      transitionInProgress = false;
      setTimeout(executeTransitionQueue, 100);  // A small delay is needed to avoid visual glitches.
    };

    if (targetScreen === currentScreen) {
      callback();
      return;
    }

    let transitions = transitionMap[currentScreen][targetScreen];
    if (transitions !== undefined) {
      transitions(callback);
    } else {
      // There is no direct transition defined; need to go to the blank screen first.
      transitionMap[currentScreen]["blank"](function () {
        transitionMap["blank"][targetScreen](callback);
      });
    }
  }
};

// Handles a websocket message to update the teams for the current match.
const handleMatchLoad = function (data) {
  currentMatch = DisplayShared.handleMatchLoad(data, redSide, blueSide);
  lastCalloutPoints = null;
  MatchIntro.build(data, redSide, blueSide, DisplayShared.getAvatarUrl);
};

// Handles a websocket message to update the match time countdown.
const handleMatchTime = function (data) {
  DisplayShared.handleMatchTime(data);
};

// Handles a websocket message to update the match score.
const handleRealtimeScore = function (data) {
  DisplayShared.handle2026RealtimeScore(
    data,
    currentMatch,
    redSide,
    blueSide,
    hubActiveController.updateHubActiveIndicator
  );
  animateScoreChanges(data);
};

const transitionBlankToIntro = function (callback) {
  hideMessage(function () {
    $(".teams").css("display", "flex");
    $(".avatars").css("display", "flex");
    $(".avatars").css("opacity", 1);
    setIntroMode(true);
    punchCircle();
    cascadeTeams(150);
    popAvatars(300);
    $(".score").transition({queue: false, width: scoreMid}, 500, "ease", function () {
      $("#eventMatchInfo").css("display", "flex");
      $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoDown}, 500, "ease", callback);
    });
  });
};

const transitionBlankToLogo = function (callback) {
  showMessage(callback);
}

const transitionBlankToMatch = function (callback) {
  hideMessage(function () {
    $(".teams").css("display", "flex");
    punchCircle();
    cascadeTeams(150);
    $(".score-fields").css("display", "flex");
    $(".score-fields").transition({queue: false, width: scoreFieldsOut}, 500, "ease");
    $("#logo").transition({queue: false, top: logoUp}, 500, "ease");
    $(".score").transition({queue: false, width: scoreOut}, 500, "ease", function () {
      $("#eventMatchInfo").css("display", "flex");
      $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoDown}, 500, "ease", callback);
      $(".score-number").transition({queue: false, opacity: 1}, 750, "ease");
      $("#matchTime").transition({queue: false, opacity: 1}, 750, "ease");
      $(".score-fields").transition({queue: false, opacity: 1}, 750, "ease");
      $(".score-aux").transition({queue: false, opacity: 1}, 750, "ease");
      revealScores();
      hubActiveController.restartPendingHubActiveIndicators();
    });
  });
};

const transitionBlankToTimeout = function (callback) {
  hideMessage(function () {
    punchCircle();
    $("#timeoutDetails").transition({queue: false, width: timeoutDetailsOut}, 500, "ease");
    $("#logo").transition({queue: false, top: logoUp}, 500, "ease", function () {
      $(".timeout-detail").transition({queue: false, opacity: 1}, 750, "ease");
      revealTimeoutDetails(0);
      $("#matchTime").transition({queue: false, opacity: 1}, 750, "ease", callback);
    });
  });
};

const transitionIntroToBlank = function (callback) {
  setIntroMode(false);
  $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoUp}, 500, "ease", function () {
    $("#eventMatchInfo").hide();
    $(".score").transition({queue: false, width: scoreIn}, 500, "ease", function () {
      $(".avatars").css("opacity", 0);
      $(".avatars").hide();
      $(".teams").hide();
      showMessage(callback);
    });
  });
};

const transitionIntroToMatch = function (callback) {
  setIntroMode(false);
  $(".avatars").transition({queue: false, opacity: 0}, 500, "ease", function () {
    $(".avatars").hide();
  });
  $(".score-fields").css("display", "flex");
  $(".score-fields").transition({queue: false, width: scoreFieldsOut}, 500, "ease");
  $("#logo").transition({queue: false, top: logoUp}, 500, "ease");
  $(".score").transition({queue: false, width: scoreOut}, 500, "ease", function () {
    $(".score-number").transition({queue: false, opacity: 1}, 750, "ease");
    $("#matchTime").transition({queue: false, opacity: 1}, 750, "ease", callback);
    $(".score-fields").transition({queue: false, opacity: 1}, 750, "ease");
    $(".score-aux").transition({queue: false, opacity: 1}, 750, "ease");
    revealScores();
    hubActiveController.restartPendingHubActiveIndicators();
  });
};

const transitionIntroToTimeout = function (callback) {
  setIntroMode(false);
  $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoUp}, 500, "ease", function () {
    $("#eventMatchInfo").hide();
    $(".score").transition({queue: false, width: scoreIn}, 500, "ease", function () {
      $(".avatars").css("opacity", 0);
      $(".avatars").hide();
      $(".teams").hide();
      $("#timeoutDetails").transition({queue: false, width: timeoutDetailsOut}, 500, "ease");
      $("#logo").transition({queue: false, top: logoUp}, 500, "ease", function () {
        $(".timeout-detail").transition({queue: false, opacity: 1}, 750, "ease");
        revealTimeoutDetails(0);
        $("#matchTime").transition({queue: false, opacity: 1}, 750, "ease", callback);
      });
    });
  });
};

const transitionLogoToBlank = function (callback) {
  showMessage(callback);
}

const transitionMatchToBlank = function (callback) {
  $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoUp}, 500, "ease");
  $("#matchTime").transition({queue: false, opacity: 0}, 300, "linear");
  $(".score-fields").transition({queue: false, opacity: 0}, 300, "ease");
  $(".score-aux").transition({queue: false, opacity: 0}, 750, "ease");
  $(".score-number").transition({queue: false, opacity: 0}, 300, "linear", function () {
    $("#eventMatchInfo").hide();
    $(".score-fields").transition({queue: false, width: 0}, 500, "ease");
    $("#logo").transition({queue: false, top: logoDown}, 500, "ease");
    $(".score").transition({queue: false, width: scoreIn}, 500, "ease", function () {
      $(".teams").hide();
      $(".score-fields").hide();
      showMessage(callback);
    });
  });
};

const transitionMatchToIntro = function (callback) {
  $(".score-number").transition({queue: false, opacity: 0}, 300, "linear");
  $(".score-fields").transition({queue: false, opacity: 0}, 300, "ease");
  $(".score-aux").transition({queue: false, opacity: 0}, 750, "ease");
  $("#matchTime").transition({queue: false, opacity: 0}, 300, "linear", function () {
    $(".score-fields").transition({queue: false, width: 0}, 500, "ease");
    $("#logo").transition({queue: false, top: logoDown}, 500, "ease");
    $(".score").transition({queue: false, width: scoreMid}, 500, "ease", function () {
      $(".score-fields").hide();
      $(".avatars").css("display", "flex");
      setIntroMode(true);
      popAvatars(0);
      $(".avatars").transition({queue: false, opacity: 1}, 500, "ease", callback);
    });
  });
};

const transitionTimeoutToBlank = function (callback) {
  $(".timeout-detail").transition({queue: false, opacity: 0}, 300, "linear");
  $("#matchTime").transition({queue: false, opacity: 0}, 300, "linear", function () {
    $("#timeoutDetails").transition({queue: false, width: timeoutDetailsIn}, 500, "ease");
    $("#logo").transition({queue: false, top: logoDown}, 500, "ease", function () {
      showMessage(callback);
    });
  });
};

const transitionTimeoutToIntro = function (callback) {
  $(".timeout-detail").transition({queue: false, opacity: 0}, 300, "linear");
  $("#matchTime").transition({queue: false, opacity: 0}, 300, "linear", function () {
    $("#timeoutDetails").transition({queue: false, width: timeoutDetailsIn}, 500, "ease");
    $("#logo").transition({queue: false, top: logoDown}, 500, "ease", function () {
      $(".avatars").css("display", "flex");
      $(".avatars").css("opacity", 1);
      $(".teams").css("display", "flex");
      setIntroMode(true);
      cascadeTeams(150);
      popAvatars(300);
      $(".score").transition({queue: false, width: scoreMid}, 500, "ease", function () {
        $("#eventMatchInfo").show();
        $("#eventMatchInfo").transition({queue: false, height: eventMatchInfoDown}, 500, "ease", callback);
      });
    });
  });
};

// Handles a websocket message to populate the final score.
const handleScorePosted = function (data) {
  MatchIntro.buildResult(data, redSide, blueSide, DisplayShared.getAvatarUrl);
  revealData = WinnerReveal.buildData(data, redSide);
  if (currentScreen === "standings") {
    standingsTable.refresh();
  }
};

// Plays the same winner reveal as the audience display, started at the same moment so the two run in step (muted, as
// the audience display carries the sound), then the final score comes up on the team intro's stage as the reveal
// wipes away. Skipped if the operator has already moved on before the reveal is due to start.
const transitionBlankToScore = function (callback) {
  hideMessage(async function () {
    while (revealStartAt !== null && Date.now() < revealStartAt && transitionQueue.length === 0) {
      await new Promise(function (resolve) {
        setTimeout(resolve, 50);
      });
    }
    if (transitionQueue.length > 0) {
      callback();
      return;
    }
    if (revealStartAt !== null && revealData !== null) {
      await WinnerReveal.play(revealData, redSide, {muted: true});
    }
    MatchIntro.play({}, "result").then(callback);
  });
};

const transitionScoreToBlank = function (callback) {
  MatchIntro.leave(true).then(function () {
    showMessage(callback);
  });
};

// The full-screen team intro holds until the operator moves on; it then splits apart as the next screen comes in.
const transitionBlankToTeamIntro = function (callback) {
  hideMessage(function () {
    MatchIntro.play().then(callback);
  });
};

const transitionTeamIntroToBlank = function (callback) {
  MatchIntro.leave(true).then(function () {
    showMessage(callback);
  });
};

const transitionTeamIntroToIntro = function (callback) {
  MatchIntro.leave(true);
  transitionBlankToIntro(callback);
};

const transitionTeamIntroToMatch = function (callback) {
  MatchIntro.leave(true);
  transitionBlankToMatch(callback);
};

// The qualification standings, built once the page has loaded, and a function to size them to the wall.
let standingsTable = null;
let fitStandings = function () {};

// The standings drop in where the bar would be and roll through their pages until the operator moves on.
const transitionBlankToStandings = function (callback) {
  hideMessage(async function () {
    await standingsTable.load();
    standingsTable.render(0, 300);
    standingsTable.startCycle();
    $("#standings").show();
    fitStandings();
    document.getElementById("standings").animate([
      {opacity: 0, translate: "0 -24px"},
      {opacity: 1, translate: "0 0"},
    ], {duration: 700, easing: flourishEase, fill: "forwards"}).finished.then(callback, callback);
  });
};

const transitionStandingsToBlank = function (callback) {
  standingsTable.stopCycle();
  document.getElementById("standings").animate([{opacity: 1}, {opacity: 0}], {duration: 400, fill: "forwards"})
    .finished.then(function () {
      $("#standings").hide();
      showMessage(callback);
    }, callback);
};

const showMessage = function (callback) {
  if (!hasMessage) {
    if (callback) {
      callback();
    }
    return;
  }
  $("#message").show();
  flourish("#message", [
    {transform: "translateY(24px)"},
    {transform: "none"},
  ], 900);
  $("#message").transition({queue: false, opacity: 1}, 750, "ease", callback);
};

const hideMessage = function (callback) {
  if (!hasMessage) {
    if (callback) {
      callback();
    }
    return;
  }
  $("#message").transition({queue: false, opacity: 0}, 750, "ease", function () {
    $("#message").hide();
    if (callback) {
      callback();
    }
  });
};

$(function () {
  // Read the configuration for this display from the URL query string.
  const urlParams = new URLSearchParams(window.location.search);
  document.body.style.backgroundColor = urlParams.get("background");
  const sides = DisplayShared.applyDisplaySides(urlParams);
  redSide = sides.redSide;
  blueSide = sides.blueSide;

  // Adjust position and size of display contents.
  const overlayCentering = $("#overlayCentering");
  overlayCentering.css("top", parseInt(urlParams.get("topSpacingPx")) + overlayTopOffset + "px");
  overlayCentering.css("transform", `scale(${urlParams.get("zoomFactor")})`);
  // The standings hang just below the logo circle, as they do under the disc on the audience display, and are scaled to
  // fill whatever room is left on the wall, whatever its shape.
  const standingsTop = parseInt(urlParams.get("topSpacingPx")) + overlayTopOffset +
    150 * parseFloat(urlParams.get("zoomFactor")) + 20;
  fitStandings = function () {
    const standings = document.getElementById("standings");
    const scale = Math.min(
      (window.innerWidth - 40) / standings.offsetWidth,
      (window.innerHeight - standingsTop - 20) / standings.offsetHeight,
    );
    $(standings).css({top: standingsTop + "px", transform: `translateX(-50%) scale(${scale})`});
  };
  window.addEventListener("resize", fitStandings);
  standingsTable = StandingsTable.create(document.getElementById("standingsPanel"));

  messageText = urlParams.get("message") || "";
  hasMessage = messageText !== "";
  const messageDiv = $("#message");
  messageDiv.text(messageText);
  messageDiv.toggle(hasMessage);
  if (hasMessage) {
    showMessage();
  }

  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/displays/wall/websocket", {
    allianceSelection: function (event) {
      handleAllianceSelection(event.data);
    },
    audienceDisplayMode: function (event) {
      handleAudienceDisplayMode(event.data);
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
    realtimeScore: function (event) {
      handleRealtimeScore(event.data);
    },
    scorePosted: function (event) {
      handleScorePosted(event.data);
    },
  });

  // Map how to transition from one screen to another. Missing links between screens indicate that first we
  // must transition to the blank screen and then to the target screen.
  transitionMap = {
    blank: {
      intro: transitionBlankToIntro,
      logo: transitionBlankToLogo,
      match: transitionBlankToMatch,
      score: transitionBlankToScore,
      standings: transitionBlankToStandings,
      teamIntro: transitionBlankToTeamIntro,
      timeout: transitionBlankToTimeout,
    },
    intro: {
      blank: transitionIntroToBlank,
      match: transitionIntroToMatch,
      timeout: transitionIntroToTimeout,
    },
    logo: {
      blank: transitionLogoToBlank,
    },
    match: {
      blank: transitionMatchToBlank,
      intro: transitionMatchToIntro,
    },
    score: {
      blank: transitionScoreToBlank,
    },
    standings: {
      blank: transitionStandingsToBlank,
    },
    teamIntro: {
      blank: transitionTeamIntroToBlank,
      intro: transitionTeamIntroToIntro,
      match: transitionTeamIntroToMatch,
    },
    timeout: {
      blank: transitionTimeoutToBlank,
      intro: transitionTimeoutToIntro,
    },
  }
});
