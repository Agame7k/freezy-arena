// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
// Author: nick@team254.com (Nick Eyre)
//
// Client-side methods for the audience display.

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
let overlayCenteringHideParams;
let overlayCenteringShowParams;
let lowerThirdVisible = false;
// Everything the winner reveal needs about the most recently posted result; null until a score has been posted.
let revealData = null;
// Live-match state for the scoring effects: the last callout points seen for each alliance (null until the first update
// for this match), which alliance last held the lead, the period shown on the phase tab, and the last second the timer
// ticked in the final countdown.
let scorePopper;
let lastScores = null;
let leadingAlliance = "";
let currentPhase = "";
let lastUrgentSecond = null;
const hubActiveController = DisplayShared.createHubActiveController(function () {
  return currentScreen;
});
const allianceSelectionTemplate = Handlebars.compile($("#allianceSelectionTemplate").html());
const sponsorImageTemplate = Handlebars.compile($("#sponsorImageTemplate").html());
const sponsorTextTemplate = Handlebars.compile($("#sponsorTextTemplate").html());

// Constants for overlay positioning. The CSS is the source of truth for the values that represent initial state.
const overlayCenteringTopUp = "-240px";
const overlayCenteringBottomHideParams = {bottom: $("#overlayCentering").css("bottom")};
const overlayCenteringBottomShowParams = {bottom: "0px"};
const overlayCenteringTopHideParams = {top: overlayCenteringTopUp};
const overlayCenteringTopShowParams = {top: "50px"};
const eventMatchInfoDown = "36px";
const eventMatchInfoUp = "0px";
const logoUp = "35px";
const logoDown = $("#logo").css("top");
const scoreIn = $(".score").css("width");
const scoreMid = "185px";
// Wide enough for the score fields (180px) and the score number (170px, room for three digits) plus the 90px the
// match circle covers at the inner end, so no digit ever ends up hidden behind it.
const scoreOut = "440px";
const scoreFieldsOut = "180px";
const timeoutDetailsIn = $("#timeoutDetails").css("width");
const timeoutDetailsOut = "680px";

// Resting places for the full-screen logo disc: centered on its own, perched on top of the final score card, or
// shrunk above the bracket. Offsets are relative to the vertical center of the screen.
const discCenterY = 0;
const discScoreY = -265;
const discBracketY = -390;
const discBracketScale = 0.75;
const discRadius = "155px";

// Motion language shared by every transition. Things arrive fast and settle softly, leave by accelerating away, and
// move in place along a symmetric curve; "pop" adds a touch of overshoot for small elements landing and "stampIn"
// accelerates into a hit, for things that slam down. Exits are kept noticeably shorter than entrances so the display
// never feels like it is waiting on itself.
const ease = {
  in: "cubic-bezier(0.16, 1, 0.3, 1)",
  out: "cubic-bezier(0.7, 0, 0.84, 0)",
  move: "cubic-bezier(0.65, 0, 0.35, 1)",
  pop: "cubic-bezier(0.34, 1.56, 0.64, 1)",
  stampIn: "cubic-bezier(0.55, 0, 1, 0.45)",
};

// Reusable keyframes.
const riseIn = [
  {opacity: 0, transform: "translateY(35%)"},
  {opacity: 1, transform: "translateY(0px)"},
];
const fadeOut = [{opacity: 0, transform: "translateY(-10%)"}];
const popIn = [{opacity: 0, transform: "scale(0.4)"}, {opacity: 1, transform: "scale(1)"}];
const popOut = [{opacity: 0, transform: "scale(0.6)"}];
const slideIn = function (fromX) {
  return [
    {opacity: 0, transform: `translateX(${fromX}px)`},
    {opacity: 1, transform: "translateX(0px)"},
  ];
};
// Drops onto the screen from larger than life, overshoots a touch small on impact and settles.
const stamp = function (fromScale) {
  return [
    {opacity: 0, transform: `scale(${fromScale})`, easing: ease.stampIn},
    {opacity: 1, transform: "scale(0.95)", offset: 0.6, easing: ease.in},
    {opacity: 1, transform: "scale(1)"},
  ];
};

// Transforms that leave an element looking untouched are stored as "none" once an animation settles, so the element
// doesn't stay promoted to its own compositing layer (which can leave text rendering slightly soft).
const restingValue = function (property, value) {
  if (property === "transform" && /^((translate[XY]?\(0(px)?\)|scale\(1\))\s*)+$/.test(value)) {
    return "none";
  }
  return value;
};

const wait = function (ms) {
  return new Promise(function (resolve) {
    setTimeout(resolve, ms);
  });
};

// Runs the given keyframes on every element matched by the target, optionally staggering the start of each one, and
// leaves each element resting on the final keyframe once done. Resolves when every element has finished; an animation
// that gets cancelled part way through resolves too, so a transition chain can never get stuck waiting on it.
const animate = function (target, keyframes, duration, easing, delay = 0, stagger = 0) {
  const finalFrame = {...keyframes[keyframes.length - 1]};
  delete finalFrame.offset;
  delete finalFrame.easing;
  Object.keys(finalFrame).forEach(function (property) {
    finalFrame[property] = restingValue(property, finalFrame[property]);
  });
  return Promise.all($(target).toArray().map(function (element, i) {
    const totalDelay = delay + i * stagger;
    const animation = element.animate(keyframes, {duration, easing, delay: totalDelay, fill: "both"});
    // Animations only advance while the browser is painting frames, which stops if the display window is minimized or
    // covered. Force completion on a wall-clock timer as a backstop so the transition queue always keeps moving.
    const backstopId = setTimeout(function () {
      if (animation.playState === "running" || animation.pending) {
        animation.finish();
      }
    }, totalDelay + duration + 250);
    return animation.finished.then(function () {
      clearTimeout(backstopId);
      $(element).css(finalFrame);
      animation.cancel();
    }, function () {
      clearTimeout(backstopId);
    });
  }));
};

// Cancels anything currently animating on the target, leaving it at whatever inline style it last rested on.
const stopAnimations = function (target) {
  $(target).each(function () {
    this.getAnimations().forEach(function (animation) {
      animation.cancel();
    });
  });
};

// How much of a point lead saturates the mini momentum glow on the center logo circle.
const MINI_MOMENTUM_SATURATION_POINTS = 40;

// Updates the --lean/--intensity custom properties driving the center circle's momentum glow. lean ranges from -1
// (blue leading) to 1 (red leading); intensity is how far the lead is into saturation, from 0 to 1.
const updateMiniMomentum = function (redScore, blueScore) {
  const diff = redScore - blueScore;
  const lean = Math.max(-1, Math.min(1, diff / MINI_MOMENTUM_SATURATION_POINTS));
  const intensity = Math.max(0, Math.min(1, Math.abs(diff) / MINI_MOMENTUM_SATURATION_POINTS));
  const matchCircle = document.getElementById("matchCircle");
  if (matchCircle) {
    matchCircle.style.setProperty("--lean", lean);
    matchCircle.style.setProperty("--intensity", intensity);
  }
};

// Rolls a ring of the given color off the match circle.
const pulseRing = function (color) {
  const ring = document.getElementById("leadPulse");
  ring.style.setProperty("--pulse-color", color);
  ring.animate([
    {opacity: 0.95, transform: "scale(1)", borderWidth: "8px"},
    {opacity: 0, transform: "scale(1.9)", borderWidth: "2px"},
  ], {duration: 900, easing: ease.in});
};

// The shortest gap between two lead-change rings, so a close match trading the lead back and forth on fuel doesn't
// strobe the match circle.
const leadPulseCooldownMs = 5000;
let lastLeadPulseTime = 0;

// Calls out climbs and foul points as they land during the match, and rolls a ring in an alliance's color off the
// match circle when it takes the lead. Only runs while the match bar is up; the first update for a match just sets the
// baseline.
const animateScoreChanges = function (data, redScore, blueScore) {
  const previous = lastScores;
  lastScores = {
    red: DisplayShared.getCalloutPoints(data.Red.ScoreSummary),
    blue: DisplayShared.getCalloutPoints(data.Blue.ScoreSummary),
  };
  if (previous === null || currentScreen !== "match") {
    return;
  }
  [[redSide, "red"], [blueSide, "blue"]].forEach(function ([side, color]) {
    $.each(lastScores[color], function (label, points) {
      scorePopper(side, points - previous[color][label], label);
    });
  });

  const leader = redScore > blueScore ? "red" : blueScore > redScore ? "blue" : "";
  if (leader !== "" && leader !== leadingAlliance && Date.now() - lastLeadPulseTime > leadPulseCooldownMs) {
    lastLeadPulseTime = Date.now();
    pulseRing(leader === "red" ? "#ff5571" : "#4da3ff");
  }
  if (leader !== "") {
    leadingAlliance = leader;
  }
};

// Works out the period of the match to show on the phase tab: auto, then teleop's transition shift, the four shifts
// and the endgame. Returns an empty string outside of a running match.
const getPhaseLabel = function (matchState, countdownSec) {
  switch (matchState) {
    case "START_MATCH":
    case "AUTO_PERIOD":
    case "PAUSE_PERIOD":
      return "Auto";
    case "TELEOP_PERIOD": {
      if (countdownSec <= matchTiming.EndgameDurationSec) {
        return "Endgame";
      }
      const teleopElapsedSec = getTeleopDurationSec() - countdownSec;
      if (teleopElapsedSec < matchTiming.TransitionShiftDurationSec) {
        return "Transition";
      }
      const shift = 1 + Math.floor((teleopElapsedSec - matchTiming.TransitionShiftDurationSec) /
        matchTiming.ShiftDurationSec);
      return `Shift ${Math.min(shift, 4)}`;
    }
    default:
      return "";
  }
};

// Updates the phase tab, swapping the new period in from underneath the old one. The endgame also rolls a gold ring off
// the match circle.
const updatePhase = function (label) {
  if (label === currentPhase) {
    return;
  }
  const previousPhase = currentPhase;
  currentPhase = label;
  const tab = $("#phaseTab");
  tab.attr("data-phase", label.toLowerCase());
  $("#phaseTabText").text(label);
  if (!phaseTabShown) {
    return;
  }
  animate(tab, [{opacity: label === "" ? 0 : 1}], 250, ease.move);
  if (label !== "" && previousPhase !== "") {
    animate("#phaseTabText", [
      {opacity: 0, transform: "translateY(100%)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 450, ease.in);
  }
  if (label === "Endgame") {
    pulseRing("#ffd166");
  }
};

// Turns the timer red for the last ten seconds of the match, punching it on every tick.
const updateTimerUrgency = function (matchState, countdownSec) {
  const urgent = matchState === "TELEOP_PERIOD" && countdownSec <= 10;
  $("#matchTime").attr("data-urgent", urgent);
  if (urgent && countdownSec !== lastUrgentSecond && currentScreen === "match") {
    document.getElementById("matchTime").animate([
      {transform: "scale(1.35)"},
      {transform: "scale(1)"},
    ], {duration: 450, easing: ease.in});
  }
  lastUrgentSecond = urgent ? countdownSec : null;
};

// Handles a websocket message to change which screen is displayed.
const handleAudienceDisplayMode = function (targetScreen) {
  transitionQueue.push(targetScreen);
  executeTransitionQueue();
};

// Sequentially executes all transitions in the queue. Returns without doing anything if another invocation is already
// in progress, since that invocation will drain the whole queue.
const executeTransitionQueue = async function () {
  if (transitionInProgress) {
    return;
  }
  transitionInProgress = true;

  while (transitionQueue.length > 0) {
    const targetScreen = transitionQueue.shift();
    if (targetScreen === currentScreen) {
      continue;
    }

    if (targetScreen === "sponsor") {
      initializeSponsorDisplay();
    }
    // Lets the stylesheet move things out of the way of the screen that's coming up, such as the lower third clearing
    // the match bar.
    $("body").attr("data-screen", targetScreen);
    if (currentScreen === "intro") {
      hideIntroExtras();
    }

    try {
      const transition = transitionMap[currentScreen][targetScreen];
      if (transition !== undefined) {
        await transition();
      } else {
        // There is no direct transition defined; need to go to the blank screen first.
        await transitionMap[currentScreen]["blank"]();
        currentScreen = "blank";
        await transitionMap["blank"][targetScreen]();
      }
    } catch (error) {
      console.error(`Transition from ${currentScreen} to ${targetScreen} failed:`, error);
    }
    currentScreen = targetScreen;
    if (currentScreen === "intro") {
      showIntroExtras();
    }
    await wait(100);  // A small delay is needed to avoid visual glitches.
  }

  transitionInProgress = false;
};

// Handles a websocket message to update the teams for the current match.
const handleMatchLoad = function (data) {
  currentMatch = DisplayShared.handleMatchLoad(data, redSide, blueSide);
  $("#matchCircle").removeAttr("data-aborted");
  updateMiniMomentum(0, 0);
  lastScores = null;
  leadingAlliance = "";
  MatchIntro.build(data, redSide, blueSide, getAvatarUrl);
  buildIntroExtras(data);
  if (currentScreen === "intro") {
    hideIntroExtras().then(function () {
      showIntroExtras();
    });
  }
};

// Fills in what goes above the intro bar for the loaded match: each team's rank and record before a qualification match,
// or where the series stands before a playoff match. Nothing is shown when there's nothing to say.
let introExtras = null;
const buildIntroExtras = function (data) {
  introExtras = null;
  const match = data.Match;
  if (match.Type === matchTypeQualification) {
    const rankings = data.Rankings || {};
    const records = data.Records || {};
    const fill = function (element, teamIds) {
      element.empty();
      teamIds.forEach(function (teamId) {
        if (!teamId) {
          return;
        }
        const rank = rankings[teamId];
        $("<div class='intro-standings-row'>").append(
          $("<span>").text(teamId),
          $("<span class='intro-standings-rank'>").text(rank ? `#${rank}` : ""),
          $("<span class='intro-standings-record'>").text(records[teamId] || ""),
        ).appendTo(element);
      });
    };
    const redTeams = [match.Red1, match.Red2, match.Red3];
    const blueTeams = [match.Blue1, match.Blue2, match.Blue3];
    fill($("#leftIntroStandings"), redSide === "left" ? redTeams : blueTeams);
    fill($("#rightIntroStandings"), redSide === "left" ? blueTeams : redTeams);
    const anyRanked = redTeams.concat(blueTeams).some(function (teamId) {
      return rankings[teamId];
    });
    if (anyRanked) {
      introExtras = "#leftIntroStandings, #rightIntroStandings";
    }
  } else if (match.Type === matchTypePlayoff && (data.SeriesStatus || data.SeriesStakes)) {
    $("#introSeriesStatus").text(data.SeriesStatus || "");
    $("#introSeriesStakes").text(data.SeriesStakes || "");
    introExtras = "#introSeries";
  }
};

const showIntroExtras = function (delay = 0) {
  if (introExtras === null) {
    return;
  }
  const fromY = overlayCenteringShowParams === overlayCenteringTopShowParams ? "-16px" : "16px";
  $(introExtras).show().each(function (i) {
    this.animate([
      {opacity: 0, translate: `0 ${fromY}`},
      {opacity: 1, translate: "0 0"},
    ], {duration: 600, delay: delay + i * 120, easing: ease.in, fill: "forwards"});
  });
};

const hideIntroExtras = function () {
  const shown = $(".intro-standings, #introSeries").filter(":visible");
  return Promise.all(shown.toArray().map(function (element) {
    return element.animate([{opacity: 0}], {duration: 250, easing: ease.out, fill: "forwards"}).finished
      .catch(function () {});
  })).then(function () {
    shown.hide();
  });
};

// Handles a websocket message to update the match time countdown.
const handleMatchTime = function (data) {
  const aborted = data.MatchAborted === true;
  DisplayShared.handleMatchTime(data);
  translateMatchTime(data, function (matchState, matchStateText, countdownSec) {
    updatePhase(aborted ? "Aborted" : getPhaseLabel(matchState, countdownSec));
    updateTimerUrgency(matchState, countdownSec);
  });
  if (aborted) {
    $("#matchTime").text("ABORT");
  }
  updateAbort(aborted);
};

// ---------------------------------------------------------------------------------------------------------------------
// Match abort.
// ---------------------------------------------------------------------------------------------------------------------

// Whether the current match has been aborted, as of the last match time message; null until the first one arrives.
let matchAborted = null;
// Resolves once the match bar may be taken down after an abort; null when no abort is playing out.
let abortBarHold = null;
// How long the match bar waits before going down, in case an abort notice is racing the screen change.
const abortGraceMs = 150;

// Throws up the abort takeover when the match is aborted, which holds until the match is reset, and slams the match bar
// to a stop underneath it.
const updateAbort = function (aborted) {
  const matchName = currentMatch ? currentMatch.LongName : "";
  if (AbortTakeover.update(aborted, "audience", matchName)) {
    abortBarHold = wait(AbortTakeover.introDurationMs - 300);
    if (currentScreen === "match") {
      slamMatchBar();
    }
  }
  if (!aborted) {
    abortBarHold = null;
    $("#matchCircle").removeAttr("data-aborted");
  }
  if (matchAborted !== null && aborted !== matchAborted) {
    // Flag or unflag the aborted match on the bracket.
    $("#bracketSvg").attr("src", "/api/bracket/svg?activeMatch=saved&v=" + new Date().getTime());
  }
  matchAborted = aborted;
};

// Turns the match circle red, punches it and rattles the whole bar, rolling red rings off the circle.
const slamMatchBar = function () {
  $("#matchCircle").attr("data-aborted", true);
  document.getElementById("matchCircle").animate([
    {transform: "scale(1.5) rotate(-12deg)"},
    {transform: "scale(0.9) rotate(4deg)", offset: 0.45},
    {transform: "scale(1.06) rotate(-2deg)", offset: 0.7},
    {transform: "none"},
  ], {duration: 700, easing: ease.in});
  document.getElementById("matchOverlay").animate([
    {transform: "none"},
    {transform: "translate(-24px, 6px) rotate(-0.8deg)"},
    {transform: "translate(20px, -5px) rotate(0.7deg)"},
    {transform: "translate(-15px, 4px) rotate(-0.5deg)"},
    {transform: "translate(11px, -3px) rotate(0.3deg)"},
    {transform: "translate(-6px, 2px)"},
    {transform: "translate(3px, 0px)"},
    {transform: "none"},
  ], {duration: 800, easing: "ease-out"});
  [0, 300, 600].forEach(function (delay) {
    setTimeout(function () {
      pulseRing("#ff5571");
    }, delay);
  });
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
  const redScore = data.Red.ScoreSummary.Score - data.Red.ScoreSummary.PostMatchPoints;
  const blueScore = data.Blue.ScoreSummary.Score - data.Blue.ScoreSummary.PostMatchPoints;
  updateMiniMomentum(redScore, blueScore);
  animateScoreChanges(data, redScore, blueScore);
};

const setFinalResultIndicator = function (side, label, result) {
  const indicator = $(`#${side}FinalResultIndicator`);
  indicator.text(label);
  indicator.attr("data-result", result);
  // Also mark the winning side's own score number, so the reveal moment gives it a bit more visual weight than a
  // flat, identical-looking pair of numbers regardless of who actually won.
  $(`#${side}FinalScore`).attr("data-result", result);
};

const cardLabels = WinnerReveal.cardLabels;
const mostSeriousCard = WinnerReveal.mostSeriousCard;
const revealSound = WinnerReveal.sound;

// Fills in an alliance's name on the final score, with its card as a pill beside it (playoffs only).
const setFinalAlliance = function (side, allianceId, card) {
  const alliance = $(`#${side}FinalAlliance`).empty();
  $("<span>").text("Alliance " + allianceId).appendTo(alliance);
  if (card !== "") {
    $("<span class='final-alliance-card'>").attr("data-card", card).text(cardLabels[card]).appendTo(alliance);
  }
};

// Strikes through the teams that got a red card or were disqualified. In playoffs the card belongs to the whole
// alliance, so every member is struck, including the one who sat the match out; otherwise it's just the carded teams.
const strikeRedCardTeams = function (side, teamIds, cards, isPlayoff, allianceCard) {
  const isRedCard = function (card) {
    return card === "red" || card === "dq";
  };
  for (let position = 1; position <= 4; position++) {
    const struck = isPlayoff ? isRedCard(allianceCard) : position <= 3 && isRedCard(cards[String(teamIds[position - 1])]);
    $(`#${side}FinalTeam${position}`).closest(".final-team-row").attr("data-red-card", struck);
  }
};

// Handles a websocket message to populate the final score data.
const handleScorePosted = function (data) {
  const isPlayoff = data.Match.Type === matchTypePlayoff;
  const redTeamIds = [data.Match.Red1, data.Match.Red2, data.Match.Red3];
  const blueTeamIds = [data.Match.Blue1, data.Match.Blue2, data.Match.Blue3];
  const redAllianceCard = mostSeriousCard(data.RedCards, redTeamIds);
  const blueAllianceCard = mostSeriousCard(data.BlueCards, blueTeamIds);
  // In playoffs the alliance's card is shown once beside its name, so the per-team cards are left blank.
  const redTeamCards = isPlayoff ? {} : data.RedCards;
  const blueTeamCards = isPlayoff ? {} : data.BlueCards;

  if (data.RedWon) {
    setFinalResultIndicator(redSide, "WINNER", "winner");
    setFinalResultIndicator(blueSide, "", "");
  } else if (data.BlueWon) {
    setFinalResultIndicator(redSide, "", "");
    setFinalResultIndicator(blueSide, "WINNER", "winner");
  } else {
    setFinalResultIndicator(redSide, "TIE", "tie");
    setFinalResultIndicator(blueSide, "TIE", "tie");
  }
  const tiebreakReason = data.TiebreakReason || "";
  $("#finalTiebreakReason").text(tiebreakReason);
  $("#finalTiebreakReason").attr("data-visible", tiebreakReason !== "");

  $(`#${redSide}FinalScore`).text(data.RedScoreSummary.Score);
  setFinalAlliance(redSide, data.Match.PlayoffRedAlliance, redAllianceCard);
  setTeamInfo(redSide, 1, data.Match.Red1, redTeamCards, data.RedRankings);
  setTeamInfo(redSide, 2, data.Match.Red2, redTeamCards, data.RedRankings);
  setTeamInfo(redSide, 3, data.Match.Red3, redTeamCards, data.RedRankings);
  if (data.RedOffFieldTeamIds.length > 0) {
    setTeamInfo(redSide, 4, data.RedOffFieldTeamIds[0], redTeamCards, data.RedRankings);
  } else {
    setTeamInfo(redSide, 4, 0, redTeamCards, data.RedRankings);
  }
  strikeRedCardTeams(redSide, redTeamIds, data.RedCards, isPlayoff, redAllianceCard);
  $(`#${redSide}FinalAutoFuelPoints`).text(data.RedScoreSummary.AutoFuelPoints);
  $(`#${redSide}FinalAutoTowerPoints`).text(data.RedScoreSummary.AutoTowerPoints);
  $(`#${redSide}FinalTeleopFuelPoints`).text(data.RedScoreSummary.TeleopFuelPoints);
  $(`#${redSide}FinalTeleopTowerPoints`).text(data.RedScoreSummary.TeleopTowerPoints);
  $(`#${redSide}FinalFoulPoints`).text(data.RedScoreSummary.FoulPoints);
  $(`#${redSide}FinalEnergizedBonusRankingPoint`).html(
    data.RedScoreSummary.EnergizedBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${redSide}FinalEnergizedBonusRankingPoint`).attr(
    "data-checked", data.RedScoreSummary.EnergizedBonusRankingPoint
  );
  $(`#${redSide}FinalSuperchargedBonusRankingPoint`).html(
    data.RedScoreSummary.SuperchargedBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${redSide}FinalSuperchargedBonusRankingPoint`).attr(
    "data-checked", data.RedScoreSummary.SuperchargedBonusRankingPoint
  );
  $(`#${redSide}FinalTraversalBonusRankingPoint`).html(
    data.RedScoreSummary.TraversalBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${redSide}FinalTraversalBonusRankingPoint`).attr(
    "data-checked", data.RedScoreSummary.TraversalBonusRankingPoint
  );
  $(`#${redSide}FinalRankingPoints`).html(data.RedRankingPoints);
  $(`#${redSide}FinalWins`).text(data.RedWins);
  const redFinalDestination = $(`#${redSide}FinalDestination`);
  redFinalDestination.html(data.RedDestination.replace("Advances to ", "Advances to<br>"));
  redFinalDestination.toggle(data.RedDestination !== "");
  redFinalDestination.attr("data-won", data.RedWon);

  $(`#${blueSide}FinalScore`).text(data.BlueScoreSummary.Score);
  setFinalAlliance(blueSide, data.Match.PlayoffBlueAlliance, blueAllianceCard);
  setTeamInfo(blueSide, 1, data.Match.Blue1, blueTeamCards, data.BlueRankings);
  setTeamInfo(blueSide, 2, data.Match.Blue2, blueTeamCards, data.BlueRankings);
  setTeamInfo(blueSide, 3, data.Match.Blue3, blueTeamCards, data.BlueRankings);
  if (data.BlueOffFieldTeamIds.length > 0) {
    setTeamInfo(blueSide, 4, data.BlueOffFieldTeamIds[0], blueTeamCards, data.BlueRankings);
  } else {
    setTeamInfo(blueSide, 4, 0, blueTeamCards, data.BlueRankings);
  }
  strikeRedCardTeams(blueSide, blueTeamIds, data.BlueCards, isPlayoff, blueAllianceCard);
  $(`#${blueSide}FinalAutoFuelPoints`).text(data.BlueScoreSummary.AutoFuelPoints);
  $(`#${blueSide}FinalAutoTowerPoints`).text(data.BlueScoreSummary.AutoTowerPoints);
  $(`#${blueSide}FinalTeleopFuelPoints`).text(data.BlueScoreSummary.TeleopFuelPoints);
  $(`#${blueSide}FinalTeleopTowerPoints`).text(data.BlueScoreSummary.TeleopTowerPoints);
  $(`#${blueSide}FinalFoulPoints`).text(data.BlueScoreSummary.FoulPoints);
  $(`#${blueSide}FinalEnergizedBonusRankingPoint`).html(
    data.BlueScoreSummary.EnergizedBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${blueSide}FinalEnergizedBonusRankingPoint`).attr(
    "data-checked", data.BlueScoreSummary.EnergizedBonusRankingPoint
  );
  $(`#${blueSide}FinalSuperchargedBonusRankingPoint`).html(
    data.BlueScoreSummary.SuperchargedBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${blueSide}FinalSuperchargedBonusRankingPoint`).attr(
    "data-checked", data.BlueScoreSummary.SuperchargedBonusRankingPoint
  );
  $(`#${blueSide}FinalTraversalBonusRankingPoint`).html(
    data.BlueScoreSummary.TraversalBonusRankingPoint ? "&#x2714;" : "&#x2718;"
  );
  $(`#${blueSide}FinalTraversalBonusRankingPoint`).attr(
    "data-checked", data.BlueScoreSummary.TraversalBonusRankingPoint
  );
  $(`#${blueSide}FinalRankingPoints`).html(data.BlueRankingPoints);
  $(`#${blueSide}FinalWins`).text(data.BlueWins);
  const blueFinalDestination = $(`#${blueSide}FinalDestination`);
  blueFinalDestination.html(data.BlueDestination.replace("Advances to ", "Advances to<br>"));
  blueFinalDestination.toggle(data.BlueDestination !== "");
  blueFinalDestination.attr("data-won", data.BlueWon);

  let matchName = data.Match.LongName;
  if (data.Match.NameDetail !== "") {
    matchName += " &ndash; " + data.Match.NameDetail;
  }
  $("#finalMatchName").html(matchName);

  // Reload the bracket to reflect any changes.
  $("#bracketSvg").attr("src", "/api/bracket/svg?activeMatch=saved&v=" + new Date().getTime());

  // Bring the standings up to date if they're on screen.
  if (currentScreen === "standings") {
    standingsTable.refresh();
  }

  if (data.Match.Type === matchTypePlayoff) {
    // Hide bonus ranking points and show playoff-only fields.
    $(".playoff-hidden-field").hide();
    $(".playoff-only-field").show();
  } else {
    $(".playoff-hidden-field").show();
    $(".playoff-only-field").hide();
  }
  $(".coopertition-hidden-field").toggle(data.CoopertitionEnabled);
  // Only qualification results count towards the rankings; practice matches don't earn ranking points that matter.
  $(".qualification-only-field").toggle(data.Match.Type === matchTypeQualification);

  // Wake the audio engine now, well ahead of the reveal, so its first cue isn't lost while it spins up.
  revealSound.start();
  revealData = WinnerReveal.buildData(data, redSide);
};

// Handles a websocket message to play a sound to signal match start/stop/etc.
const handlePlaySound = function (sound) {
  $("audio").each(function (k, v) {
    // Stop and reset any sounds that are still playing.
    v.pause();
    v.currentTime = 0;
  });
  $("#sound-" + sound)[0].play();
};

// Handles a websocket message to update the alliance selection screen.
const handleAllianceSelection = function (data) {
  const alliances = data.Alliances;
  const rankedTeams = data.RankedTeams;
  if (alliances && alliances.length > 0) {
    const previousPicks = $(".selection-cell").map(function () {
      return $(this).text().trim();
    }).get();
    const numColumns = alliances[0].TeamIds.length + 1;
    $.each(alliances, function (k, v) {
      v.Index = k + 1;
    });
    $("#allianceSelection").html(allianceSelectionTemplate({alliances: alliances, numColumns: numColumns}));

    // Flash any cell that just received a pick so the audience's eye lands on it, and call the pick out at the bottom
    // of the screen. A batch of several at once is a bulk edit rather than picks happening live, so it isn't called out.
    const cells = $(".selection-cell");
    if (currentScreen === "allianceSelection" && previousPicks.length === cells.length) {
      const newPicks = [];
      cells.each(function (i) {
        const teamId = $(this).text().trim();
        if (previousPicks[i] === "" && teamId !== "") {
          this.animate([
            {backgroundColor: "rgba(255, 209, 102, 1)", transform: "scale(1.3)", color: "#0b1728"},
            {backgroundColor: "rgba(255, 209, 102, 0)", transform: "scale(1)"},
          ], {duration: 1100, easing: ease.in});
          newPicks.push({alliance: $(this).closest("tr").index() + 1, isCaptain: $(this).index() === 1, teamId: teamId});
        }
      });
      if (newPicks.length <= 2) {
        newPicks.forEach(queuePickCallout);
      }
    }
  }
  if (rankedTeams) {
    let text = "";
    $.each(rankedTeams, function (i, v) {
      if (!v.Picked) {
        text += `<div class="unpicked"><div class="unpicked-rank">${v.Rank}.</div>` +
          `<div class="unpicked-team">${v.TeamId}</div></div>`;
      }
    });
    $("#allianceRankings").html(text);
  }

  if (data.ShowTimer) {
    $("#allianceSelectionTimer").text(getCountdownString(data.TimeRemainingSec));
  } else {
    $("#allianceSelectionTimer").html("&nbsp;");
  }
};

// Pick callouts play one after another, so two picks entered close together each get their moment.
let pickCalloutChain = Promise.resolve();

const queuePickCallout = function (pick) {
  pickCalloutChain = pickCalloutChain.then(function () {
    return showPickCallout(pick);
  });
};

// Wipes a banner up from the bottom of the screen naming the team that was just picked, holds it, and wipes it away.
const showPickCallout = async function (pick) {
  if (currentScreen !== "allianceSelection") {
    return;
  }
  const callout = $("#pickCallout");
  $("#pickCalloutLabel").text(`Alliance ${pick.alliance} ${pick.isCaptain ? "captain" : "picks"}`);
  $("#pickCalloutTeam").text(pick.teamId);
  $("#pickCalloutAvatar").css("visibility", "visible").attr("src", getAvatarUrl(pick.teamId));
  callout.css("display", "flex");
  await Promise.all([
    animate("#pickCalloutBody", [
      {clipPath: "polygon(0 0, 0 0, 0 100%, 0 100%)"},
      {clipPath: "polygon(0 0, 100% 0, calc(100% - 28px) 100%, 0 100%)"},
    ], 500, ease.in),
    animate("#pickCalloutLabel", [
      {opacity: 0, transform: "translateY(100%)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 450, ease.in, 200),
    animate("#pickCalloutTeam", slideIn(-40), 500, ease.in, 150),
    animate("#pickCalloutAvatar", popIn, 500, ease.pop, 300),
  ]);
  await wait(2600);
  await Promise.all([
    animate("#pickCalloutLabel", [{opacity: 0}], 200, ease.out),
    animate("#pickCalloutBody", [{clipPath: "polygon(100% 0, 100% 0, calc(100% - 28px) 100%, calc(100% - 28px) 100%)"}],
      400, ease.out),
  ]);
  callout.hide();
};

// Handles a websocket message to populate and/or show/hide a lower third.
const handleLowerThird = function (data) {
  if (data.LowerThird !== null) {
    if (data.LowerThird.BottomText === "") {
      $("#lowerThirdTop").hide();
      $("#lowerThirdBottom").hide();
      $("#lowerThirdSingle").text(data.LowerThird.TopText);
      $("#lowerThirdSingle").show();
    } else {
      $("#lowerThirdSingle").hide();
      $("#lowerThirdTop").text(data.LowerThird.TopText);
      $("#lowerThirdBottom").text(data.LowerThird.BottomText);
      $("#lowerThirdTop").show();
      $("#lowerThirdBottom").show();
    }
  }

  const lowerThird = $("#lowerThird");
  if (data.ShowLowerThird && !lowerThirdVisible) {
    // Wipes open left to right behind a gliding accent bar, then the text settles in line by line.
    lowerThirdVisible = true;
    stopAnimations("#lowerThird, #lowerThird > *");
    lowerThird.show();
    animate(lowerThird, [
      {clipPath: "inset(0 100% 0 0)", transform: "translateX(-40px)"},
      {clipPath: "inset(0 0% 0 0)", transform: "translateX(0px)"},
    ], 800, ease.in);
    animate(lowerThird.children(":visible"), [
      {opacity: 0, transform: "translateY(12px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 550, ease.in, 250, 90);
  } else if (!data.ShowLowerThird && lowerThirdVisible) {
    lowerThirdVisible = false;
    stopAnimations("#lowerThird, #lowerThird > *");
    animate(lowerThird.children(":visible"), [{opacity: 0}], 200, ease.out);
    animate(lowerThird, [{clipPath: "inset(0 0 0 100%)", transform: "translateX(40px)"}], 550, ease.out, 100)
      .then(function () {
        if (!lowerThirdVisible) {
          lowerThird.hide();
        }
      });
  }
};

// ---------------------------------------------------------------------------------------------------------------------
// Match overlay building blocks. Each one animates a single layer of the broadcast bug and returns a promise, so the
// screen transitions below are just compositions of these with some overlap between them.
// ---------------------------------------------------------------------------------------------------------------------

// Raises the whole bug into frame while the center circle pops up into place.
const riseOverlay = function (delay) {
  return Promise.all([
    animate("#overlayCentering", [overlayCenteringHideParams, overlayCenteringShowParams], 750, ease.in, delay),
    animate("#matchCircle", [
      {opacity: 0, transform: "scale(0.3)"},
      // Ends on "none" rather than an identity transform so the circle doesn't keep a stacking context, which would pull
      // its momentum glow up in front of the score bar.
      {opacity: 1, transform: "none"},
    ], 700, ease.pop, delay + 150),
  ]);
};

// Drops the bug out of frame, shrinking the center circle away as it goes.
const sinkOverlay = function () {
  return Promise.all([
    animate("#matchCircle", [{opacity: 0, transform: "scale(0.5)"}], 350, ease.out),
    animate("#overlayCentering", [overlayCenteringHideParams], 550, ease.out, 80),
  ]);
};

// Whether the phase tab is up, which it is whenever the match readouts are.
let phaseTabShown = false;

// The phase tab slides out from behind the match circle, away from the nearest screen edge.
const showPhaseTab = function (delay) {
  phaseTabShown = true;
  const fromY = overlayCenteringShowParams === overlayCenteringTopShowParams ? -30 : 30;
  return animate("#phaseTab", [
    {opacity: 0, transform: `translateY(${fromY}px)`},
    {opacity: currentPhase === "" ? 0 : 1, transform: "translateY(0px)"},
  ], 500, ease.in, delay);
};

const hidePhaseTab = function () {
  phaseTabShown = false;
  return animate("#phaseTab", [{opacity: 0}], 200, ease.out);
};

const openInfoBar = function (delay) {
  $("#eventMatchInfo").css("display", "flex");
  return Promise.all([
    animate("#eventMatchInfo", [{height: eventMatchInfoUp}, {height: eventMatchInfoDown}], 550, ease.in, delay),
    animate("#eventMatchInfo > span", [
      {opacity: 0, transform: "translateY(-8px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 500, ease.in, delay + 150, 80),
  ]);
};

const closeInfoBar = function () {
  return animate("#eventMatchInfo", [{height: eventMatchInfoUp}], 350, ease.out).then(function () {
    $("#eventMatchInfo").hide();
  });
};

// Team numbers glide in from each side's outer edge, one after another.
const cascadeTeams = function (delay) {
  $(".teams").css("display", "flex");
  return Promise.all([
    animate("#leftTeams > div", slideIn(-20), 550, ease.in, delay, 70),
    animate("#rightTeams > div", slideIn(20), 550, ease.in, delay, 70),
  ]);
};

const fadeTeams = function () {
  return animate(".teams > div", [{opacity: 0}], 250, ease.out);
};

const revealMatchReadouts = function (delay) {
  return Promise.all([
    animate(".score-number", riseIn, 600, ease.in, delay, 90),
    animate("#matchTime", riseIn, 600, ease.in, delay + 140),
    animate(".score-fields", riseIn, 600, ease.in, delay + 200),
    showPhaseTab(delay + 250),
    wait(delay).then(hubActiveController.restartPendingHubActiveIndicators),
  ]);
};

const hideMatchReadouts = function () {
  return Promise.all([
    animate(".score-number, #matchTime, .score-fields", fadeOut, 260, ease.out),
    hidePhaseTab(),
  ]);
};

// Widens the score panels out to their full in-match layout.
const expandMatch = function (delay) {
  $(".score-fields").css("display", "flex");
  return Promise.all([
    animate(".score", [{width: scoreOut}], 750, ease.in, delay),
    animate(".score-fields", [{width: "0px"}, {width: scoreFieldsOut}], 750, ease.in, delay),
    animate("#logo", [{top: logoUp}], 650, ease.move, delay + 100),
    revealMatchReadouts(delay + 350),
  ]);
};

// Folds the in-match layout back down to a bare center circle (or to the intro width, if given).
const collapseMatch = async function (targetScoreWidth) {
  await Promise.all([hideMatchReadouts(), closeInfoBar()]);
  await Promise.all([
    animate(".score-fields", [{width: "0px"}], 500, ease.move),
    animate(".score", [{width: targetScoreWidth}], 500, ease.move),
    animate("#logo", [{top: logoDown}], 500, ease.move),
  ]);
  $(".score-fields").hide();
};

const popAvatars = function (delay) {
  $(".avatars").css({display: "flex", opacity: 1});
  return animate(".avatar", popIn, 550, ease.pop, delay, 60);
};

const hideAvatars = function () {
  return animate(".avatar", popOut, 250, ease.out, 0, 30).then(function () {
    $(".avatars").hide();
  });
};

// Opens the pre-match intro: alliance colors, team numbers and avatars.
const expandIntro = function (delay) {
  $("#matchOverlay").attr("data-mode", "intro");
  return Promise.all([
    animate(".score", [{width: scoreMid}], 700, ease.in, delay),
    cascadeTeams(delay + 120),
    popAvatars(delay + 250),
    openInfoBar(delay + 400),
  ]);
};

const collapseIntro = async function () {
  $("#matchOverlay").removeAttr("data-mode");
  await Promise.all([closeInfoBar(), hideAvatars(), fadeTeams()]);
  await animate(".score", [{width: scoreIn}], 450, ease.move);
  $(".teams").hide();
};

// Unfurls the timeout banner out from behind the center circle, with the break details sliding outward from it.
const expandTimeout = function (delay) {
  return Promise.all([
    animate("#timeoutDetails", [
      {width: timeoutDetailsIn, opacity: 0},
      {width: timeoutDetailsOut, opacity: 1},
    ], 750, ease.in, delay),
    animate("#logo", [{top: logoUp}], 650, ease.move, delay),
    animate("#timeoutBreakDescription", slideIn(40), 600, ease.in, delay + 300),
    animate("#timeoutNextMatch", slideIn(-40), 600, ease.in, delay + 360),
    animate("#matchTime", riseIn, 600, ease.in, delay + 250),
  ]);
};

const collapseTimeout = async function () {
  await animate(".timeout-detail, #matchTime", fadeOut, 260, ease.out);
  await Promise.all([
    animate("#timeoutDetails", [{width: timeoutDetailsIn, opacity: 0}], 500, ease.move),
    animate("#logo", [{top: logoDown}], 500, ease.move),
  ]);
};

// ---------------------------------------------------------------------------------------------------------------------
// Match overlay screens.
// ---------------------------------------------------------------------------------------------------------------------

const transitionBlankToMatch = function () {
  return Promise.all([riseOverlay(0), cascadeTeams(250), expandMatch(200), openInfoBar(700)]);
};

const transitionMatchToBlank = async function () {
  // An abort blanks the display at the same moment it's announced; give the notice a beat to land, then keep the bar up
  // while the abort plays out over it.
  await wait(abortGraceMs);
  if (abortBarHold !== null) {
    await abortBarHold;
  }
  await Promise.all([collapseMatch(scoreIn), fadeTeams()]);
  $(".teams").hide();
  await sinkOverlay();
};

// Sound cues for the full-screen intro, reusing the winner reveal's synthesizer.
const matchIntroSound = {
  whoosh: function () {
    revealSound.start().then(function () {
      revealSound.whoosh(0.8, 0, 0.28);
    });
  },
  slam: function () {
    revealSound.slam();
  },
};

const transitionBlankToIntro = function () {
  return Promise.all([riseOverlay(0), expandIntro(200)]);
};

// The full-screen team intro holds until the operator moves on; it then splits apart with "VS" diving towards the
// bug, which rises and opens up underneath it.
const leaveTeamIntro = function () {
  return MatchIntro.leave(overlayCenteringShowParams === overlayCenteringTopShowParams, matchIntroSound);
};

const transitionBlankToTeamIntro = function () {
  return MatchIntro.play(matchIntroSound);
};

const transitionTeamIntroToBlank = function () {
  return leaveTeamIntro();
};

const transitionTeamIntroToIntro = function () {
  return Promise.all([leaveTeamIntro(), riseOverlay(300), expandIntro(500)]);
};

const transitionTeamIntroToMatch = function () {
  return Promise.all([leaveTeamIntro(), riseOverlay(300), cascadeTeams(550), expandMatch(500), openInfoBar(1000)]);
};

const transitionIntroToBlank = async function () {
  await collapseIntro();
  await sinkOverlay();
};

const transitionIntroToMatch = function () {
  $("#matchOverlay").removeAttr("data-mode");
  return Promise.all([hideAvatars(), expandMatch(120)]);
};

const transitionMatchToIntro = async function () {
  await collapseMatch(scoreMid);
  $("#matchOverlay").attr("data-mode", "intro");
  await Promise.all([popAvatars(0), openInfoBar(150)]);
};

const transitionBlankToTimeout = function () {
  return Promise.all([riseOverlay(0), expandTimeout(250)]);
};

const transitionTimeoutToBlank = async function () {
  await collapseTimeout();
  await sinkOverlay();
};

const transitionIntroToTimeout = async function () {
  await collapseIntro();
  await expandTimeout(0);
};

const transitionTimeoutToIntro = async function () {
  await collapseTimeout();
  await expandIntro(0);
};

// ---------------------------------------------------------------------------------------------------------------------
// Full-screen building blocks: the logo disc, the iris that opens the backdrop out from behind it, and the content
// panels (final score, bracket, sponsors) that sit on top of the backdrop.
// ---------------------------------------------------------------------------------------------------------------------

const discTransform = function (y, scale) {
  return `translateY(${y}px) scale(${scale})`;
};

// Pops the disc up out of nothing in the center of the screen.
const showDisc = function (delay) {
  return Promise.all([
    animate("#logoDisc", [
      {opacity: 0, transform: discTransform(discCenterY, 0.4)},
      {opacity: 1, transform: discTransform(discCenterY, 1)},
    ], 850, ease.pop, delay),
    animate("#blindsLogo", [
      {opacity: 0, transform: "scale(0.8)"},
      {opacity: 1, transform: "scale(1)"},
    ], 700, ease.in, delay + 200),
  ]);
};

const hideDisc = function (delay) {
  return Promise.all([
    animate("#blindsLogo", [{opacity: 0}], 250, ease.out, delay),
    animate("#logoDisc", [{opacity: 0, transform: discTransform(discCenterY, 0.6)}], 400, ease.out, delay + 80),
  ]);
};

// Glides the disc to a new resting place (and back into view, if it had been faded away).
const moveDisc = function (y, scale, delay = 0) {
  return animate("#logoDisc", [{opacity: 1, transform: discTransform(y, scale)}], 850, ease.move, delay);
};

// Opens the backdrop as an expanding circle, starting from the given radius. A thin ring rides out ahead of the edge.
const openIris = function (delay, fromRadius) {
  return Promise.all([
    animate("#blindsBackdrop", [
      {clipPath: `circle(${fromRadius} at 50% 50%)`},
      // A percentage rather than viewport units, which the stage zoom would shrink; 75% reaches past the corners.
      {clipPath: "circle(75% at 50% 50%)"},
    ], 1200, ease.in, delay),
    animate("#blindsRing", [
      {opacity: 1, transform: "scale(1)"},
      {opacity: 0, transform: "scale(7)"},
    ], 1300, ease.in, delay),
  ]);
};

const closeIris = function (delay = 0) {
  return animate("#blindsBackdrop", [{clipPath: "circle(0px at 50% 50%)"}], 700, ease.out, delay);
};

// Once the scores have finished counting, the verdict lands: the losing score steps back as the winning one flashes
// gold, the badges stamp down and any rank changes tick up or down.
const settleFinalScore = function () {
  $(".final-score").attr("data-settled", "");
  // Not through animate(), which would leave an inline color behind to override the dimming the next time this side
  // loses.
  $(".final-score[data-result=winner]").each(function () {
    this.animate([{color: "#fff"}, {color: "#ffd166", offset: 0.25}, {color: "#fff"}], {duration: 900, easing: ease.move});
  });
  return Promise.all([
    animate(".final-result-indicator, .final-destination", stamp(1.8), 420, "linear", 0, 90),
    animate("#finalTiebreakReason", [
      {opacity: 0, transform: "translateY(-12px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 500, ease.in, 300),
    animate(".final-team-rank img[src$='rank-up.svg']", [
      {opacity: 0, transform: "translateY(12px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 500, ease.pop, 250, 60),
    animate(".final-team-rank img[src$='rank-down.svg']", [
      {opacity: 0, transform: "translateY(-12px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 500, ease.pop, 250, 60),
  ]);
};

// The final score card splits open from the middle, and the scores count up under the logo disc while the teams and
// breakdown fill in around them. Straight after the winner reveal, which has just tallied the scores, they're shown as
// they are rather than counted again.
const revealFinalScore = function (afterReveal) {
  const finalScore = $("#finalScore");
  finalScore.show();
  $(".final-score").removeAttr("data-settled");
  // These only come in once the scores have finished counting.
  $(".final-result-indicator, .final-destination, #finalTiebreakReason, .final-team-rank img").css("opacity", 0);

  // Stagger the breakdown row by row, running all three columns in lockstep so each line reads across as a unit.
  const breakdownRows = [];
  $("#leftFinalBreakdown, #centerFinalBreakdown, #rightFinalBreakdown").each(function () {
    const rows = $(this).find("div:visible").filter(function () {
      return $(this).children("div").length === 0;
    });
    breakdownRows.push(animate(rows, [
      {opacity: 0, transform: "translateY(12px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 550, ease.in, 450, 50));
  });

  const countMs = afterReveal ? 0 : 1400;
  const countDelayMs = afterReveal ? 600 : 350;
  const scoreCounts = afterReveal ? [] : $(".final-score").toArray().map(function (element) {
    return DisplayEffects.countUp(element, countMs, countDelayMs);
  });

  return Promise.all([
    // The clip would hide the badges hanging off the card's edges, so it's dropped as soon as the card is open.
    animate(finalScore, [
      {opacity: 1, clipPath: "inset(0% 50% 0% 50%)"},
      {opacity: 1, clipPath: "inset(0% 0% 0% 0%)"},
    ], 650, ease.in).then(function () {
      finalScore.css("clip-path", "");
    }),
    animate(".final-score", [
      {opacity: 0, transform: "translateY(24px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 600, ease.in, 200),
    animate(".final-teams.reversible-left .final-team-row", slideIn(-24), 600, ease.in, 400, 70),
    animate(".final-teams.reversible-right .final-team-row", slideIn(24), 600, ease.in, 400, 70),
    animate(".final-alliance", [{opacity: 0}, {opacity: 1}], 600, ease.in, 350),
    animate("#finalEventMatchInfo > div", [
      {opacity: 0, transform: "translateY(8px)"},
      {opacity: 1, transform: "translateY(0px)"},
    ], 600, ease.in, 700, 100),
    ...breakdownRows,
    ...scoreCounts,
    wait(countDelayMs + countMs).then(settleFinalScore),
  ]);
};

const hideFinalScore = function () {
  return animate("#finalScore", [{opacity: 0, transform: "translateY(28px) scale(0.97)"}], 450, ease.out)
    .then(function () {
      $("#finalScore").hide();
    });
};

const revealBracket = function (delay) {
  $("#bracket").show();
  return animate("#bracket", [
    {opacity: 0, transform: "scale(1.04)"},
    {opacity: 1, transform: "scale(1)"},
  ], 800, ease.in, delay);
};

const hideBracket = function () {
  return animate("#bracket", [{opacity: 0, transform: "scale(0.97)"}], 450, ease.out)
    .then(function () {
      $("#bracket").hide();
    });
};

// The qualification standings, built once the page has loaded.
let standingsTable = null;

const revealStandings = async function (delay) {
  await standingsTable.load();
  standingsTable.render(0, delay + 350);
  $("#standings").show();
  standingsTable.startCycle();
  return animate("#standings", [
    {opacity: 0, transform: "scale(1.04)"},
    {opacity: 1, transform: "scale(1)"},
  ], 800, ease.in, delay);
};

const hideStandings = function () {
  standingsTable.stopCycle();
  return animate("#standings", [{opacity: 0, transform: "scale(0.97)"}], 450, ease.out)
    .then(function () {
      $("#standings").hide();
    });
};

// Standalone sponsor entrance: the card lifts up into place.
const revealSponsor = function (delay) {
  $("#sponsor").show();
  return animate("#sponsor", [
    {opacity: 0, transform: "translateY(48px) scale(0.95)", clipPath: "circle(620px at 50% 50%)"},
    {opacity: 1, transform: "translateY(0px) scale(1)", clipPath: "circle(620px at 50% 50%)"},
  ], 900, ease.in, delay);
};

const hideSponsor = function () {
  return animate("#sponsor", [{opacity: 0, transform: "translateY(28px) scale(0.97)"}], 450, ease.out)
    .then(function () {
      $("#sponsor").hide();
    });
};

// Plays the winner reveal, then brings in the final score as the reveal wipes away.
const presentFinalScore = async function () {
  if (revealData !== null) {
    await WinnerReveal.play(revealData, redSide);
  }
  await revealFinalScore(revealData !== null);
};

// ---------------------------------------------------------------------------------------------------------------------
// Full-screen screens.
// ---------------------------------------------------------------------------------------------------------------------

const transitionBlankToLogo = function () {
  return Promise.all([showDisc(0), openIris(350, discRadius)]);
};

const transitionLogoToBlank = function () {
  return Promise.all([closeIris(), hideDisc(400)]);
};

// The luma variant is the disc alone, with no backdrop behind it.
const transitionBlankToLogoLuma = function () {
  return showDisc(0);
};

const transitionLogoLumaToBlank = function () {
  return hideDisc(0);
};

const transitionLogoLumaToLogo = function () {
  return openIris(0, discRadius);
};

const transitionLogoToLogoLuma = function () {
  return closeIris();
};

const transitionLogoToScore = async function () {
  await Promise.all([moveDisc(discScoreY, 1), wait(1000)]);
  await presentFinalScore();
};

const transitionScoreToLogo = function () {
  return Promise.all([hideFinalScore(), moveDisc(discCenterY, 1, 150)]);
};

const transitionLogoToBracket = function () {
  return Promise.all([moveDisc(discBracketY, discBracketScale), revealBracket(250)]);
};

const transitionBracketToLogo = function () {
  return Promise.all([hideBracket(), moveDisc(discCenterY, 1, 150)]);
};

const transitionLogoToStandings = function () {
  return Promise.all([moveDisc(discBracketY, discBracketScale), revealStandings(250)]);
};

const transitionStandingsToLogo = function () {
  return Promise.all([hideStandings(), moveDisc(discCenterY, 1, 150)]);
};

// The disc is already up top for both, so the one panel just swaps for the other underneath it.
const transitionBracketToStandings = function () {
  return Promise.all([hideBracket(), revealStandings(300)]);
};

const transitionStandingsToBracket = function () {
  return Promise.all([hideStandings(), revealBracket(300)]);
};

const transitionScoreToStandings = async function () {
  await Promise.all([hideFinalScore(), moveDisc(discBracketY, discBracketScale, 100), revealStandings(500)]);
};

const transitionStandingsToScore = async function () {
  await Promise.all([hideStandings(), moveDisc(discScoreY, 1, 100), wait(1000)]);
  await presentFinalScore();
};

const transitionBlankToStandings = async function () {
  await transitionBlankToLogo();
  await transitionLogoToStandings();
};

const transitionStandingsToBlank = async function () {
  await transitionStandingsToLogo();
  await transitionLogoToBlank();
};

const transitionBracketToScore = async function () {
  await Promise.all([hideBracket(), moveDisc(discScoreY, 1, 100), wait(1000)]);
  await presentFinalScore();
};

const transitionScoreToBracket = async function () {
  await Promise.all([hideFinalScore(), moveDisc(discBracketY, discBracketScale, 100), revealBracket(500)]);
};

// The disc melts into the sponsor card: the card grows outward from the disc's exact outline as the disc fades.
const transitionLogoToSponsor = function () {
  $("#sponsor").show();
  return Promise.all([
    animate("#logoDisc", [{opacity: 0, transform: discTransform(discCenterY, 0.85)}], 450, ease.out),
    animate("#sponsor", [
      {opacity: 1, transform: "translateY(0px) scale(1)", clipPath: `circle(${discRadius} at 50% 50%)`},
      {opacity: 1, transform: "translateY(0px) scale(1)", clipPath: "circle(620px at 50% 50%)"},
    ], 900, ease.in, 100),
  ]);
};

// The reverse: the card contracts back down to the disc's outline and the disc reappears in its place.
const transitionSponsorToLogo = async function () {
  await Promise.all([
    animate("#sponsor", [{opacity: 0, clipPath: `circle(${discRadius} at 50% 50%)`}], 650, ease.move),
    animate("#logoDisc", [{opacity: 1, transform: discTransform(discCenterY, 1)}], 650, ease.in, 300),
  ]);
  $("#sponsor").hide();
};

const transitionBlankToSponsor = function () {
  return Promise.all([openIris(0, "0px"), revealSponsor(450)]);
};

const transitionSponsorToBlank = function () {
  return Promise.all([hideSponsor(), closeIris(250)]);
};

const transitionBlankToScore = async function () {
  await transitionBlankToLogo();
  await transitionLogoToScore();
};

const transitionScoreToBlank = async function () {
  await transitionScoreToLogo();
  await transitionLogoToBlank();
};

const transitionScoreToLogoLuma = async function () {
  await transitionScoreToLogo();
  await transitionLogoToLogoLuma();
};

const transitionScoreToSponsor = async function () {
  await transitionScoreToLogo();
  await transitionLogoToSponsor();
};

const transitionBlankToBracket = async function () {
  await transitionBlankToLogo();
  await transitionLogoToBracket();
};

const transitionBracketToBlank = async function () {
  await transitionBracketToLogo();
  await transitionLogoToBlank();
};

const transitionBracketToLogoLuma = async function () {
  await transitionBracketToLogo();
  await transitionLogoToLogoLuma();
};

const transitionBracketToSponsor = async function () {
  await transitionBracketToLogo();
  await transitionLogoToSponsor();
};

const transitionLogoLumaToBracket = async function () {
  await transitionLogoLumaToLogo();
  await transitionLogoToBracket();
};

const transitionLogoLumaToScore = async function () {
  await transitionLogoLumaToLogo();
  await transitionLogoToScore();
};

const transitionSponsorToBracket = async function () {
  await transitionSponsorToLogo();
  await transitionLogoToBracket();
};

const transitionSponsorToScore = async function () {
  await transitionSponsorToLogo();
  await transitionLogoToScore();
};

// ---------------------------------------------------------------------------------------------------------------------
// Alliance selection.
// ---------------------------------------------------------------------------------------------------------------------

const transitionBlankToAllianceSelection = function () {
  $("#allianceSelectionCentering").show();
  $("#allianceRankingsCentering.enabled").show();
  return Promise.all([
    animate("#allianceSelectionCentering", slideIn(90), 800, ease.in),
    animate("#allianceSelectionTable tr", slideIn(40), 650, ease.in, 150, 45),
    animate("#allianceRankingsCentering.enabled", slideIn(-90), 800, ease.in, 100),
    animate("#allianceRankings .unpicked", [{opacity: 0}, {opacity: 1}], 500, ease.in, 300, 12),
  ]);
};

const transitionAllianceSelectionToBlank = async function () {
  await Promise.all([
    animate("#allianceSelectionCentering", [{opacity: 0, transform: "translateX(70px)"}], 450, ease.out),
    animate("#allianceRankingsCentering.enabled", [{opacity: 0, transform: "translateX(-70px)"}], 450, ease.out),
  ]);
  $("#allianceSelectionCentering, #allianceRankingsCentering").hide();
};

// Loads sponsor slide data and builds the slideshow HTML.
const initializeSponsorDisplay = function () {
  $.getJSON("/api/sponsor_slides", function (slides) {
    $("#sponsorContainer").empty();

    // Inject the HTML for each slide into the DOM.
    $.each(slides, function (index, slide) {
      slide.DisplayTimeMs = slide.DisplayTimeSec * 1000;
      slide.First = index === 0;

      let slideHtml;
      if (slide.Image) {
        slideHtml = sponsorImageTemplate(slide);
      } else {
        slideHtml = sponsorTextTemplate(slide);
      }
      $("#sponsorContainer").append(slideHtml);
    });
  });
};

const getAvatarUrl = function (teamId) {
  return DisplayShared.getAvatarUrl(teamId);
};

const setTeamInfo = function (side, position, teamId, cards, rankings) {
  const teamNumberElement = $(`#${side}FinalTeam${position}`);
  teamNumberElement.empty().append($("<span class='final-team-number-text'>").text(teamId));
  teamNumberElement.toggle(teamId > 0);
  const avatarElement = $(`#${side}FinalTeam${position}Avatar`);
  avatarElement.attr("src", getAvatarUrl(teamId));
  avatarElement.toggle(teamId > 0);

  const cardElement = $(`#${side}FinalTeam${position}Card`);
  cardElement.attr("data-card", cards[teamId.toString()] || "");

  const ranking = rankings[teamId];
  let rankIndicator = "";
  let rankNumber = "";
  if (ranking !== undefined && ranking !== null && ranking.Rank !== 0) {
    rankNumber = ranking.Rank;
    if (rankNumber > ranking.PreviousRank && ranking.PreviousRank > 0) {
      rankIndicator = "rank-down";
    } else if (rankNumber < ranking.PreviousRank) {
      rankIndicator = "rank-up";
    }
  }

  const rankIndicatorElement = $(`#${side}FinalTeam${position}RankIndicator`);
  rankIndicatorElement.attr("src", rankIndicator === "" ? "" : `/static/img/${rankIndicator}.svg`);
  rankIndicatorElement.toggle(rankIndicator !== "" && teamId > 0);

  const rankNumberElement = $(`#${side}FinalTeam${position}RankNumber`);
  rankNumberElement.text(rankNumber);
  rankNumberElement.toggle(teamId > 0);
};

$(function () {
  // Read the configuration for this display from the URL query string.
  const urlParams = new URLSearchParams(window.location.search);
  DisplayShared.fitStage();
  standingsTable = StandingsTable.create(document.getElementById("standingsPanel"));
  document.body.style.backgroundColor = urlParams.get("background");
  // The stylesheet gives <html> a background too, which stops the body color from filling the screen; override it so
  // chroma-key backgrounds (e.g. #0f0) cover the whole display.
  document.documentElement.style.backgroundColor = urlParams.get("background");
  const sides = DisplayShared.applyDisplaySides(urlParams);
  redSide = sides.redSide;
  blueSide = sides.blueSide;
  // Lets the stylesheet color things that aren't themselves reversible (like the team columns' edges) by side.
  $("body").attr("data-reversed", urlParams.get("reversed") === "true");
  const overlayAtTop = urlParams.get("overlayLocation") === "top";
  $("body").attr("data-overlay-location", overlayAtTop ? "top" : "bottom");
  if (overlayAtTop) {
    overlayCenteringHideParams = overlayCenteringTopHideParams;
    overlayCenteringShowParams = overlayCenteringTopShowParams;
    $("#overlayCentering").css("top", overlayCenteringTopUp);
  } else {
    overlayCenteringHideParams = overlayCenteringBottomHideParams;
    overlayCenteringShowParams = overlayCenteringBottomShowParams;
  }
  // Score chips drift away from whichever screen edge the bar is on.
  scorePopper = DisplayShared.createScorePopper(overlayAtTop);

  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/displays/audience/websocket", {
    allianceSelection: function (event) {
      handleAllianceSelection(event.data);
    },
    audienceDisplayMode: function (event) {
      handleAudienceDisplayMode(event.data);
    },
    lowerThird: function (event) {
      handleLowerThird(event.data);
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
    playSound: function (event) {
      handlePlaySound(event.data);
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
    allianceSelection: {
      blank: transitionAllianceSelectionToBlank,
    },
    blank: {
      allianceSelection: transitionBlankToAllianceSelection,
      bracket: transitionBlankToBracket,
      intro: transitionBlankToIntro,
      logo: transitionBlankToLogo,
      logoLuma: transitionBlankToLogoLuma,
      match: transitionBlankToMatch,
      score: transitionBlankToScore,
      sponsor: transitionBlankToSponsor,
      standings: transitionBlankToStandings,
      teamIntro: transitionBlankToTeamIntro,
      timeout: transitionBlankToTimeout,
    },
    bracket: {
      blank: transitionBracketToBlank,
      logo: transitionBracketToLogo,
      logoLuma: transitionBracketToLogoLuma,
      score: transitionBracketToScore,
      sponsor: transitionBracketToSponsor,
      standings: transitionBracketToStandings,
    },
    intro: {
      blank: transitionIntroToBlank,
      match: transitionIntroToMatch,
      timeout: transitionIntroToTimeout,
    },
    logo: {
      blank: transitionLogoToBlank,
      bracket: transitionLogoToBracket,
      logoLuma: transitionLogoToLogoLuma,
      score: transitionLogoToScore,
      sponsor: transitionLogoToSponsor,
      standings: transitionLogoToStandings,
    },
    logoLuma: {
      blank: transitionLogoLumaToBlank,
      bracket: transitionLogoLumaToBracket,
      logo: transitionLogoLumaToLogo,
      score: transitionLogoLumaToScore,
    },
    match: {
      blank: transitionMatchToBlank,
      intro: transitionMatchToIntro,
    },
    score: {
      blank: transitionScoreToBlank,
      bracket: transitionScoreToBracket,
      logo: transitionScoreToLogo,
      logoLuma: transitionScoreToLogoLuma,
      sponsor: transitionScoreToSponsor,
      standings: transitionScoreToStandings,
    },
    standings: {
      blank: transitionStandingsToBlank,
      bracket: transitionStandingsToBracket,
      logo: transitionStandingsToLogo,
      score: transitionStandingsToScore,
    },
    sponsor: {
      blank: transitionSponsorToBlank,
      bracket: transitionSponsorToBracket,
      logo: transitionSponsorToLogo,
      score: transitionSponsorToScore,
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
