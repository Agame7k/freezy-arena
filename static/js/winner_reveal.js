// Copyright 2026 Team 254. All Rights Reserved.
//
// The winner reveal, shared by the audience and wall displays so both play the exact same sequence in step. The wall
// plays it muted, since the audience display carries the sound.
//
// Nothing about the result shows until the very end. Two light trails, one in each alliance's color, tear in from
// opposite edges of a dark grid and race each other with hard right-angle turns, cutting across each other's path and
// trading the lead back and forth. Their paths mirror each other exactly, so neither is ever really ahead. They climb
// onto a dark orb in the middle of the screen and wind around it, faster and tighter, as it charges up and the view
// closes in. The orb then sucks everything in, the sound drops out for a heartbeat, and it detonates: the blast wave
// uncovers the winner's color filling the screen as the result stamps down with the score, the ranking points (or where
// the winner advances to, in playoffs) and confetti. A tie blows open into both colors at once. A skewed wipe then
// carries it all off to uncover the score card.

(function (window) {
  const ease = {
    in: "cubic-bezier(0.16, 1, 0.3, 1)",
    out: "cubic-bezier(0.7, 0, 0.84, 0)",
    move: "cubic-bezier(0.65, 0, 0.35, 1)",
    pop: "cubic-bezier(0.34, 1.56, 0.64, 1)",
  };
  // Accelerates into a hit, for text that stamps down onto the screen.
  const easeStampIn = "cubic-bezier(0.55, 0, 1, 0.45)";
  const cardSeverity = {"": 0, "yellow": 1, "red": 2, "dq": 3};
  const cardLabels = {yellow: "Yellow card", red: "Red card", dq: "Disqualified"};
  const accentColors = {red: "#ff5571", blue: "#4da3ff"};
  const burstColors = {
    red: ["#ff5571", "#ff9aae", "#ffffff", "#ffd166"],
    blue: ["#4da3ff", "#a8d2ff", "#ffffff", "#ffd166"],
    tie: ["#ff5571", "#4da3ff", "#ffffff", "#ffd166"],
  };
  const confettiColors = {
    red: ["#ff5571", "#c22a4b", "#ff9aae", "#ffffff", "#ffd166"],
    blue: ["#4da3ff", "#2466c2", "#a8d2ff", "#ffffff", "#ffd166"],
    tie: ["#ff5571", "#4da3ff", "#ffd166", "#ffffff"],
  };
  const championConfettiColors = ["#ffd166", "#ffe8a3", "#ffffff", "#e0a526"];
  // The timeline, in milliseconds from the start: the race, the two trails winding around the orb, the orb pulling
  // everything in, and a held breath before it goes off.
  const timeline = {
    raceStart: 400,
    raceMs: 3600,
    swirlMs: 2400,
    implodeMs: 450,
    breathMs: 550,
  };
  timeline.swirlStart = timeline.raceStart + timeline.raceMs;
  timeline.implodeStart = timeline.swirlStart + timeline.swirlMs;
  timeline.explodeAt = timeline.implodeStart + timeline.implodeMs + timeline.breathMs;
  // The ranking points an alliance can earn in a qualification match: three for a win plus three bonuses.
  const maxRankingPoints = 6;
  // Whether this display keeps the reveal silent; set on each play().
  let muted = false;

  const wait = function (ms) {
    return new Promise(function (resolve) {
      setTimeout(resolve, ms);
    });
  };

  // Writes a keyframe's values to the element's inline style. Custom properties need setProperty; the rest are set
  // directly so camel-cased names like clipPath work.
  const applyFrame = function (element, frame) {
    Object.keys(frame).forEach(function (property) {
      if (property.startsWith("--")) {
        element.style.setProperty(property, frame[property]);
      } else {
        element.style[property] = frame[property];
      }
    });
  };

  // Runs the keyframes on every matched element (optionally staggered) and leaves each resting on the final frame.
  // Resolves when all are done; a wall-clock backstop finishes them if the browser stops painting frames, so the reveal
  // can never stall a display's transition queue.
  const animate = function (target, keyframes, duration, easing, delay = 0, stagger = 0) {
    const finalFrame = {...keyframes[keyframes.length - 1]};
    delete finalFrame.offset;
    delete finalFrame.easing;
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
        applyFrame(element, finalFrame);
        animation.cancel();
      }, function () {
        clearTimeout(backstopId);
      });
    }));
  };

  // Text that drops onto the screen from larger than life, overshoots a touch small on impact and settles.
  const stamp = function (fromScale) {
    return [
      {opacity: 0, transform: `scale(${fromScale})`, easing: easeStampIn},
      {opacity: 1, transform: "scale(0.96)", offset: 0.6, easing: ease.in},
      {opacity: 1, transform: "none"},
    ];
  };

  // Jolts the whole stage, as if hit. amplitude is the size of the first kick in pixels; the rest die away from it.
  const shake = function (amplitude, duration) {
    const kicks = [[0, 0], [-1, 0.6], [0.8, -0.7], [-0.55, -0.4], [0.35, 0.45], [-0.15, 0.15], [0, 0]];
    return animate("#winnerRevealStage", kicks.map(function ([x, y]) {
      return {transform: `translate(${x * amplitude}px, ${y * amplitude}px)`};
    }), duration, "linear");
  };

  // Returns the most serious card given to any of the listed teams.
  const mostSeriousCard = function (cards, teamIds) {
    let card = "";
    teamIds.forEach(function (teamId) {
      const teamCard = cards[String(teamId)] || "";
      if ((cardSeverity[teamCard] || 0) > cardSeverity[card]) {
        card = teamCard;
      }
    });
    return card;
  };

  // Lists the cards to show under the result: one per carded alliance in playoffs, one per carded team otherwise.
  const buildRevealCards = function (data, isPlayoff) {
    const revealCards = [];
    [
      ["Red", data.RedCards || {}, [data.Match.Red1, data.Match.Red2, data.Match.Red3]],
      ["Blue", data.BlueCards || {}, [data.Match.Blue1, data.Match.Blue2, data.Match.Blue3]],
    ].forEach(function ([color, cards, teamIds]) {
      if (isPlayoff) {
        const allianceCard = mostSeriousCard(cards, teamIds);
        if (allianceCard !== "") {
          revealCards.push({card: allianceCard, label: `${color} alliance · ${cardLabels[allianceCard]}`});
        }
        return;
      }
      $.each(cards, function (teamId, card) {
        if (cardLabels[card]) {
          revealCards.push({card: card, label: `${teamId} · ${cardLabels[card]}`});
        }
      });
    });
    return revealCards;
  };

  // Builds the reveal's markup once and adds it to the page.
  const mount = function () {
    if ($("#winnerReveal").length > 0) {
      return;
    }
    $("body").prepend(`
      <div id="winnerReveal">
        <div id="winnerRevealStage">
          <div id="winnerRevealWorld">
            <div class="reveal-side" id="winnerRevealLeft"></div>
            <div class="reveal-side" id="winnerRevealRight"></div>
            <div id="winnerRevealRays"></div>
            <div id="winnerRevealSeam">
              <div class="reveal-seam-glow"></div>
              <div class="reveal-seam-core"></div>
            </div>
          </div>
          <canvas id="winnerRevealRace"></canvas>
          <div class="reveal-shockwave"></div>
          <div class="reveal-shockwave" data-ring="gold"></div>
          <div id="winnerRevealHeader"><span></span></div>
          <div id="winnerRevealResult">
            <div id="winnerRevealTitle"></div>
            <div id="winnerRevealVerdict"></div>
            <div id="winnerRevealScore">
              <span id="winnerRevealLeftScore"></span>
              <span class="winner-reveal-score-divider"></span>
              <span id="winnerRevealRightScore"></span>
            </div>
            <div id="winnerRevealStandings"></div>
            <div id="winnerRevealTiebreak"></div>
            <div id="winnerRevealCards"></div>
          </div>
          <div id="winnerRevealFlash"></div>
        </div>
        <div id="winnerRevealWipe"></div>
      </div>
    `);
  };

  // Soundtrack for the winner reveal, synthesized live with the Web Audio API so there are no audio files to manage and
  // every cue lands exactly on its visual beat. If the browser refuses to start audio, every cue quietly does nothing
  // and the reveal plays silently.
  const revealSound = (function () {
    let context = null;
    let output = null;
    let reverb = null;
    let noise = null;
    // Sustained voices (the tension bed and the orb's whir), cut off together by hush() or impact().
    let sustainedVoices = [];

    // Builds the shared signal chain on first use: everything feeds a master gain into a compressor (so stacked cues
    // can't clip), with a synthetic reverb (a decaying burst of noise used as the impulse response) as a send.
    const setUp = function () {
      const AudioContextClass = window.AudioContext || window.webkitAudioContext;
      if (!AudioContextClass) {
        return false;
      }
      context = new AudioContextClass();

      const compressor = context.createDynamicsCompressor();
      compressor.threshold.value = -14;
      compressor.ratio.value = 6;
      compressor.connect(context.destination);
      output = context.createGain();
      output.gain.value = 0.85;
      output.connect(compressor);

      const impulseLength = Math.floor(context.sampleRate * 2.6);
      const impulse = context.createBuffer(2, impulseLength, context.sampleRate);
      for (let channel = 0; channel < 2; channel++) {
        const samples = impulse.getChannelData(channel);
        for (let i = 0; i < impulseLength; i++) {
          samples[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / impulseLength, 3.5);
        }
      }
      reverb = context.createConvolver();
      reverb.buffer = impulse;
      const reverbReturn = context.createGain();
      reverbReturn.gain.value = 0.4;
      reverb.connect(reverbReturn);
      reverbReturn.connect(output);

      const noiseLength = context.sampleRate * 2;
      noise = context.createBuffer(1, noiseLength, context.sampleRate);
      const noiseSamples = noise.getChannelData(0);
      for (let i = 0; i < noiseLength; i++) {
        noiseSamples[i] = Math.random() * 2 - 1;
      }
      return true;
    };

    const ready = function () {
      return context !== null && context.state === "running";
    };

    const oscillator = function (type, frequency) {
      const node = context.createOscillator();
      node.type = type;
      node.frequency.value = frequency;
      return node;
    };

    const noiseSource = function () {
      const node = context.createBufferSource();
      node.buffer = noise;
      node.loop = true;
      return node;
    };

    // Plays a source through a percussive envelope (fast attack, exponential release) into the dry mix plus a share of
    // reverb, then stops it once it has decayed. Returns the envelope's gain node.
    const hit = function (source, peak, attack, release, wet, when = context.currentTime, input = source) {
      const envelope = context.createGain();
      envelope.gain.setValueAtTime(0.0001, when);
      envelope.gain.exponentialRampToValueAtTime(peak, when + attack);
      envelope.gain.exponentialRampToValueAtTime(0.0001, when + attack + release);
      input.connect(envelope);
      envelope.connect(output);
      if (wet > 0) {
        const send = context.createGain();
        send.gain.value = wet;
        envelope.connect(send);
        send.connect(reverb);
      }
      source.start(when);
      source.stop(when + attack + release + 0.05);
      return envelope;
    };

    // Starts a source swelling up to the given level over the given number of seconds, held until it's cut off.
    const sustain = function (source, peak, duration, input = source) {
      const now = context.currentTime;
      const envelope = context.createGain();
      envelope.gain.setValueAtTime(0.0001, now);
      envelope.gain.exponentialRampToValueAtTime(peak, now + duration);
      input.connect(envelope);
      envelope.connect(output);
      source.start(now);
      sustainedVoices.push({source: source, envelope: envelope});
    };

    // Fades out and stops every sustained voice over the given number of seconds.
    const release = function (fadeSec) {
      const now = context.currentTime;
      sustainedVoices.forEach(function (voice) {
        voice.envelope.gain.cancelScheduledValues(now);
        voice.envelope.gain.setValueAtTime(Math.max(voice.envelope.gain.value, 0.0001), now);
        voice.envelope.gain.exponentialRampToValueAtTime(0.0001, now + fadeSec);
        voice.source.stop(now + fadeSec + 0.02);
      });
      sustainedVoices = [];
    };

    // Band-passed noise swept up and back down: an airy "whoosh" lasting the given number of seconds.
    const whoosh = function (duration, delaySec = 0, peak = 0.35) {
      if (!ready()) {
        return;
      }
      const when = context.currentTime + delaySec;
      const source = noiseSource();
      const filter = context.createBiquadFilter();
      filter.type = "bandpass";
      filter.Q.value = 1.4;
      filter.frequency.setValueAtTime(350, when);
      filter.frequency.exponentialRampToValueAtTime(4200, when + duration * 0.55);
      filter.frequency.exponentialRampToValueAtTime(900, when + duration);
      source.connect(filter);
      hit(source, peak, duration * 0.55, duration * 0.45, 0.35, when, filter);
    };

    return {
      // Starts (or wakes) the audio engine. Resolves once it's running, or after a short grace period if the browser
      // won't allow it, so callers can await it without risking a stall.
      start: function () {
        if (muted || (context === null && !setUp())) {
          return Promise.resolve();
        }
        if (context.state === "running") {
          return Promise.resolve();
        }
        return Promise.race([context.resume().catch(function () {}), wait(250)]);
      },

      // Builds tension for the given number of seconds: a low drone swelling up, a noise riser sweeping upward and a
      // thin tone gliding up an octave and a half.
      tension: function (duration) {
        if (!ready()) {
          return;
        }
        const end = context.currentTime + duration;
        sustain(oscillator("sine", 55), 0.3, duration);
        sustain(oscillator("sine", 55.6), 0.2, duration);

        const riser = noiseSource();
        const riserFilter = context.createBiquadFilter();
        riserFilter.type = "bandpass";
        riserFilter.Q.value = 5;
        riserFilter.frequency.setValueAtTime(250, context.currentTime);
        riserFilter.frequency.exponentialRampToValueAtTime(5000, end);
        riser.connect(riserFilter);
        sustain(riser, 0.18, duration, riserFilter);

        const glide = oscillator("triangle", 220);
        glide.frequency.exponentialRampToValueAtTime(660, end);
        sustain(glide, 0.04, duration);
      },

      whoosh: whoosh,

      // A light trail snapping round a corner: a quick falling zap.
      zip: function () {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        const zap = oscillator("sawtooth", 1400);
        zap.frequency.setValueAtTime(1400, now);
        zap.frequency.exponentialRampToValueAtTime(260, now + 0.1);
        const zapFilter = context.createBiquadFilter();
        zapFilter.type = "lowpass";
        zapFilter.frequency.value = 2500;
        zap.connect(zapFilter);
        hit(zap, 0.07, 0.003, 0.11, 0.2, now, zapFilter);
      },

      // The orb winding up as the trails spin around it: a whir climbing in pitch and volume over the given number of
      // seconds.
      swirl: function (duration) {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        const whir = oscillator("sawtooth", 70);
        whir.frequency.exponentialRampToValueAtTime(700, now + duration);
        const whirFilter = context.createBiquadFilter();
        whirFilter.type = "bandpass";
        whirFilter.Q.value = 4;
        whirFilter.frequency.setValueAtTime(250, now);
        whirFilter.frequency.exponentialRampToValueAtTime(2600, now + duration);
        whir.connect(whirFilter);
        sustain(whir, 0.16, duration, whirFilter);
      },

      // The orb pulling everything in: the sustained sound cuts away under a rush of air sweeping downward.
      implode: function (duration) {
        if (!ready()) {
          return;
        }
        release(duration);
        const now = context.currentTime;
        const rush = noiseSource();
        const rushFilter = context.createBiquadFilter();
        rushFilter.type = "lowpass";
        rushFilter.frequency.setValueAtTime(7000, now);
        rushFilter.frequency.exponentialRampToValueAtTime(150, now + duration);
        rush.connect(rushFilter);
        hit(rush, 0.3, duration * 0.8, duration * 0.2, 0.3, now, rushFilter);
      },

      // A single heartbeat: two low thumps, the second a little softer.
      heartbeat: function () {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        [{at: 0, pitch: 64, peak: 1.0}, {at: 0.17, pitch: 56, peak: 0.7}].forEach(function (beat) {
          const body = oscillator("sine", beat.pitch);
          body.frequency.setValueAtTime(beat.pitch, now + beat.at);
          body.frequency.exponentialRampToValueAtTime(34, now + beat.at + 0.2);
          hit(body, beat.peak, 0.006, 0.24, 0.1, now + beat.at);
        });
      },

      // The payoff: fires a sub drop, a bright crack and a wide shimmering chord all at once.
      impact: function (winner) {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        release(0.08);

        const sub = oscillator("sine", 120);
        sub.frequency.exponentialRampToValueAtTime(36, now + 0.7);
        hit(sub, 1.0, 0.005, 1.5, 0.15);

        const crack = noiseSource();
        const crackFilter = context.createBiquadFilter();
        crackFilter.type = "highpass";
        crackFilter.frequency.value = 1400;
        crack.connect(crackFilter);
        hit(crack, 0.45, 0.002, 0.35, 0.7, now, crackFilter);

        // A major chord for a win; a suspended, unresolved one for a tie.
        const chord = winner === "tie" ? [220, 293.66, 329.63, 440, 587.33] : [220, 277.18, 329.63, 440, 554.37];
        const chordFilter = context.createBiquadFilter();
        chordFilter.type = "lowpass";
        chordFilter.Q.value = 2;
        chordFilter.frequency.setValueAtTime(7000, now);
        chordFilter.frequency.exponentialRampToValueAtTime(700, now + 2.6);
        const chordBus = context.createGain();
        chordBus.connect(chordFilter);
        chord.forEach(function (frequency) {
          [-7, 7].forEach(function (detuneCents) {
            const voice = oscillator("sawtooth", frequency);
            voice.detune.value = detuneCents;
            voice.connect(chordBus);
            voice.start(now);
            voice.stop(now + 3.2);
          });
        });
        const chordEnvelope = context.createGain();
        chordEnvelope.gain.setValueAtTime(0.0001, now);
        chordEnvelope.gain.exponentialRampToValueAtTime(0.09, now + 0.01);
        chordEnvelope.gain.exponentialRampToValueAtTime(0.0001, now + 3.1);
        chordFilter.connect(chordEnvelope);
        chordEnvelope.connect(output);
        const chordSend = context.createGain();
        chordSend.gain.value = 0.6;
        chordEnvelope.connect(chordSend);
        chordSend.connect(reverb);

        [1760, 2637].forEach(function (frequency, i) {
          hit(oscillator("sine", frequency), 0.035, 0.01, 2.2, 0.9, now + 0.03 * i);
        });
      },

      // A second, heavier thud under the verdict landing.
      slam: function () {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        const sub = oscillator("sine", 90);
        sub.frequency.exponentialRampToValueAtTime(40, now + 0.4);
        hit(sub, 0.8, 0.004, 0.9, 0.2);

        const body = oscillator("sawtooth", 110);
        const bodyFilter = context.createBiquadFilter();
        bodyFilter.type = "lowpass";
        bodyFilter.frequency.value = 700;
        body.connect(bodyFilter);
        hit(body, 0.18, 0.004, 0.5, 0.3, now, bodyFilter);
      },
    };
  })();

  // The light race, drawn on its own canvas and driven entirely by the clock, so every frame is worked out from how long
  // it has been since the start and a dropped frame never throws the timing off. leftColor and rightColor are the
  // alliances on each side of the screen; explode() is handed the colors to burst into once the winner is known.
  const createRace = function (canvas, leftColor, rightColor) {
    const ratio = window.devicePixelRatio || 1;
    const width = window.innerWidth;
    const height = window.innerHeight;
    canvas.width = width * ratio;
    canvas.height = height * ratio;
    const context = canvas.getContext("2d");

    const orb = {x: width / 2, y: height * 0.47, radius: height * 0.09};
    const orbitRadius = orb.radius * 1.5;
    // The orbits are circles tipped most of the way away from the viewer, so they read as rings around a ball.
    const inclination = 1.2;
    const orbitTilt = 0.6;
    const orbitPoint = function (theta, tilt, radius) {
      const x = Math.cos(theta) * radius;
      const y = Math.sin(theta) * radius * Math.cos(inclination);
      const z = Math.sin(theta) * radius * Math.sin(inclination);
      return {
        x: orb.x + x * Math.cos(tilt) - y * Math.sin(tilt),
        y: orb.y + x * Math.sin(tilt) + y * Math.cos(tilt),
        z: z,
      };
    };

    // Each trail's route to the orb: in from its own edge, hard turns through the other's half and back, then onto the
    // orb where its orbit begins. The two are mirror images, so they're always the same distance from the finish.
    const leftOrbitStart = orbitPoint(Math.PI, orbitTilt, orbitRadius);
    const route = function (mirror) {
      const x = function (fraction) {
        return mirror ? width * (1 - fraction) : width * fraction;
      };
      const lane = mirror ? 0.07 : 0;
      return [
        {x: mirror ? width + 40 : -40, y: height * (0.7 + lane)},
        {x: x(0.66), y: height * (0.7 + lane)},
        {x: x(0.66), y: height * (0.26 - lane)},
        {x: x(0.4), y: height * (0.26 - lane)},
        {x: x(0.4), y: leftOrbitStart.y},
        {x: mirror ? width - leftOrbitStart.x : leftOrbitStart.x, y: leftOrbitStart.y},
      ];
    };
    const routes = [route(false), route(true)].map(function (points) {
      const lengths = [0];
      for (let i = 1; i < points.length; i++) {
        lengths.push(lengths[i - 1] + Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y));
      }
      return {points: points, lengths: lengths, total: lengths[lengths.length - 1], lastCorner: 0};
    });
    const colors = [accentColors[leftColor], accentColors[rightColor]];

    // Where a trail is when the given fraction of the way along its route, and every corner it has turned so far.
    const alongRoute = function (routeInfo, fraction) {
      const distance = fraction * routeInfo.total;
      const passed = [routeInfo.points[0]];
      for (let i = 1; i < routeInfo.points.length; i++) {
        if (distance >= routeInfo.lengths[i]) {
          passed.push(routeInfo.points[i]);
          continue;
        }
        const segment = (distance - routeInfo.lengths[i - 1]) / (routeInfo.lengths[i] - routeInfo.lengths[i - 1]);
        const from = routeInfo.points[i - 1];
        const to = routeInfo.points[i];
        passed.push({x: from.x + (to.x - from.x) * segment, y: from.y + (to.y - from.y) * segment});
        return {trail: passed, corner: i - 1};
      }
      return {trail: passed, corner: routeInfo.points.length - 1};
    };

    // How far round its orbit a trail has wound after the given time winding: slow to start, then faster and faster.
    const swirlAngle = function (seconds) {
      const startTurns = 0.9;
      const endTurns = 4.5;
      const duration = timeline.swirlMs / 1000;
      const clamped = Math.min(seconds, duration);
      let turns = startTurns * clamped + ((endTurns - startTurns) * clamped * clamped) / (2 * duration);
      if (seconds > duration) {
        turns += endTurns * (seconds - duration);
      }
      return turns * 2 * Math.PI;
    };

    // One trail's position on its orbit, winding the opposite way to the other so the two mirror each other.
    const orbitPosition = function (index, seconds, radius) {
      const angle = swirlAngle(seconds);
      const precession = 0.35 * seconds;
      return index === 0 ?
        orbitPoint(Math.PI + angle, orbitTilt + precession, radius) :
        orbitPoint(-angle, -orbitTilt - precession, radius);
    };

    // Line widths are designed at 1080p and scaled to the screen.
    const scale = height / 1080;

    // A trail of light: a wide, faint halo and a narrower band in the alliance's color around a white-hot core.
    const strokeGlow = function (points, color, alpha) {
      if (points.length < 2 || alpha <= 0) {
        return;
      }
      [[26, 0.14, color], [11, 0.45, color], [4, 1, "#ffffff"]].forEach(function ([lineWidth, strength, stroke]) {
        context.globalAlpha = alpha * strength;
        context.strokeStyle = stroke;
        context.lineWidth = lineWidth * scale;
        context.beginPath();
        context.moveTo(points[0].x, points[0].y);
        points.slice(1).forEach(function (point) {
          context.lineTo(point.x, point.y);
        });
        context.stroke();
      });
    };

    const drawHead = function (point, color, alpha) {
      context.globalAlpha = alpha * 0.4;
      context.fillStyle = color;
      context.beginPath();
      context.arc(point.x, point.y, 24 * scale, 0, Math.PI * 2);
      context.fill();
      context.globalAlpha = alpha;
      context.fillStyle = "#ffffff";
      context.beginPath();
      context.arc(point.x, point.y, 8 * scale, 0, Math.PI * 2);
      context.fill();
    };

    const drawGrid = function (alpha, elapsedMs) {
      const spacing = 64;
      const offset = (elapsedMs * 0.02) % spacing;
      context.globalAlpha = alpha;
      context.strokeStyle = "#9fb3cc";
      context.lineWidth = 1;
      context.beginPath();
      for (let x = -spacing; x < width + spacing; x += spacing) {
        context.moveTo(x + offset, 0);
        context.lineTo(x + offset, height);
      }
      for (let y = -spacing; y < height + spacing; y += spacing) {
        context.moveTo(0, y + offset);
        context.lineTo(width, y + offset);
      }
      context.stroke();
    };

    // The orb: a dark ball with a rim lit in both alliances' colors, glowing white-gold as it charges (0 to 1, and past
    // 1 as it collapses).
    const drawOrb = function (radius, charge, flicker) {
      const glowRadius = radius * (1.6 + 1.8 * charge);
      const glow = context.createRadialGradient(orb.x, orb.y, radius * 0.6, orb.x, orb.y, glowRadius);
      glow.addColorStop(0, `rgba(255, 226, 160, ${Math.min(0.9, 0.15 + 0.6 * charge) * flicker})`);
      glow.addColorStop(1, "rgba(255, 226, 160, 0)");
      context.globalAlpha = 1;
      context.fillStyle = glow;
      context.beginPath();
      context.arc(orb.x, orb.y, glowRadius, 0, Math.PI * 2);
      context.fill();

      const core = Math.min(1, charge);
      const body = context.createRadialGradient(orb.x - radius * 0.3, orb.y - radius * 0.35, radius * 0.05,
        orb.x, orb.y, radius);
      body.addColorStop(0, `rgb(${Math.round(60 + 195 * core)}, ${Math.round(70 + 180 * core)}, ${Math.round(90 + 130 * core)})`);
      body.addColorStop(0.7, `rgb(${Math.round(14 + 120 * core)}, ${Math.round(22 + 100 * core)}, ${Math.round(40 + 60 * core)})`);
      body.addColorStop(1, "#060b16");
      context.fillStyle = body;
      context.beginPath();
      context.arc(orb.x, orb.y, radius, 0, Math.PI * 2);
      context.fill();

      const rim = context.createLinearGradient(orb.x - radius, orb.y, orb.x + radius, orb.y);
      rim.addColorStop(0, colors[0]);
      rim.addColorStop(1, colors[1]);
      context.strokeStyle = rim;
      context.lineWidth = Math.max(2, radius * 0.06);
      context.globalAlpha = 0.5 + 0.5 * core;
      context.stroke();
    };

    let running = false;
    let startTime = 0;
    let explodeTime = null;
    let particles = [];
    let lastFrameTime = 0;
    // Called whenever a trail snaps round a corner, for the sound.
    let onCorner = function () {};

    const drawFrame = function (now) {
      if (!running) {
        context.setTransform(1, 0, 0, 1, 0, 0);
        context.clearRect(0, 0, canvas.width, canvas.height);
        return;
      }
      const elapsed = now - startTime;
      const dt = Math.min(3, (now - lastFrameTime) / (1000 / 60));
      lastFrameTime = now;
      const raceFraction = Math.max(0, Math.min(1, (elapsed - timeline.raceStart) / timeline.raceMs));
      const swirlSeconds = Math.max(0, (elapsed - timeline.swirlStart) / 1000);
      const swirlFraction = Math.min(1, swirlSeconds / (timeline.swirlMs / 1000));
      // Eased so the collapse settles into the held breath rather than stopping dead, which reads as a dropped frame.
      const implodeLinear = Math.max(0, Math.min(1, (elapsed - timeline.implodeStart) / timeline.implodeMs));
      const implodeFraction = implodeLinear * implodeLinear * (3 - 2 * implodeLinear);
      // Through the held breath the frame keeps moving: the view keeps creeping in, the charge keeps building and the
      // collapsed orb throbs in time with the two heartbeat thumps, so the screen never sits frozen.
      const breathSeconds = Math.max(0, (elapsed - timeline.implodeStart - timeline.implodeMs) / 1000);
      const breathFraction = Math.min(1, breathSeconds / (timeline.breathMs / 1000));
      const thump = function (at, width) {
        return Math.exp(-Math.pow((breathSeconds - at) / width, 2));
      };
      const heartbeatPulse = breathSeconds > 0 ? thump(0.03, 0.05) + 0.7 * thump(0.2, 0.05) : 0;
      const exploded = explodeTime !== null;

      // The view closes in on the orb while the trails wind around it.
      const zoom = exploded ? 1 :
        1 + 0.35 * swirlFraction * swirlFraction + 0.1 * implodeFraction + 0.08 * breathFraction * breathFraction;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      context.clearRect(0, 0, width, height);
      context.setTransform(ratio * zoom, 0, 0, ratio * zoom, ratio * orb.x * (1 - zoom), ratio * orb.y * (1 - zoom));
      context.lineCap = "round";
      context.lineJoin = "round";

      if (!exploded) {
        drawGrid(0.07 * (1 - implodeFraction), elapsed);

        // The race trails stay lit on the grid until the two are well into their orbits, then burn out.
        const raceTrailAlpha = Math.max(0, 1 - swirlFraction * 1.6);
        context.globalCompositeOperation = "lighter";
        routes.forEach(function (routeInfo, index) {
          // The two trade the lead back and forth, but arrive together.
          const surge = 0.045 * Math.sin(4 * Math.PI * raceFraction) * (index === 0 ? 1 : -1);
          const {trail, corner} = alongRoute(routeInfo, Math.max(0, Math.min(1, raceFraction + surge)));
          if (corner > routeInfo.lastCorner) {
            routeInfo.lastCorner = corner;
            onCorner();
          }
          strokeGlow(trail, colors[index], raceTrailAlpha);
          if (raceFraction < 1) {
            drawHead(trail[trail.length - 1], colors[index], 1);
          }
        });
        context.globalCompositeOperation = "source-over";

        // The orb fades up as the trails close in on it, then charges while they wind around it and collapses to a
        // point as it pulls them in.
        const orbVisible = Math.max(0, Math.min(1, (elapsed - (timeline.swirlStart - 1200)) / 900));
        if (orbVisible > 0) {
          const radius = orb.radius * (0.6 + 0.4 * orbVisible) * (1 - 0.75 * implodeFraction) * (1 + 0.45 * heartbeatPulse);
          const orbitNow = orbitRadius * (1 - 0.18 * swirlFraction) * (1 - implodeFraction);
          const charge = swirlFraction + implodeFraction + 0.5 * breathFraction + 0.6 * heartbeatPulse;
          const flicker = 0.85 + 0.15 * Math.sin(elapsed * 0.05 * (1 + 3 * swirlFraction));

          // Each trail as a comet tail wound round the orb, split into the parts behind and in front of it.
          const tails = [0, 1].map(function (index) {
            if (swirlSeconds <= 0) {
              return [];
            }
            const points = [];
            for (let step = 0; step < 36; step++) {
              const seconds = swirlSeconds - step * 0.014;
              if (seconds < 0) {
                break;
              }
              points.push(orbitPosition(index, seconds, orbitNow));
            }
            return points;
          });
          const drawTails = function (front) {
            context.globalCompositeOperation = "lighter";
            tails.forEach(function (points, index) {
              for (let i = 1; i < points.length; i++) {
                if ((points[i].z >= 0) !== front) {
                  continue;
                }
                strokeGlow([points[i - 1], points[i]], colors[index], (front ? 1 : 0.35) * (1 - i / points.length));
              }
            });
            context.globalCompositeOperation = "source-over";
          };

          drawTails(false);
          context.globalAlpha = orbVisible;
          drawOrb(radius, charge, flicker);
          drawTails(true);
          tails.forEach(function (points, index) {
            if (points.length > 0 && points[0].z >= 0) {
              drawHead(points[0], colors[index], 1 - implodeFraction);
            }
          });
        }
      }

      // The blast: streaks of light flying out from where the orb was.
      if (exploded) {
        context.globalCompositeOperation = "lighter";
        particles = particles.filter(function (particle) {
          particle.vx *= Math.pow(0.95, dt);
          particle.vy *= Math.pow(0.95, dt);
          particle.x += particle.vx * dt;
          particle.y += particle.vy * dt;
          particle.life -= particle.decay * dt;
          if (particle.life <= 0) {
            return false;
          }
          context.globalAlpha = particle.life;
          context.strokeStyle = particle.color;
          context.lineWidth = particle.width;
          context.beginPath();
          context.moveTo(particle.x - particle.vx * 2.5, particle.y - particle.vy * 2.5);
          context.lineTo(particle.x, particle.y);
          context.stroke();
          return true;
        });
        context.globalCompositeOperation = "source-over";
        if (particles.length === 0) {
          running = false;
        }
      }
      context.globalAlpha = 1;
      requestAnimationFrame(drawFrame);
    };

    return {
      start: function (cornerCallback) {
        onCorner = cornerCallback;
        running = true;
        startTime = performance.now();
        lastFrameTime = startTime;
        requestAnimationFrame(drawFrame);
      },
      explode: function (burst) {
        explodeTime = performance.now();
        particles = [];
        for (let i = 0; i < 320; i++) {
          const angle = Math.random() * Math.PI * 2;
          const speed = (8 + Math.random() * 34) * scale;
          particles.push({
            x: orb.x,
            y: orb.y,
            vx: Math.cos(angle) * speed,
            vy: Math.sin(angle) * speed,
            life: 1,
            decay: 0.012 + Math.random() * 0.02,
            width: 1.5 + Math.random() * 3,
            color: burst[i % burst.length],
          });
        }
      },
      stop: function () {
        running = false;
      },
      orbCenter: {x: orb.x / width, y: orb.y / height},
    };
  };

  // Fills in the standings shown under the result: each alliance's ranking points in qualifications, or where the winner
  // goes next in playoffs.
  const renderStandings = function (data) {
    const standings = $("#winnerRevealStandings").empty();
    if (data.isPlayoff) {
      const winner = data.left.color === data.winner ? data.left : data.right.color === data.winner ? data.right : null;
      if (data.champion) {
        // The champions are named team by team in place of the generic "Tournament Winner".
        $("<div class='reveal-destination'>").text(data.championTeams.join("  ·  ")).appendTo(standings);
        return;
      }
      if (winner !== null && winner.destination !== "") {
        // The server formats the destination as HTML with an en dash entity; decode it since this is set as text.
        $("<div class='reveal-destination'>").text(winner.destination.replace(/&ndash;/g, "–")).appendTo(standings);
      }
      return;
    }
    if (data.unrankedLabel !== "") {
      // There are no ranking points to show for a match that doesn't count towards the rankings.
      $("<div class='reveal-destination'>").text(data.unrankedLabel).appendTo(standings);
      return;
    }
    [data.left, data.right].forEach(function (alliance) {
      const plate = $("<div class='reveal-rp'>").attr("data-alliance", alliance.color).appendTo(standings);
      $("<span class='reveal-rp-name'>").text(alliance.color).appendTo(plate);
      const pips = $("<span class='reveal-rp-pips'>").appendTo(plate);
      for (let i = 0; i < Math.max(maxRankingPoints, alliance.rankingPoints); i++) {
        $("<span class='reveal-rp-pip'>").attr("data-lit", i < alliance.rankingPoints).appendTo(pips);
      }
      $("<span class='reveal-rp-count'>").text(`${alliance.rankingPoints} RP`).appendTo(plate);
    });
  };

  // Plays the reveal for the given result. Resolves at the midpoint of the exit wipe, while the screen is fully covered,
  // so the caller can start bringing in whatever sits underneath as the wipe clears.
  const playWinnerReveal = async function (data) {
    const reveal = $("#winnerReveal");
    const revealElement = reveal[0];
    const isTie = data.winner === "tie";

    // Everything that gives the result away is set up now but stays hidden until the orb goes off. The world behind it
    // is already the winner's color (or split between the two for a tie), just clipped away to nothing.
    reveal.removeAttr("data-winner");
    $("#winnerRevealLeft").attr("data-alliance", data.left.color);
    $("#winnerRevealRight").attr("data-alliance", data.right.color);
    const takeover = isTie ? 50 : data.winner === data.left.color ? 130 : -30;
    applyFrame(revealElement, {"--seam": `${takeover}%`, "--spread": "300%"});
    const header = $("#winnerRevealHeader > span").empty();
    $("<span>").text(data.kicker).appendTo(header);
    $("<span class='reveal-header-accent'>").text("Final result").appendTo(header);
    $("#winnerRevealTitle").text(data.title);
    $("#winnerRevealVerdict").text(data.verdict).toggle(data.verdict !== "");
    $("#winnerRevealLeftScore").text(data.leftScore).attr("data-won", isTie || data.winner === data.left.color);
    $("#winnerRevealRightScore").text(data.rightScore).attr("data-won", isTie || data.winner === data.right.color);
    renderStandings(data);
    $("#winnerRevealTiebreak").text(data.decider).toggle(data.decider !== "");
    const revealCards = $("#winnerRevealCards").empty();
    data.cards.forEach(function (revealCard) {
      $("<div class='reveal-card'>").attr("data-card", revealCard.card).text(revealCard.label).appendTo(revealCards);
    });
    $("#winnerRevealResult > div, #winnerRevealHeader, #winnerRevealRays, .reveal-shockwave").css("opacity", 0);
    $("#winnerRevealRace").css("opacity", 1);
    $("#winnerRevealStage").css("opacity", 1);
    await revealSound.start();
    reveal.css("display", "block");

    const race = createRace(document.getElementById("winnerRevealRace"), data.left.color, data.right.color);
    const orbAt = `${race.orbCenter.x * 100}% ${race.orbCenter.y * 100}%`;
    $("#winnerRevealWorld").css("clip-path", `circle(0px at ${orbAt})`);

    // The race: the stage fades up, and the two trails tear in and race each other to the orb.
    animate(reveal, [{opacity: 0}, {opacity: 1}], 300, ease.move);
    animate("#winnerRevealHeader", [
      {opacity: 0, transform: "translateY(-3vh)"},
      {opacity: 1, transform: "none"},
    ], 600, ease.in, 200);
    revealSound.tension((timeline.implodeStart + 300) / 1000);
    revealSound.whoosh(0.8, timeline.raceStart / 1000, 0.3);
    race.start(revealSound.zip);
    // Each beat is timed from the start of the race rather than chained off the one before, so timer lateness can't
    // build up and leave the canvas, which runs on the race clock, sitting past its end waiting for the explosion.
    const raceStartedAt = performance.now();
    const waitUntil = function (ms) {
      return wait(Math.max(0, ms - (performance.now() - raceStartedAt)));
    };
    await waitUntil(timeline.swirlStart);

    // The swirl: both trails wind round the orb, faster and tighter, as it charges.
    revealSound.swirl(timeline.swirlMs / 1000);
    await waitUntil(timeline.implodeStart);

    // The orb pulls everything in, and the sound drops out for a heartbeat.
    revealSound.implode(timeline.implodeMs / 1000);
    await waitUntil(timeline.implodeStart + timeline.implodeMs);
    revealSound.heartbeat();
    await waitUntil(timeline.explodeAt);

    // It goes off: a flash and shockwaves, the blast wave uncovering the winner's color behind it, light rays, the
    // result stamping down and the confetti going off around it.
    reveal.attr("data-champion", data.champion ? "" : null);
    reveal.attr("data-winner", data.winner);
    revealSound.impact(data.winner);
    race.explode(burstColors[data.winner]);
    DisplayEffects.confetti.fire(confettiColors[data.winner], {amount: isTie ? 0.8 : 1.1, delay: 200});
    if (!isTie) {
      DisplayEffects.confetti.fire(confettiColors[data.winner], {amount: 0.7, delay: 1200});
    }
    if (data.champion) {
      DisplayEffects.confetti.fire(championConfettiColors, {amount: 0.9, delay: 2200});
    }
    animate("#winnerRevealFlash", [{opacity: 1}, {opacity: 0}], 800, "ease-out");
    shake(26, 700);
    animate(".reveal-shockwave", [
      {opacity: 0.9, transform: "scale(0.2)", borderWidth: "1.4vh"},
      {opacity: 0, transform: "scale(5)", borderWidth: "0.2vh"},
    ], 1100, ease.in, 0, 160);
    animate("#winnerRevealWorld", [
      {clipPath: `circle(0px at ${orbAt})`},
      {clipPath: `circle(150vmax at ${orbAt})`},
    ], 900, ease.in);
    animate("#winnerRevealRays", [{opacity: 0}, {opacity: 1}], 900, ease.in, 150);
    if (data.verdict !== "") {
      // The verdict landing gets its own thud.
      setTimeout(function () {
        revealSound.slam();
        shake(10, 320);
      }, 580);
    }
    await Promise.all([
      animate("#winnerRevealTitle", stamp(1.8), 480, "linear", 150),
      animate("#winnerRevealVerdict", stamp(2.2), 420, "linear", 330),
      animate("#winnerRevealScore", [
        {opacity: 0, transform: "translateY(4vh)"},
        {opacity: 1, transform: "none"},
      ], 600, ease.in, 570),
      animate("#winnerRevealStandings", [
        {opacity: 0, transform: "translateY(2vh)"},
        {opacity: 1, transform: "none"},
      ], 500, ease.in, 800),
      animate(".reveal-rp-pip[data-lit=true]", [
        {opacity: 0, transform: "scale(0.2)"},
        {opacity: 1, transform: "none"},
      ], 350, ease.pop, 1000, 70),
      animate("#winnerRevealTiebreak", [{opacity: 0}, {opacity: 1}], 500, ease.in, 950),
      animate("#winnerRevealCards", [
        {opacity: 0, transform: "translateY(12px)"},
        {opacity: 1, transform: "none"},
      ], 500, ease.in, 1050),
    ]);

    // Hold on the result, then wipe it away.
    await wait(1800);
    revealSound.whoosh(1.2);
    await animate("#winnerRevealWipe", [
      {transform: "translateX(-155vw) skewX(-12deg)"},
      {transform: "translateX(0vw) skewX(-12deg)"},
    ], 550, ease.out);
    race.stop();
    $("#winnerRevealStage").css("opacity", 0);
    animate("#winnerRevealWipe", [{transform: "translateX(155vw) skewX(-12deg)"}], 650, ease.in).then(function () {
      reveal.hide();
      $("#winnerRevealWipe").css("transform", "");
    });
  };

  window.WinnerReveal = {
    // The reveal's synthesizer, which the audience display also uses for its other cues.
    sound: revealSound,
    cardLabels: cardLabels,
    mostSeriousCard: mostSeriousCard,

    // Turns a scorePosted websocket message into what the reveal shows.
    buildData: function (data, redSide) {
      // Go by the declared result rather than comparing scores, so a playoff match won on a tiebreaker reveals the
      // alliance that actually advanced.
      const winner = data.RedWon ? "red" : data.BlueWon ? "blue" : "tie";
      const isPlayoff = data.Match.Type === matchTypePlayoff;
      const alliance = function (color) {
        const isRed = color === "red";
        const playoffAlliance = isRed ? data.Match.PlayoffRedAlliance : data.Match.PlayoffBlueAlliance;
        return {
          color: color,
          label: isPlayoff && playoffAlliance > 0 ? `Alliance ${playoffAlliance}` : `${color} alliance`,
          score: String((isRed ? data.RedScoreSummary : data.BlueScoreSummary).Score),
          rankingPoints: (isRed ? data.RedRankingPoints : data.BlueRankingPoints) || 0,
          destination: (isRed ? data.RedDestination : data.BlueDestination) || "",
        };
      };
      const red = alliance("red");
      const blue = alliance("blue");
      // The match that decides the event gets the champions' treatment rather than a plain win.
      const winningAlliance = winner === "red" ? red : winner === "blue" ? blue : null;
      const champion = isPlayoff && winningAlliance !== null && winningAlliance.destination === "Tournament Winner";
      const championTeams = !champion ? [] : (winner === "red" ?
        [data.Match.Red1, data.Match.Red2, data.Match.Red3] : [data.Match.Blue1, data.Match.Blue2, data.Match.Blue3]
      ).filter(function (teamId) {
        return teamId > 0;
      });
      const left = redSide === "left" ? red : blue;
      const right = redSide === "left" ? blue : red;
      const cards = buildRevealCards(data, isPlayoff);
      const disqualified = cards.some(function (revealCard) {
        return revealCard.card === "dq";
      });
      return {
        winner: winner,
        isPlayoff: isPlayoff,
        // Practice and test matches don't count towards the rankings; this names the kind of match instead.
        unrankedLabel: data.Match.Type === matchTypePractice ? "Practice Match" :
          data.Match.Type === matchTypeQualification || isPlayoff ? "" : "Test Match",
        kicker: data.Match.LongName + (data.Match.NameDetail ? " – " + data.Match.NameDetail : ""),
        title: winner === "tie" ? "TIE MATCH" : (winner === "red" ? red : blue).label.toUpperCase(),
        verdict: winner === "tie" ? "" : champion ? "EVENT CHAMPIONS" : "WINS",
        champion: champion,
        championTeams: championTeams,
        left: left,
        right: right,
        leftScore: left.score,
        rightScore: right.score,
        // What decided the result if the points didn't.
        decider: data.TiebreakReason || (disqualified ? "Disqualification" : ""),
        cards: cards,
      };
    },

    // Plays the whole reveal, resolving once it has wiped away. Pass muted to keep this display silent.
    play: function (data, redSide, options = {}) {
      muted = Boolean(options.muted);
      mount();
      return playWinnerReveal(data);
    },
  };
})(window);
