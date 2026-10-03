// Copyright 2026 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Shared helpers for audience and wall displays.

(function (window) {
  window.DisplayShared = {
    applyDisplaySides: function (urlParams) {
      const reversed = urlParams.get("reversed");
      const redSide = reversed === "true" ? "right" : "left";
      const blueSide = reversed === "true" ? "left" : "right";
      $(".reversible-left").attr("data-reversed", reversed);
      $(".reversible-right").attr("data-reversed", reversed);
      return {redSide: redSide, blueSide: blueSide};
    },

    getAvatarUrl: function (teamId) {
      return "/api/teams/" + teamId + "/avatar";
    },

    // Scales a page laid out in fixed pixels for a 1920x1080 screen so that it fits whatever screen it's shown on. It
    // zooms by whichever of width and height is tighter, so on a screen of a different shape the layout gets extra room
    // along the other axis rather than being cropped or letterboxed, and edge-anchored elements stay on their edges.
    // Layers sized in viewport units already fit any screen; they undo the zoom by reading --stage-zoom.
    fitStage: function () {
      const fit = function () {
        const zoom = Math.min(window.innerWidth / 1920, window.innerHeight / 1080);
        document.documentElement.style.zoom = zoom;
        document.documentElement.style.setProperty("--stage-zoom", zoom);
      };
      fit();
      window.addEventListener("resize", fit);
    },

    // Returns the points in a live score summary that are worth calling out as they happen, by the label to call them
    // out with. Fuel isn't among them: it scores almost continuously, so it just moves the number. Teleop tower points
    // aren't either, since they're only added after the match.
    getCalloutPoints: function (scoreSummary) {
      return {TOWER: scoreSummary.AutoTowerPoints, FOUL: scoreSummary.FoulPoints};
    },

    // Returns a function that calls out a batch of points on one side's score, such as a climb or foul points: the number
    // punches and flashes gold, and a chip like "+15 TOWER" floats off it. More of the same kind landing within a moment
    // is added into the same chip rather than stacking up new ones. It's meant for discrete events only; fuel scores
    // almost continuously and would have it firing constantly. floatsDown picks which way the chips drift, away from the
    // nearest screen edge.
    createScorePopper: function (floatsDown) {
      const chips = {};
      const easeIn = "cubic-bezier(0.16, 1, 0.3, 1)";
      return function (side, points, label) {
        const number = document.getElementById(`${side}ScoreNumber`);
        if (number === null || points <= 0) {
          return;
        }
        number.animate([
          {transform: "scale(1)", color: "#fff"},
          {transform: "scale(1.25)", color: "#ffd166", offset: 0.25},
          {transform: "scale(1)", color: "#fff"},
        ], {duration: 500, easing: easeIn});

        const now = Date.now();
        let chip = chips[side];
        if (chip !== undefined && chip.label === label && now - chip.startedAt < 900 && chip.element.isConnected) {
          chip.points += points;
          chip.animation.cancel();
        } else {
          if (chip !== undefined) {
            chip.element.remove();
          }
          // Nudged outwards from the middle of the screen, to keep clear of whatever sits on the match circle.
          const rect = number.getBoundingClientRect();
          const centerX = rect.left + rect.width / 2;
          const outward = centerX < window.innerWidth / 2 ? -1 : 1;
          const element = document.createElement("div");
          element.className = "score-chip";
          element.style.left = centerX + outward * rect.width * 0.2 + "px";
          element.style.top = (floatsDown ? rect.bottom - 10 : rect.top - 30) + "px";
          document.body.appendChild(element);
          chip = {element: element, points: points, label: label};
          chips[side] = chip;
        }
        chip.startedAt = now;
        chip.element.textContent = `+${chip.points}`;
        $("<span class='score-chip-label'>").text(label).appendTo(chip.element);
        const drift = floatsDown ? 1 : -1;
        chip.animation = chip.element.animate([
          {opacity: 0, transform: "translate(-50%, 0px) scale(0.5)"},
          {opacity: 1, transform: `translate(-50%, ${drift * 14}px) scale(1.1)`, offset: 0.15},
          {opacity: 1, transform: `translate(-50%, ${drift * 26}px) scale(1)`, offset: 0.7},
          {opacity: 0, transform: `translate(-50%, ${drift * 44}px) scale(1)`},
        ], {duration: 1300, easing: "cubic-bezier(0.25, 0.8, 0.4, 1)", fill: "forwards"});
        const finishedChip = chip;
        chip.animation.onfinish = function () {
          finishedChip.element.remove();
        };
      };
    },

    handleMatchLoad: function (data, redSide, blueSide) {
      const currentMatch = data.Match;
      $(`#${redSide}Team1`).text(currentMatch.Red1);
      $(`#${redSide}Team1`).attr("data-yellow-card", data.Teams["R1"]?.YellowCard);
      $(`#${redSide}Team2`).text(currentMatch.Red2);
      $(`#${redSide}Team2`).attr("data-yellow-card", data.Teams["R2"]?.YellowCard);
      $(`#${redSide}Team3`).text(currentMatch.Red3);
      $(`#${redSide}Team3`).attr("data-yellow-card", data.Teams["R3"]?.YellowCard);
      $(`#${redSide}Team1Avatar`).attr("src", this.getAvatarUrl(currentMatch.Red1));
      $(`#${redSide}Team2Avatar`).attr("src", this.getAvatarUrl(currentMatch.Red2));
      $(`#${redSide}Team3Avatar`).attr("src", this.getAvatarUrl(currentMatch.Red3));
      $(`#${blueSide}Team1`).text(currentMatch.Blue1);
      $(`#${blueSide}Team1`).attr("data-yellow-card", data.Teams["B1"]?.YellowCard);
      $(`#${blueSide}Team2`).text(currentMatch.Blue2);
      $(`#${blueSide}Team2`).attr("data-yellow-card", data.Teams["B2"]?.YellowCard);
      $(`#${blueSide}Team3`).text(currentMatch.Blue3);
      $(`#${blueSide}Team3`).attr("data-yellow-card", data.Teams["B3"]?.YellowCard);
      $(`#${blueSide}Team1Avatar`).attr("src", this.getAvatarUrl(currentMatch.Blue1));
      $(`#${blueSide}Team2Avatar`).attr("src", this.getAvatarUrl(currentMatch.Blue2));
      $(`#${blueSide}Team3Avatar`).attr("src", this.getAvatarUrl(currentMatch.Blue3));

      if (currentMatch.Type === matchTypePlayoff) {
        $(`#${redSide}PlayoffAlliance`).text(currentMatch.PlayoffRedAlliance);
        $(`#${blueSide}PlayoffAlliance`).text(currentMatch.PlayoffBlueAlliance);
        $(".playoff-alliance").show();

        // In playoffs a yellow card belongs to the whole alliance, so it's flagged once with a glyph on the alliance's
        // team column rather than on each team.
        [[redSide, ["R1", "R2", "R3"]], [blueSide, ["B1", "B2", "B3"]]].forEach(function ([side, stations]) {
          const allianceCarded = stations.some(function (station) {
            return data.Teams[station]?.YellowCard;
          });
          $(`#${side}Teams`).attr("data-alliance-card", allianceCarded ? "yellow" : null);
          $(`#${side}Teams > div`).attr("data-yellow-card", false);
        });

        if (data.Matchup.NumWinsToAdvance > 1) {
          $(`#${redSide}PlayoffAllianceWins`).text(data.Matchup.RedAllianceWins);
          $(`#${blueSide}PlayoffAllianceWins`).text(data.Matchup.BlueAllianceWins);
          $("#playoffSeriesStatus").css("display", "flex");
        } else {
          $("#playoffSeriesStatus").hide();
        }
      } else {
        $(`#${redSide}PlayoffAlliance`).text("");
        $(`#${blueSide}PlayoffAlliance`).text("");
        $(".playoff-alliance").hide();
        $(".teams").removeAttr("data-alliance-card");
        $("#playoffSeriesStatus").hide();
      }

      let matchName = data.Match.LongName;
      if (data.Match.NameDetail !== "") {
        matchName += " &ndash; " + data.Match.NameDetail;
      }
      $("#matchName").html(matchName);
      const timeoutNextMatchName = data.BreakNextMatchName || "";
      const timeoutDetailOpacity = $("#timeoutBreakDescription").css("opacity");
      $("#timeoutNextMatch").toggle(timeoutNextMatchName !== "").css(
        "opacity", timeoutNextMatchName === "" ? 0 : timeoutDetailOpacity
      );
      $("#timeoutNextMatchName").text(timeoutNextMatchName);
      $("#timeoutBreakDescription").text(data.BreakDescription);
      return currentMatch;
    },

    handleMatchTime: function (data) {
      translateMatchTime(data, function (matchState, matchStateText, countdownSec) {
        $("#matchTime").text(getCountdownString(countdownSec));
      });
    },

    handle2026RealtimeScore: function (data, currentMatch, redSide, blueSide, updateHubActiveIndicator) {
      $(`#${redSide}ScoreNumber`).text(data.Red.ScoreSummary.Score - data.Red.ScoreSummary.PostMatchPoints);
      $(`#${blueSide}ScoreNumber`).text(data.Blue.ScoreSummary.Score - data.Blue.ScoreSummary.PostMatchPoints);

      $(`#${redSide}FuelNumerator`).text(data.Red.ScoreSummary.NumFuel - data.Red.ScoreSummary.NumFuelPostMatch);
      $(`#${redSide}FuelDenominator`).text(data.Red.ScoreSummary.NumFuelGoal);
      $(`#${blueSide}FuelNumerator`).text(data.Blue.ScoreSummary.NumFuel - data.Blue.ScoreSummary.NumFuelPostMatch);
      $(`#${blueSide}FuelDenominator`).text(data.Blue.ScoreSummary.NumFuelGoal);
      if (currentMatch && currentMatch.Type === matchTypePlayoff) {
        $(`#${redSide}FuelDenominator`).hide();
        $(`#${blueSide}FuelDenominator`).hide();
      } else {
        $(`#${redSide}FuelDenominator`).show();
        $(`#${blueSide}FuelDenominator`).show();
      }

      updateHubActiveIndicator(redSide, data.Red.ActiveRemainingSec, data.Red.ActiveDurationSec);
      updateHubActiveIndicator(blueSide, data.Blue.ActiveRemainingSec, data.Blue.ActiveDurationSec);
    },

    createHubActiveController: function (getCurrentScreen) {
      const activeProgressLength = 158;
      const leftActiveProgressStartOffset = parseFloat($("#leftHubActive svg .active-progress").attr("stroke-dashoffset"));
      const rightActiveProgressStartOffset = parseFloat(
        $("#rightHubActive svg .active-progress").attr("stroke-dashoffset")
      );
      const activeFadeTimeMs = 300;
      const activeDwellTimeMs = 500;
      const hubActiveStateBySide = {
        left: {
          active: false, lastRemainingSec: 0, lastDurationSec: 0,
          activeUntilTimeMs: null,
          hideTimeoutId: null, resetTimeoutId: null, animationFrameId: null, pendingRestart: false,
        },
        right: {
          active: false, lastRemainingSec: 0, lastDurationSec: 0,
          activeUntilTimeMs: null,
          hideTimeoutId: null, resetTimeoutId: null, animationFrameId: null, pendingRestart: false,
        },
      };

      const getActiveProgressStartOffset = function (side) {
        return side === "left" ? leftActiveProgressStartOffset : rightActiveProgressStartOffset;
      };

      const getActiveProgressEndOffset = function (side) {
        return side === "left" ? activeProgressLength : -activeProgressLength;
      };

      const getActiveProgressOffset = function (side, activeRemainingSec, activeDurationSec) {
        const progressRatio = Math.max(0, Math.min(activeRemainingSec, activeDurationSec)) / activeDurationSec;
        const startOffset = getActiveProgressStartOffset(side);
        const endOffset = getActiveProgressEndOffset(side);
        return startOffset + (1 - progressRatio) * (endOffset - startOffset);
      };

      const getAdjustedActiveRemainingSec = function (state) {
        if (state.activeUntilTimeMs === null) {
          return state.lastRemainingSec;
        }

        return Math.max(0, (state.activeUntilTimeMs - Date.now()) / 1000);
      };

      const restartHubActiveAnimation = function (state, hubActiveCircle, side, activeRemainingSec, activeDurationSec) {
        if (state.animationFrameId !== null) {
          cancelAnimationFrame(state.animationFrameId);
          state.animationFrameId = null;
        }

        hubActiveCircle.stop(true, true);
        hubActiveCircle.css("transition", "none");
        hubActiveCircle.css("stroke-dashoffset", getActiveProgressOffset(side, activeRemainingSec, activeDurationSec));
        hubActiveCircle[0].getBoundingClientRect();

        state.animationFrameId = requestAnimationFrame(function () {
          hubActiveCircle.css("transition", `stroke-dashoffset ${activeRemainingSec * 1000}ms linear`);
          hubActiveCircle.css("stroke-dashoffset", getActiveProgressEndOffset(side));
          state.animationFrameId = null;
        });
      };

      const resetHubActiveAnimation = function (state, hubActiveCircle, side) {
        if (!state.active) {
          hubActiveCircle.stop(true, true);
          hubActiveCircle.css("transition", "");
          hubActiveCircle.css("stroke-dashoffset", getActiveProgressStartOffset(side));
        }
      };

      const cancelPendingHubActiveReset = function (state, hubActiveCircle) {
        if (state.hideTimeoutId !== null) {
          clearTimeout(state.hideTimeoutId);
          state.hideTimeoutId = null;
        }
        if (state.resetTimeoutId !== null) {
          clearTimeout(state.resetTimeoutId);
          state.resetTimeoutId = null;
          hubActiveCircle.off("transitionend.hubActiveReset");
        }
      };

      const scheduleHubActiveReset = function (state, hubActiveCircle, side) {
        const reset = function () {
          if (state.resetTimeoutId !== null) {
            clearTimeout(state.resetTimeoutId);
          }
          hubActiveCircle.off("transitionend.hubActiveReset");
          resetHubActiveAnimation(state, hubActiveCircle, side);
          state.resetTimeoutId = null;
        };

        hubActiveCircle.off("transitionend.hubActiveReset");
        hubActiveCircle.on("transitionend.hubActiveReset", function (event) {
          if (event.originalEvent.propertyName === "opacity") {
            reset();
          }
        });
        state.resetTimeoutId = setTimeout(reset, activeFadeTimeMs + 100);
      };

      return {
        restartPendingHubActiveIndicators: function () {
          $.each(hubActiveStateBySide, function (side, state) {
            const activeRemainingSec = getAdjustedActiveRemainingSec(state);
            if (!state.pendingRestart || !state.active || activeRemainingSec <= 0 || state.lastDurationSec <= 0) {
              return;
            }

            const hubActiveCircle = $(`#${side}HubActive svg .active-progress`);
            restartHubActiveAnimation(state, hubActiveCircle, side, activeRemainingSec, state.lastDurationSec);
            state.pendingRestart = false;
          });
        },

        updateHubActiveIndicator: function (side, activeRemainingSec, activeDurationSec) {
          const state = hubActiveStateBySide[side];
          const hubActiveDiv = $(`#${side}HubActive`);
          const hubActiveCircle = $(`#${side}HubActive svg .active-progress`);
          const hubActiveText = $(`#${side}HubActive svg text`);
          const wasActive = state.active;

          if (activeRemainingSec > 0 && activeDurationSec > 0) {
            const shouldRestartAnimation = !state.active ||
              activeRemainingSec > state.lastRemainingSec ||
              activeDurationSec !== state.lastDurationSec;

            cancelPendingHubActiveReset(state, hubActiveCircle);
            state.active = true;
            state.activeUntilTimeMs = Date.now() + activeRemainingSec * 1000;
            state.pendingRestart = getCurrentScreen() !== "match";
            hubActiveDiv.attr("data-active", true);
            hubActiveText.text(activeRemainingSec);

            if (shouldRestartAnimation) {
              if (state.pendingRestart) {
                hubActiveCircle.stop(true, true);
                hubActiveCircle.css("transition", "");
                hubActiveCircle.css("stroke-dashoffset", getActiveProgressOffset(side, activeRemainingSec, activeDurationSec));
              } else {
                restartHubActiveAnimation(state, hubActiveCircle, side, activeRemainingSec, activeDurationSec);
              }
            }
          } else {
            state.active = false;
            state.activeUntilTimeMs = null;
            state.pendingRestart = false;
            hubActiveText.text(activeRemainingSec);
            if (state.animationFrameId !== null) {
              cancelAnimationFrame(state.animationFrameId);
              state.animationFrameId = null;
            }
            if (wasActive) {
              state.hideTimeoutId = setTimeout(function () {
                hubActiveDiv.attr("data-active", false);
                scheduleHubActiveReset(state, hubActiveCircle, side);
                state.hideTimeoutId = null;
              }, activeDwellTimeMs);
            } else if (state.hideTimeoutId === null && state.resetTimeoutId === null) {
              resetHubActiveAnimation(state, hubActiveCircle, side);
              hubActiveDiv.attr("data-active", false);
            }
          }

          state.lastRemainingSec = activeRemainingSec;
          state.lastDurationSec = activeDurationSec;
        },
      };
    },
  };
})(window);
