// Copyright 2026 Team 254. All Rights Reserved.
//
// Full-screen match intro shared by the audience and wall displays: the two alliances slam in from either side and
// meet at a diagonal seam, "VS" lands with a flash and a burst of particles, and each team's card slides in with its
// number scrambling into place. It holds there as its own screen until the operator moves on, at which point the sides
// split apart to uncover whatever comes next. The same stage can instead show a match's final result, with each side's
// score, the winner and any cards.

(function (window) {
  const easeIn = "cubic-bezier(0.16, 1, 0.3, 1)";
  const easeOut = "cubic-bezier(0.7, 0, 0.84, 0)";
  const easeMove = "cubic-bezier(0.65, 0, 0.35, 1)";
  const easeSlam = "cubic-bezier(0.5, 0, 0.75, 0)";
  const particleColors = ["#ff5571", "#4da3ff", "#ffd166", "#ffffff"];
  const cardSeverity = {"": 0, "yellow": 1, "red": 2, "dq": 3};
  const cardLabels = {yellow: "Yellow card", red: "Red card", dq: "Disqualified"};

  // The latest data for each of the two things the stage can show, and which one is currently up. The next match is
  // loaded as soon as a score is committed, so both have to be kept rather than drawn as they arrive.
  let introArgs = null;
  let resultArgs = null;
  let showing = null;

  // Runs the keyframes on every matched element (optionally staggered) and leaves each resting on the final frame.
  // Resolves when all are done; a wall-clock backstop finishes them if the browser stops painting frames, so the
  // intro can never stall a display's transition queue.
  const animate = function (target, keyframes, duration, easing, delay = 0, stagger = 0) {
    const finalFrame = {...keyframes[keyframes.length - 1]};
    return Promise.all($(target).toArray().map(function (element, i) {
      const totalDelay = delay + i * stagger;
      const animation = element.animate(keyframes, {duration, easing, delay: totalDelay, fill: "both"});
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

  const slideIn = function (fromX) {
    return [
      {opacity: 0, filter: "blur(6px)", transform: `translateX(${fromX}px)`},
      {opacity: 1, filter: "none", transform: "none"},
    ];
  };

  // Cycles random digits through each character of the element's number before settling each one, left to right.
  const scrambleNumber = function (element, delay, duration) {
    const text = String(element.dataset.number);
    const start = Date.now() + delay;
    const intervalId = setInterval(function () {
      const elapsed = Date.now() - start;
      if (elapsed < 0) {
        return;
      }
      const settled = Math.floor((elapsed / duration) * text.length);
      if (settled >= text.length) {
        element.textContent = text;
        clearInterval(intervalId);
        return;
      }
      let scrambled = text.slice(0, settled);
      for (let i = settled; i < text.length; i++) {
        scrambled += Math.floor(Math.random() * 10);
      }
      element.textContent = scrambled;
    }, 45);
  };

  // A burst of spinning outlined shapes flying out from the center of the screen.
  const burstParticles = function (canvas) {
    const ratio = window.devicePixelRatio || 1;
    const width = window.innerWidth;
    const height = window.innerHeight;
    canvas.width = width * ratio;
    canvas.height = height * ratio;
    const context = canvas.getContext("2d");
    context.scale(ratio, ratio);

    const particles = [];
    for (let i = 0; i < 160; i++) {
      const angle = Math.random() * Math.PI * 2;
      const speed = 6 + Math.random() * 18;
      particles.push({
        x: width / 2 + Math.cos(angle) * 150,
        y: height / 2 + Math.sin(angle) * 150,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed,
        size: 3 + Math.random() * 10,
        rotation: Math.random() * Math.PI,
        spin: (Math.random() - 0.5) * 0.3,
        sides: Math.random() < 0.5 ? 3 : 4,
        filled: Math.random() < 0.3,
        color: particleColors[i % particleColors.length],
        life: 1,
        decay: 0.008 + Math.random() * 0.014,
      });
    }

    const drawFrame = function () {
      context.clearRect(0, 0, width, height);
      let alive = false;
      particles.forEach(function (particle) {
        particle.x += particle.vx;
        particle.y += particle.vy;
        particle.vx *= 0.94;
        particle.vy *= 0.94;
        particle.rotation += particle.spin;
        particle.life -= particle.decay;
        if (particle.life <= 0) {
          return;
        }
        alive = true;
        context.save();
        context.globalAlpha = particle.life;
        context.translate(particle.x, particle.y);
        context.rotate(particle.rotation);
        context.beginPath();
        for (let side = 0; side < particle.sides; side++) {
          const pointAngle = (side / particle.sides) * Math.PI * 2;
          context.lineTo(Math.cos(pointAngle) * particle.size, Math.sin(pointAngle) * particle.size);
        }
        context.closePath();
        if (particle.filled) {
          context.fillStyle = particle.color;
          context.fill();
        } else {
          context.strokeStyle = particle.color;
          context.lineWidth = 1.5;
          context.stroke();
        }
        context.restore();
      });
      if (alive) {
        requestAnimationFrame(drawFrame);
      } else {
        context.clearRect(0, 0, width, height);
      }
    };
    requestAnimationFrame(drawFrame);
  };

  // Builds the intro's markup once and appends it to the page.
  const mount = function () {
    if ($("#matchIntro").length > 0) {
      return;
    }
    const intro = $("<div id='matchIntro'>").appendTo("body");
    ["Left", "Right"].forEach(function (side) {
      $(`<div class='match-intro-side' id='matchIntro${side}'>`)
        .append(`<div class='match-intro-alliance' id='matchIntro${side}Alliance'></div>`)
        .append(`<div class='match-intro-teams' id='matchIntro${side}Teams'></div>`)
        .appendTo(intro);
    });
    intro.append("<div id='matchIntroSeam'></div>");
    intro.append("<canvas id='matchIntroParticles'></canvas>");
    intro.append($("<div id='matchIntroEvent'>").text($("#eventMatchInfo > span").first().text()));
    intro.append("<div id='matchIntroVs'>VS</div>");
    intro.append("<div id='matchIntroMatchName'></div>");
    intro.append("<div id='matchIntroFlash'></div>");
  };

  // Clears out anything only the result view draws, so the stage can be reused for the intro.
  const clearResult = function () {
    $(".match-intro-score, .match-intro-pills, .match-intro-destination").remove();
    $(".match-intro-side").removeAttr("data-result");
  };

  const formatMatchName = function (match) {
    return match.LongName + (match.NameDetail ? " – " + match.NameDetail : "");
  };

  // Adds a team's row: avatar, number (with any card beside it) and whatever goes underneath.
  const addTeamRow = function (teams, teamId, getAvatarUrl) {
    const row = $("<div class='match-intro-team'>").appendTo(teams);
    $("<img class='match-intro-avatar'>").attr("src", getAvatarUrl(teamId)).on("error", function () {
      $(this).css("visibility", "hidden");
    }).appendTo(row);
    const info = $("<div class='match-intro-info'>").appendTo(row);
    // Any card sits beside the number rather than inside it, since the number's text gets scrambled.
    const numberRow = $("<div class='match-intro-number-row'>").appendTo(info);
    $("<div class='match-intro-number'>").attr("data-number", teamId).text(teamId).appendTo(numberRow);
    return {row: row, info: info, numberRow: numberRow};
  };

  const addCardGlyph = function (parent, card) {
    $("<div class='match-intro-yellow-card'>").attr({"data-card": card, "title": cardLabels[card]}).appendTo(parent);
  };

  const renderIntro = function () {
    mount();
    clearResult();
    $("#matchIntro").attr("data-view", "intro");
    $("#matchIntroVs").text("VS");
    if (introArgs === null) {
      return;
    }
    const {data, redSide, blueSide, getAvatarUrl} = introArgs;
    const match = data.Match;
    const isPlayoff = match.Type === matchTypePlayoff;
    [
      {
        side: redSide, color: "red", label: "Red Alliance", stations: ["R1", "R2", "R3"],
        teamIds: [match.Red1, match.Red2, match.Red3], playoffAlliance: match.PlayoffRedAlliance,
      },
      {
        side: blueSide, color: "blue", label: "Blue Alliance", stations: ["B1", "B2", "B3"],
        teamIds: [match.Blue1, match.Blue2, match.Blue3], playoffAlliance: match.PlayoffBlueAlliance,
      },
    ].forEach(function (alliance) {
      const prefix = alliance.side === "left" ? "#matchIntroLeft" : "#matchIntroRight";
      $(prefix).attr("data-alliance", alliance.color);
      const allianceLabel = $(`${prefix}Alliance`).empty();
      $("<span>").text(
        isPlayoff && alliance.playoffAlliance > 0 ? `Alliance ${alliance.playoffAlliance}` : alliance.label
      ).appendTo(allianceLabel);
      // In playoffs a yellow card belongs to the whole alliance, so it's shown once beside the alliance's name.
      const allianceCarded = alliance.stations.some(function (station) {
        return data.Teams[station] && data.Teams[station].YellowCard;
      });
      if (isPlayoff && allianceCarded) {
        $("<div class='match-intro-yellow-card' title='Yellow card'>").appendTo(allianceLabel);
      }

      const teams = $(`${prefix}Teams`).empty();
      alliance.teamIds.forEach(function (teamId, i) {
        if (!teamId) {
          return;
        }
        const team = data.Teams[alliance.stations[i]];
        const {info, numberRow} = addTeamRow(teams, teamId, getAvatarUrl);
        if (!isPlayoff && team && team.YellowCard) {
          addCardGlyph(numberRow, "yellow");
        }
        $("<div class='match-intro-nickname'>").text((team && team.Nickname) || `Team ${teamId}`).appendTo(info);
        const meta = $("<div class='match-intro-meta'>").appendTo(info);
        const rank = data.Rankings ? data.Rankings[teamId] : undefined;
        if (rank) {
          $("<span class='match-intro-rank'>").text(`Rank ${rank}`).appendTo(meta);
        }
        const location = team ? [team.City, team.StateProv, team.Country].filter(Boolean).join(", ") : "";
        if (location !== "") {
          $("<span class='match-intro-location'>").text(location).appendTo(meta);
        }
      });
    });
    $("#matchIntroMatchName").text(formatMatchName(match));
  };

  // Draws a match's final result: each side's score and verdict, the alliance's card in playoffs, and every team with
  // its own card and rank. Red-carded teams are struck through; in playoffs that's the whole alliance, backup included.
  const renderResult = function () {
    mount();
    clearResult();
    $("#matchIntro").attr("data-view", "result");
    $("#matchIntroVs").text("FINAL");
    if (resultArgs === null) {
      return;
    }
    const {data, redSide, blueSide, getAvatarUrl} = resultArgs;
    const match = data.Match;
    const isPlayoff = match.Type === matchTypePlayoff;
    [
      {
        side: redSide, color: "red", label: "Red Alliance", won: data.RedWon,
        teamIds: [match.Red1, match.Red2, match.Red3], offFieldTeamIds: data.RedOffFieldTeamIds || [],
        cards: data.RedCards || {}, rankings: data.RedRankings || {}, score: data.RedScoreSummary.Score,
        playoffAlliance: match.PlayoffRedAlliance, destination: data.RedDestination || "",
      },
      {
        side: blueSide, color: "blue", label: "Blue Alliance", won: data.BlueWon,
        teamIds: [match.Blue1, match.Blue2, match.Blue3], offFieldTeamIds: data.BlueOffFieldTeamIds || [],
        cards: data.BlueCards || {}, rankings: data.BlueRankings || {}, score: data.BlueScoreSummary.Score,
        playoffAlliance: match.PlayoffBlueAlliance, destination: data.BlueDestination || "",
      },
    ].forEach(function (alliance) {
      const prefix = alliance.side === "left" ? "#matchIntroLeft" : "#matchIntroRight";
      const isTie = !data.RedWon && !data.BlueWon;
      $(prefix).attr({"data-alliance": alliance.color, "data-result": isTie ? "tie" : alliance.won ? "won" : "lost"});

      let allianceCard = "";
      alliance.teamIds.forEach(function (teamId) {
        const card = alliance.cards[String(teamId)] || "";
        if ((cardSeverity[card] || 0) > cardSeverity[allianceCard]) {
          allianceCard = card;
        }
      });

      $("<div class='match-intro-score'>").attr("data-number", alliance.score).text(alliance.score)
        .insertBefore(`${prefix}Alliance`);
      const allianceLabel = $(`${prefix}Alliance`).empty();
      $("<span>").text(
        isPlayoff && alliance.playoffAlliance > 0 ? `Alliance ${alliance.playoffAlliance}` : alliance.label
      ).appendTo(allianceLabel);
      // The verdict and card get a row of their own under the name, which alone nearly fills the side's width.
      const pills = $("<div class='match-intro-pills'>").insertAfter(allianceLabel);
      if (isTie || alliance.won) {
        $("<span class='match-intro-verdict'>").text(isTie ? "Tie" : "Winner").appendTo(pills);
      }
      if (isPlayoff && allianceCard !== "") {
        $("<span class='match-intro-card-pill'>").attr("data-card", allianceCard).text(cardLabels[allianceCard])
          .appendTo(pills);
      }
      if (isPlayoff && alliance.destination !== "") {
        $("<div class='match-intro-destination'>").text(alliance.destination).insertAfter(pills);
      }

      const allianceStruck = isPlayoff && (allianceCard === "red" || allianceCard === "dq");
      const teams = $(`${prefix}Teams`).empty();
      alliance.teamIds.concat(alliance.offFieldTeamIds.slice(0, 1)).forEach(function (teamId, i) {
        if (!teamId) {
          return;
        }
        const card = i < 3 ? alliance.cards[String(teamId)] || "" : "";
        const {row, info, numberRow} = addTeamRow(teams, teamId, getAvatarUrl);
        row.attr("data-red-card", allianceStruck || card === "red" || card === "dq");
        if (!isPlayoff && card !== "") {
          addCardGlyph(numberRow, card);
        }
        const meta = $("<div class='match-intro-meta'>").appendTo(info);
        const ranking = alliance.rankings[teamId];
        if (!isPlayoff && ranking && ranking.Rank) {
          $("<span class='match-intro-rank'>").text(`Rank ${ranking.Rank}`).appendTo(meta);
        }
        if (i >= 3) {
          $("<span class='match-intro-location'>").text("Backup").appendTo(meta);
        }
      });
    });
    $("#matchIntroMatchName").text(formatMatchName(match));
  };

  window.MatchIntro = {
    // Takes the teams for the intro from a matchLoad websocket message. Redraws straight away unless a result is on
    // screen, which must stay put even though the next match has already been loaded.
    build: function (data, redSide, blueSide, getAvatarUrl) {
      introArgs = {data, redSide, blueSide, getAvatarUrl};
      if (showing !== "result") {
        renderIntro();
      }
    },

    // Takes a match's final result from a scorePosted websocket message, for showing with play("result").
    buildResult: function (data, redSide, blueSide, getAvatarUrl) {
      resultArgs = {data, redSide, blueSide, getAvatarUrl};
      if (showing === "result") {
        renderResult();
      }
    },

    // Brings the stage in, resolving once everything has landed. view is "intro" (the default) or "result"; sound may
    // supply whoosh() and slam() cues.
    play: async function (sound = {}, view = "intro") {
      showing = view;
      if (view === "result") {
        renderResult();
      } else {
        renderIntro();
      }
      $("#matchIntro").attr("data-playing", "").show();
      if (sound.whoosh) {
        sound.whoosh();
      }

      // The two sides slam in from their outer edges.
      await Promise.all([
        animate("#matchIntroLeft", [{transform: "translateX(-105%)"}, {transform: "translateX(0%)"}], 550, easeSlam),
        animate("#matchIntroRight", [{transform: "translateX(105%)"}, {transform: "translateX(0%)"}], 550, easeSlam),
      ]);

      // Impact: flash, shake, seam and "VS", with a burst of particles in both alliance colors.
      if (sound.slam) {
        sound.slam();
      }
      burstParticles(document.getElementById("matchIntroParticles"));
      document.getElementById("matchIntro").animate([
        {transform: "translate(18px, -6px)"},
        {transform: "translate(-14px, 5px)"},
        {transform: "translate(9px, -3px)"},
        {transform: "translate(-5px, 2px)"},
        {transform: "translate(0px, 0px)"},
      ], {duration: 420, easing: "linear"});
      animate("#matchIntroFlash", [{opacity: 0.9}, {opacity: 0}], 550, easeIn);
      animate("#matchIntroSeam", [{opacity: 0, transform: "scaleY(0)"}, {opacity: 1, transform: "scaleY(1)"}], 500,
        easeIn);
      animate("#matchIntroVs", [
        {opacity: 0, transform: "translate(-50%, -50%) scale(3.2) rotate(-8deg)", filter: "blur(24px)"},
        {opacity: 1, transform: "translate(-50%, -50%)", filter: "none"},
      ], 600, easeIn);
      animate("#matchIntroEvent", [
        {opacity: 0, transform: "translate(-50%, -4vh)"},
        {opacity: 1, transform: "translate(-50%, 0px)"},
      ], 650, easeIn, 250);
      animate("#matchIntroMatchName", [
        {opacity: 0, transform: "translate(-50%, 4vh)"},
        {opacity: 1, transform: "translate(-50%, 0px)"},
      ], 650, easeIn, 350);
      animate(".match-intro-alliance", [
        {opacity: 0, letterSpacing: "0.9em"},
        {opacity: 1, letterSpacing: "0.3em"},
      ], 800, easeIn, 200);
      animate(".match-intro-destination", [{opacity: 0}, {opacity: 1}], 600, easeIn, 500);
      $(".match-intro-score").each(function () {
        scrambleNumber(this, 150, 900);
      });
      animate(".match-intro-score", [
        {opacity: 0, transform: "scale(1.8)", filter: "blur(12px)"},
        {opacity: 1, transform: "none", filter: "none"},
      ], 700, easeIn, 100);

      // Team cards glide in from each outer edge in turn, their numbers scrambling into place as they land.
      $(".match-intro-team").each(function () {
        scrambleNumber($(this).find(".match-intro-number")[0], 350 + $(this).index() * 140, 800);
      });
      await Promise.all([
        animate("#matchIntroLeft .match-intro-team", slideIn(-90), 700, easeIn, 350, 140),
        animate("#matchIntroRight .match-intro-team", slideIn(90), 700, easeIn, 350, 140),
        // Any yellow cards flip in once their teams have landed.
        animate(".match-intro-yellow-card", [
          {opacity: 0, transform: "rotate(-10deg) rotateY(90deg) scale(1.8)"},
          {opacity: 1, transform: "rotate(-10deg)"},
        ], 550, easeIn, 1100, 120),
        animate(".match-intro-verdict, .match-intro-card-pill", [
          {opacity: 0, transform: "scale(0.4)"},
          {opacity: 1, transform: "none"},
        ], 600, easeIn, 1000, 120),
      ]);
    },

    // Splits the two sides apart, with "VS" diving up or down towards wherever the display's overlay sits.
    leave: async function (diveUp, sound = {}) {
      if (sound.whoosh) {
        sound.whoosh();
      }
      animate(
        ".match-intro-team, .match-intro-alliance, .match-intro-score, .match-intro-destination, #matchIntroEvent, " +
        "#matchIntroMatchName, #matchIntroSeam",
        [{opacity: 0}], 250, easeOut
      );
      await Promise.all([
        animate("#matchIntroVs", [
          {opacity: 0, transform: `translate(-50%, calc(-50% + ${diveUp ? "-38vh" : "38vh"})) scale(0.25)`,
            filter: "blur(6px)"},
        ], 550, easeOut, 100),
        animate("#matchIntroLeft", [{transform: "translateX(-105%)"}], 750, easeMove, 150),
        animate("#matchIntroRight", [{transform: "translateX(105%)"}], 750, easeMove, 150),
      ]);
      $("#matchIntro").removeAttr("data-playing").hide();
      showing = null;
    },
  };
})(window);
