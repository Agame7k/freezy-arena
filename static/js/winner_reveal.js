// Copyright 2026 Team 254. All Rights Reserved.
//
// The winner reveal, shared by the audience and wall displays so both play the exact same sequence in step. The wall
// plays it muted, since the audience display carries the sound.

(function (window) {
  // Motion language shared with the audience display: things arrive fast and settle softly, leave by accelerating away,
  // and move in place along a symmetric curve.
  const ease = {
    in: "cubic-bezier(0.16, 1, 0.3, 1)",
    out: "cubic-bezier(0.7, 0, 0.84, 0)",
    move: "cubic-bezier(0.65, 0, 0.35, 1)",
    pop: "cubic-bezier(0.34, 1.56, 0.64, 1)",
  };
  const cardSeverity = {"": 0, "yellow": 1, "red": 2, "dq": 3};
  const cardLabels = {yellow: "Yellow card", red: "Red card", dq: "Disqualified"};
  // Whether this display keeps the reveal silent; set on each play().
  let muted = false;

  const wait = function (ms) {
    return new Promise(function (resolve) {
      setTimeout(resolve, ms);
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
        $(element).css(finalFrame);
        animation.cancel();
      }, function () {
        clearTimeout(backstopId);
      });
    }));
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
    const hexOuter = "0,-190 164.5,-95 164.5,95 0,190 -164.5,95 -164.5,-95";
    const hexInner = "0,-160 138.6,-80 138.6,80 0,160 -138.6,80 -138.6,-80";
    let emblem = "<circle id='winnerRevealOrbit' r='232'/>";
    for (let i = 0; i < 3; i++) {
      emblem += `<polygon class='reveal-shockwave' points='${hexOuter}'/>`;
    }
    emblem += `<polygon class='reveal-hex-outer' pathLength='1' points='${hexOuter}'/>`;
    emblem += `<polygon class='reveal-hex-inner' pathLength='1' points='${hexInner}'/>`;
    for (let i = 1; i <= 6; i++) {
      emblem += `<line class='reveal-tick' x1='0' y1='-202' x2='0' y2='-220' transform='rotate(${i * 60})'/>`;
    }
    $("body").prepend(`
      <div id="winnerReveal">
        <div id="winnerRevealStage">
          <svg id="winnerRevealGrid">
            <defs>
              <pattern id="winnerRevealHexPattern" width="56" height="100" patternUnits="userSpaceOnUse">
                <path d="M28 66L0 50L0 16L28 0L56 16L56 50L28 66L28 100"/>
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#winnerRevealHexPattern)"/>
          </svg>
          <div id="winnerRevealRays"></div>
          <canvas id="winnerRevealAmbient"></canvas>
          <div id="winnerRevealStrobe"></div>
          <div id="winnerRevealHud">
            <div class="reveal-corner" data-corner="top-left"></div>
            <div class="reveal-corner" data-corner="top-right"></div>
            <div class="reveal-corner" data-corner="bottom-left"></div>
            <div class="reveal-corner" data-corner="bottom-right"></div>
            <div class="reveal-ticker" data-edge="top"><div class="reveal-ticker-track"></div></div>
            <div class="reveal-ticker" data-edge="bottom"><div class="reveal-ticker-track"></div></div>
          </div>
          <div id="winnerRevealFlare"></div>
          <canvas id="winnerRevealParticles"></canvas>
          <div id="winnerRevealBeam"></div>
          <svg id="winnerRevealEmblem" viewBox="-250 -250 500 500">${emblem}</svg>
          <div id="winnerRevealText">
            <div id="winnerRevealKicker"></div>
            <div id="winnerRevealTitle"></div>
            <div id="winnerRevealVerdict"></div>
            <div id="winnerRevealScore">
              <span id="winnerRevealLeftScore"></span>
              <span class="winner-reveal-score-divider"></span>
              <span id="winnerRevealRightScore"></span>
            </div>
            <div id="winnerRevealTiebreak"></div>
            <div id="winnerRevealCards"></div>
          </div>
          <div id="winnerRevealWhiteout"></div>
        </div>
        <div id="winnerRevealWipe"></div>
      </div>
    `);
  };

  // -------------------------------------------------------------------------------------------------------------------
  // Winner reveal: a full-screen sequence drawn entirely in code, played between the logo and the final score. It is
  // built for suspense, so nothing hints at the result until the last moment: a beam and a hexagonal emblem draw in
  // neutral silver over a line of meaningless shifting glyphs, then the scene flickers between red and blue faster and
  // faster, falls still for a breath, and only then floods with the winner's color as the name snaps into place, the
  // emblem bursts, the verdict slams down and the score decodes. A diagonal color wipe then carries it all off to
  // uncover the score card underneath.
  // -------------------------------------------------------------------------------------------------------------------

  const revealGlyphs = Array.from("◆◇▲△▼▽◢◣◤◥■□◈⬡⬢╳ΞΣΔΛΠΦΨΩ");
  const revealDigitGlyphs = Array.from("0123456789");
  const revealColors = {red: "#ff5571", blue: "#4da3ff", tie: "#ffd166"};

  const randomGlyph = function (glyphs) {
    return glyphs[Math.floor(Math.random() * glyphs.length)];
  };

  // Soundtrack for the winner reveal, synthesized live with the Web Audio API so there are no audio files to manage and
  // every cue lands exactly on its visual beat. Each cue is fired from playWinnerReveal (or decodeText) at the matching
  // moment. If the browser refuses to start audio, every cue quietly does nothing and the reveal plays silently.
  const revealSound = (function () {
    let context = null;
    let output = null;
    let reverb = null;
    let noise = null;
    let tensionVoices = [];
    let lastChatterTime = 0;

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

    // Fades out and stops whatever is left of the tension bed over the given number of seconds.
    const releaseTension = function (fadeSec) {
      const now = context.currentTime;
      tensionVoices.forEach(function (voice) {
        voice.envelope.gain.cancelScheduledValues(now);
        voice.envelope.gain.setValueAtTime(Math.max(voice.envelope.gain.value, 0.0001), now);
        voice.envelope.gain.exponentialRampToValueAtTime(0.0001, now + fadeSec);
        voice.source.stop(now + fadeSec + 0.02);
      });
      tensionVoices = [];
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
      // thin tone gliding up an octave and a half. Held until impact() cuts it off.
      tension: function (duration) {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        const end = now + duration;
        const swell = function (source, peak, input = source) {
          const envelope = context.createGain();
          envelope.gain.setValueAtTime(0.0001, now);
          envelope.gain.exponentialRampToValueAtTime(peak, end);
          input.connect(envelope);
          envelope.connect(output);
          source.start(now);
          tensionVoices.push({source: source, envelope: envelope});
        };

        swell(oscillator("sine", 55), 0.3);
        swell(oscillator("sine", 55.6), 0.2);

        const riser = noiseSource();
        const riserFilter = context.createBiquadFilter();
        riserFilter.type = "bandpass";
        riserFilter.Q.value = 5;
        riserFilter.frequency.setValueAtTime(250, now);
        riserFilter.frequency.exponentialRampToValueAtTime(5000, end);
        riser.connect(riserFilter);
        swell(riser, 0.22, riserFilter);

        const glide = oscillator("triangle", 220);
        glide.frequency.exponentialRampToValueAtTime(660, end);
        swell(glide, 0.05);
      },

      whoosh: whoosh,

      // A tiny, quiet digital blip at a random pitch: the texture of glyphs cycling. Rate-limited so several decoders
      // running at once don't pile up into noise.
      chatter: function () {
        if (!ready() || context.currentTime - lastChatterTime < 0.06) {
          return;
        }
        lastChatterTime = context.currentTime;
        hit(oscillator("square", 1800 + Math.random() * 2600), 0.018, 0.002, 0.03, 0);
      },

      // A clean blip for a character locking into place; progress (0 to 1) raises the pitch across the word.
      lock: function (progress) {
        if (!ready()) {
          return;
        }
        hit(oscillator("sine", 620 * Math.pow(2, progress * 1.3)), 0.09, 0.003, 0.12, 0.25);
      },

      // One tick of the red/blue roulette: two alternating pitches, like a clock that can't make up its mind.
      tease: function (side) {
        if (!ready()) {
          return;
        }
        hit(oscillator("triangle", side === "red" ? 392 : 523.25), 0.12, 0.003, 0.09, 0.2);
        const click = noiseSource();
        const clickFilter = context.createBiquadFilter();
        clickFilter.type = "highpass";
        clickFilter.frequency.value = 3000;
        click.connect(clickFilter);
        hit(click, 0.06, 0.001, 0.02, 0, context.currentTime, clickFilter);
      },

      // The held breath right before the answer: lets the tension fall away to near-silence.
      hush: function () {
        if (!ready()) {
          return;
        }
        releaseTension(0.3);
      },

      // The payoff: cuts the tension and fires a sub drop, a bright crack and a wide shimmering chord all at once.
      impact: function (winner) {
        if (!ready()) {
          return;
        }
        const now = context.currentTime;
        releaseTension(0.08);

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

  // Decodes text into the element: every character starts out as a shifting glyph and locks into its real letter in a
  // rough left-to-right wave. Each character's box is fixed to the width of its final letter up front, so the line
  // doesn't jitter while the glyphs cycle. Runs on wall-clock timers rather than frames so it always finishes on time.
  const decodeText = function (target, text, duration, delay, glyphs = revealGlyphs) {
    const container = $(target).empty();
    const letters = Array.from(text);
    const spans = letters.map(function (letter) {
      const span = $("<span>").text(letter).appendTo(container);
      if (letter.trim() === "") {
        return null;
      }
      span.css("width", span[0].getBoundingClientRect().width + "px").addClass("reveal-glyph")
        .text(randomGlyph(glyphs));
      return span;
    });

    const start = Date.now() + delay;
    const lockTimes = spans.map(function (span, i) {
      const wave = letters.length > 1 ? i / (letters.length - 1) : 1;
      return start + Math.min(duration, duration * (0.3 + 0.6 * wave) + Math.random() * duration * 0.1);
    });

    return new Promise(function (resolve) {
      const intervalId = setInterval(function () {
        const now = Date.now();
        let pending = false;
        spans.forEach(function (span, i) {
          if (span === null || span.hasClass("locked")) {
            return;
          }
          if (now >= lockTimes[i]) {
            span.text(letters[i]).addClass("locked");
            revealSound.lock(letters.length > 1 ? i / (letters.length - 1) : 1);
          } else {
            pending = true;
            span.text(randomGlyph(glyphs));
          }
        });
        if (pending && now >= start) {
          revealSound.chatter();
        }
        if (!pending) {
          clearInterval(intervalId);
          resolve();
        }
      }, 45);
    });
  };

  // Fills the element with a fixed number of endlessly shifting glyphs in identical boxes, so it hints at nothing about
  // the text that will eventually replace it. Returns a function that stops the cycling.
  const scrambleText = function (target, count, glyphs = revealGlyphs) {
    const container = $(target).empty();
    const spans = [];
    for (let i = 0; i < count; i++) {
      spans.push($("<span>").addClass("reveal-glyph reveal-glyph-fixed").text(randomGlyph(glyphs)).appendTo(container));
    }
    const intervalId = setInterval(function () {
      spans.forEach(function (span) {
        span.text(randomGlyph(glyphs));
      });
      revealSound.chatter();
    }, 45);
    return function () {
      clearInterval(intervalId);
    };
  };

  // Throws a burst of small outlined shards outward from the emblem's edge, slowing and fading as they go. Purely
  // decorative, so nothing waits on it.
  const burstRevealParticles = function (colors) {
    const canvas = document.getElementById("winnerRevealParticles");
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
        x: width / 2 + Math.cos(angle) * 190,
        y: height / 2 + Math.sin(angle) * 190,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed,
        size: 3 + Math.random() * 10,
        rotation: Math.random() * Math.PI,
        spin: (Math.random() - 0.5) * 0.3,
        sides: Math.random() < 0.5 ? 3 : 4,
        filled: Math.random() < 0.3,
        color: colors[i % colors.length],
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
      }
    };
    requestAnimationFrame(drawFrame);
  };

  // The reveal's ambient background layer: faint columns of falling glyphs plus drifting dust motes, redrawn every
  // frame in whatever --reveal-color currently is so it follows the build-up, roulette and reveal automatically.
  // surge() kicks the rain into a brief burst of speed that eases back off.
  const revealAmbient = (function () {
    const columnSpacing = 42;
    const glyphSize = 20;
    let canvas = null;
    let context = null;
    let columns = [];
    let motes = [];
    let running = false;
    let boost = 0;

    const setUp = function () {
      canvas = document.getElementById("winnerRevealAmbient");
      context = canvas.getContext("2d");
      const ratio = window.devicePixelRatio || 1;
      canvas.width = window.innerWidth * ratio;
      canvas.height = window.innerHeight * ratio;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);

      columns = [];
      for (let x = columnSpacing / 2; x < window.innerWidth; x += columnSpacing) {
        const length = 8 + Math.floor(Math.random() * 14);
        columns.push({
          x: x,
          y: -Math.random() * window.innerHeight,
          speed: 1.5 + Math.random() * 4,
          glyphs: Array.from({length: length}, function () {
            return randomGlyph(revealGlyphs);
          }),
        });
      }
      motes = Array.from({length: 80}, function () {
        return {
          x: Math.random() * window.innerWidth,
          y: Math.random() * window.innerHeight,
          radius: 0.8 + Math.random() * 2.2,
          drift: 0.2 + Math.random() * 0.6,
          phase: Math.random() * Math.PI * 2,
        };
      });
    };

    const drawFrame = function () {
      if (!running) {
        context.clearRect(0, 0, canvas.width, canvas.height);
        return;
      }
      const width = window.innerWidth;
      const height = window.innerHeight;
      const color = getComputedStyle(canvas).getPropertyValue("--reveal-color").trim() || "#dfe7f2";
      const speedFactor = 1 + boost * 7;
      boost *= 0.965;
      context.clearRect(0, 0, width, height);
      context.fillStyle = color;
      context.font = `${glyphSize}px sans-serif`;
      context.textAlign = "center";

      columns.forEach(function (column) {
        column.y += column.speed * speedFactor;
        if (column.y - column.glyphs.length * glyphSize > height) {
          column.y = -Math.random() * height * 0.5;
        }
        if (Math.random() < 0.15) {
          column.glyphs[Math.floor(Math.random() * column.glyphs.length)] = randomGlyph(revealGlyphs);
        }
        // The head of each column is brightest, trailing off toward the tail.
        column.glyphs.forEach(function (glyph, i) {
          context.globalAlpha = (1 - i / column.glyphs.length) * (0.22 + boost * 0.5);
          context.fillText(glyph, column.x, column.y - i * glyphSize);
        });
      });

      const now = performance.now() / 1000;
      motes.forEach(function (mote) {
        mote.y -= mote.drift * speedFactor;
        if (mote.y < -5) {
          mote.y = height + 5;
          mote.x = Math.random() * width;
        }
        context.globalAlpha = 0.25 + 0.35 * (0.5 + 0.5 * Math.sin(now * 2 + mote.phase));
        context.beginPath();
        context.arc(mote.x, mote.y, mote.radius, 0, Math.PI * 2);
        context.fill();
      });
      context.globalAlpha = 1;
      requestAnimationFrame(drawFrame);
    };

    return {
      start: function () {
        setUp();
        boost = 0;
        running = true;
        requestAnimationFrame(drawFrame);
      },
      stop: function () {
        running = false;
      },
      surge: function () {
        boost = 1;
      },
    };
  })();

  // Fills both edge tickers with the given phrase, repeated enough to loop seamlessly (the track is two identical
  // halves and scrolls by exactly one half).
  const setRevealTicker = function (phrase) {
    const half = Array(10).fill(phrase).join("  ◆  ") + "  ◆  ";
    $(".reveal-ticker-track").text(half + half);
  };

  // Plays the reveal for the given result. Resolves at the midpoint of the exit wipe, while the screen
  // is fully covered, so the caller can start bringing in whatever sits underneath as the wipe clears.
  const playWinnerReveal = async function (data, redSide) {
    const accent = revealColors[data.winner];
    const reveal = $("#winnerReveal");
    const colors = data.winner === "tie" ? [revealColors.red, revealColors.blue, revealColors.tie] :
      [revealColors[data.winner], "#ffffff", revealColors.tie];

    // Start neutral: nothing on screen may hint at the result until the reveal moment.
    reveal.removeAttr("data-winner").removeAttr("data-tease");
    const scoreStyle = document.getElementById("winnerRevealScore").style;
    scoreStyle.setProperty("--left-reveal-color", redSide === "left" ? revealColors.red : revealColors.blue);
    scoreStyle.setProperty("--right-reveal-color", redSide === "left" ? revealColors.blue : revealColors.red);
    $("#winnerRevealKicker, #winnerRevealTitle, #winnerRevealLeftScore, #winnerRevealRightScore").empty();
    $("#winnerRevealVerdict").text(data.verdict).toggle(data.verdict !== "");
    $("#winnerRevealTiebreak").text(data.tiebreak).toggle(data.tiebreak !== "");
    const revealCards = $("#winnerRevealCards").empty();
    data.cards.forEach(function (revealCard) {
      $("<div class='reveal-card'>").attr("data-card", revealCard.card).text(revealCard.label).appendTo(revealCards);
    });
    // Elements that only animate in act two would otherwise still be showing from the previous reveal during act one.
    $("#winnerRevealText > div, #winnerRevealFlare").css("opacity", 0);
    $("#winnerRevealTitle").css({textShadow: "", transform: ""});
    $("#winnerRevealStage").css("opacity", 1);
    setRevealTicker(`${data.kicker.toUpperCase()}  ◆  FINAL RESULT`);
    const particleCanvas = document.getElementById("winnerRevealParticles");
    particleCanvas.getContext("2d").clearRect(0, 0, particleCanvas.width, particleCanvas.height);
    await revealSound.start();
    reveal.css("display", "flex");
    revealAmbient.start();

    // Act one: the beam draws and the emblem traces itself in neutral silver, while the title line fills with shifting
    // glyphs that give nothing away (not even the length of the name). The tension bed builds through act two.
    revealSound.whoosh(1.0, 0.15, 0.25);
    revealSound.tension(3.3);
    let stopScramble = function () {};
    await Promise.all([
      animate(reveal, [{opacity: 0}, {opacity: 1}], 350, ease.move),
      animate("#winnerRevealBeam", [
        {transform: "scaleX(0)", opacity: 1},
        {transform: "scaleX(1)", opacity: 1, offset: 0.6},
        {transform: "scaleX(1)", opacity: 0},
      ], 1200, ease.in, 150),
      animate(".reveal-hex-outer", [
        {strokeDashoffset: 1, transform: "rotate(-60deg)"},
        {strokeDashoffset: 0, transform: "rotate(0deg)"},
      ], 1400, ease.in, 350),
      animate(".reveal-hex-inner", [
        {strokeDashoffset: -1, transform: "rotate(60deg)"},
        {strokeDashoffset: 0, transform: "rotate(0deg)"},
      ], 1200, ease.in, 600),
      animate(".reveal-tick", [{opacity: 0}, {opacity: 1}], 400, ease.in, 1200, 70),
      animate("#winnerRevealOrbit", [{opacity: 0}, {opacity: 1}], 1200, ease.in, 500),
      animate("#winnerRevealGrid", [{opacity: 0}, {opacity: 0.12}], 1400, ease.in),
      animate("#winnerRevealRays", [{opacity: 0}, {opacity: 0.14}], 1600, ease.in, 200),
      animate(".reveal-corner", [
        {opacity: 0, transform: "scale(1.5)"},
        {opacity: 1, transform: "scale(1)"},
      ], 700, ease.in, 250, 70),
      animate(".reveal-ticker", [{opacity: 0}, {opacity: 1}], 900, ease.in, 500),
      animate("#winnerRevealKicker", [
        {opacity: 0, transform: "translateY(-12px)"},
        {opacity: 1, transform: "translateY(0px)"},
      ], 800, ease.in, 450),
      decodeText("#winnerRevealKicker", data.kicker.toUpperCase(), 700, 450),
      wait(700).then(function () {
        stopScramble = scrambleText("#winnerRevealTitle", 9);
        return animate("#winnerRevealTitle", [{opacity: 0, filter: "blur(12px)"}, {opacity: 1, filter: "blur(0px)"}],
          500, ease.in);
      }),
    ]);

    // Act two: the roulette. The whole scene flickers between the two alliance colors, faster and faster, never
    // settling on either.
    let teaseSide = Math.random() < 0.5 ? "red" : "blue";
    for (let interval = 380; interval > 60; interval *= 0.8) {
      reveal.attr("data-tease", teaseSide);
      revealSound.tease(teaseSide);
      animate("#winnerRevealStrobe", [{opacity: 0.7}, {opacity: 0}], interval * 0.95, ease.in);
      await wait(interval);
      teaseSide = teaseSide === "red" ? "blue" : "red";
    }

    // The breath: back to neutral, the emblem draws in, and the sound drops away for a beat before the answer.
    reveal.removeAttr("data-tease");
    revealSound.hush();
    await Promise.all([
      animate("#winnerRevealEmblem", [{transform: "scale(1)"}, {transform: "scale(0.93)"}], 450, ease.move),
      animate("#winnerRevealGrid", [{opacity: 0.04}], 450, ease.move),
      animate("#winnerRevealRays", [{opacity: 0.04}], 450, ease.move),
    ]);

    // Act three: the color floods in, the name snaps into place and everything fires at once.
    stopScramble();
    reveal.attr("data-winner", data.winner);
    burstRevealParticles(colors);
    revealAmbient.surge();
    setRevealTicker(data.winner === "tie" ?
      `TIE MATCH  ◆  ${data.leftScore} – ${data.rightScore}` :
      `${data.title} ${data.verdict}  ◆  ${data.leftScore} – ${data.rightScore}`);
    animate("#winnerRevealWhiteout", [{opacity: 0}, {opacity: 0.85, offset: 0.1}, {opacity: 0}], 650, "ease-out");
    animate(".reveal-shockwave", [
      {opacity: 0.9, transform: "scale(1)", strokeWidth: 6},
      {opacity: 0, transform: "scale(5)", strokeWidth: 1},
    ], 1600, ease.in, 0, 180);
    animate("#winnerRevealGrid", [
      {opacity: 0.6, transform: "scale(1.25)"},
      {opacity: 0.14, transform: "scale(1)"},
    ], 1500, ease.in);
    animate("#winnerRevealRays", [{opacity: 0.85}, {opacity: 0.3}], 1800, ease.in);
    animate("#winnerRevealStage", [
      {transform: "translate(0px, 0px)"},
      {transform: "translate(-14px, 8px)"},
      {transform: "translate(11px, -9px)"},
      {transform: "translate(-7px, -5px)"},
      {transform: "translate(5px, 6px)"},
      {transform: "translate(-2px, 2px)"},
      {transform: "translate(0px, 0px)"},
    ], 480, "linear");
    revealSound.impact(data.winner);
    if (data.verdict !== "") {
      setTimeout(revealSound.slam, 100);
    }
    await Promise.all([
      animate("#winnerRevealFlare", [
        {opacity: 0, transform: "scale(0.5)"},
        {opacity: 1, transform: "scale(1)", offset: 0.2},
        {opacity: 0, transform: "scale(1.7)"},
      ], 1200, ease.in),
      animate("#winnerRevealEmblem", [
        {transform: "scale(0.93)"},
        {transform: "scale(1.12)", offset: 0.18},
        {transform: "scale(1)"},
      ], 1000, ease.in),
      decodeText("#winnerRevealTitle", data.title, 450, 0),
      animate("#winnerRevealTitle", [
        {transform: "scale(1)", textShadow: "0 0 0 transparent"},
        {transform: "scale(1.05)", textShadow: `0 0 40px ${accent}`, offset: 0.2},
        {transform: "scale(1)", textShadow: `0 0 18px ${accent}`},
      ], 900, ease.in),
      animate("#winnerRevealVerdict", [
        {opacity: 0, transform: "scale(1.6)", letterSpacing: "1em", filter: "blur(16px)"},
        {opacity: 1, transform: "scale(1)", letterSpacing: "0.22em", filter: "blur(0px)"},
      ], 650, ease.in, 100),
      animate("#winnerRevealScore", [
        {opacity: 0, transform: "translateY(18px)"},
        {opacity: 1, transform: "translateY(0px)"},
      ], 600, ease.in, 350),
      decodeText("#winnerRevealLeftScore", data.leftScore, 800, 350, revealDigitGlyphs),
      decodeText("#winnerRevealRightScore", data.rightScore, 800, 450, revealDigitGlyphs),
      animate("#winnerRevealTiebreak", [{opacity: 0}, {opacity: 1}], 600, ease.in, 900),
      animate("#winnerRevealCards", [
        {opacity: 0, transform: "translateY(12px)"},
        {opacity: 1, transform: "translateY(0px)"},
      ], 600, ease.in, 1050),
    ]);

    // Hold on the result, then wipe it away.
    await wait(1500);
    revealSound.whoosh(1.2);
    await animate("#winnerRevealWipe", [
      {transform: "translateX(-155vw) skewX(-12deg)"},
      {transform: "translateX(0vw) skewX(-12deg)"},
    ], 550, ease.out);
    $("#winnerRevealStage").css("opacity", 0);
    animate("#winnerRevealWipe", [{transform: "translateX(155vw) skewX(-12deg)"}], 650, ease.in).then(function () {
      reveal.hide();
      revealAmbient.stop();
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
      const scores = {red: String(data.RedScoreSummary.Score), blue: String(data.BlueScoreSummary.Score)};
      return {
        winner: winner,
        kicker: data.Match.LongName + (data.Match.NameDetail ? " – " + data.Match.NameDetail : ""),
        title: winner === "tie" ? "TIE MATCH" : winner.toUpperCase() + " ALLIANCE",
        verdict: winner === "tie" ? "" : "WINS",
        leftScore: scores[redSide === "left" ? "red" : "blue"],
        rightScore: scores[redSide === "left" ? "blue" : "red"],
        tiebreak: data.TiebreakReason || "",
        cards: buildRevealCards(data, data.Match.Type === matchTypePlayoff),
      };
    },

    // Plays the whole reveal, resolving once it has wiped away. Pass muted to keep this display silent.
    play: function (data, redSide, options = {}) {
      muted = Boolean(options.muted);
      mount();
      return playWinnerReveal(data, redSide);
    },
  };
})(window);
