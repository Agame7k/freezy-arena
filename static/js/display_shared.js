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
      this.applyEventInfo(data.Event, currentMatch, redSide, blueSide);
      return currentMatch;
    },

    // Shows the field name and conference information (multi-field and multi-conference events).
    applyEventInfo: function (event, match, redSide, blueSide) {
      if (!event) {
        return;
      }
      // Field name in a corner, so that viewers know which field the display belongs to.
      let badge = $("#fieldNameBadge");
      if (event.FieldName) {
        if (badge.length === 0) {
          badge = $('<div id="fieldNameBadge"></div>').css({
            position: "fixed", top: "0.5em", right: "0.8em", zIndex: 1000, padding: "0.1em 0.6em",
            background: "rgba(0, 0, 0, 0.7)", color: "#fff", fontSize: "1.6em", borderRadius: "0.3em",
            textTransform: "uppercase", fontFamily: "FuturaLTBold, sans-serif",
          });
          $("body").append(badge);
        }
        badge.text(event.FieldName).show();
      } else {
        badge.hide();
      }

      // Playoff alliances are labeled by conference and seed (e.g. N3).
      if (match.Type === matchTypePlayoff) {
        if (event.RedAllianceLabel) {
          $(`#${redSide}PlayoffAlliance`).text(event.RedAllianceLabel);
        }
        if (event.BlueAllianceLabel) {
          $(`#${blueSide}PlayoffAlliance`).text(event.BlueAllianceLabel);
        }
      }

      // A conference color bar under each team number.
      const teams = {
        [`${redSide}Team1`]: match.Red1, [`${redSide}Team2`]: match.Red2, [`${redSide}Team3`]: match.Red3,
        [`${blueSide}Team1`]: match.Blue1, [`${blueSide}Team2`]: match.Blue2, [`${blueSide}Team3`]: match.Blue3,
      };
      $.each(teams, function (elementId, teamId) {
        const conference = event.MultiConference && event.Conferences[event.TeamConferences[teamId]];
        $(`#${elementId}`).css("box-shadow", conference ? `inset 0 -0.25em 0 ${conference.Color}` : "");
      });
    },

    // Returns the text to show for a team's rank: the conference rank (e.g. "N4") in a multi-conference event, or
    // the overall rank otherwise, along with the previous rank to compare against for the rank change arrow.
    formatRank: function (event, teamId, ranking) {
      if (!ranking || ranking.Rank === 0) {
        return {text: "", rank: 0, previousRank: 0, overall: ""};
      }
      if (event && event.MultiConference && event.ConferenceRanks[teamId]) {
        const conference = event.Conferences[event.TeamConferences[teamId]];
        return {
          text: (conference ? conference.ShortName : "") + event.ConferenceRanks[teamId],
          rank: event.ConferenceRanks[teamId],
          previousRank: event.PreviousConfRanks[teamId] || 0,
          overall: "#" + ranking.Rank,
        };
      }
      return {text: ranking.Rank, rank: ranking.Rank, previousRank: ranking.PreviousRank, overall: ""};
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
