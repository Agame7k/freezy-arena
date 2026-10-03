// Copyright 2026 Team 254. All Rights Reserved.
//
// Effects shared by the full-screen sequences on the audience and wall displays: sparks thrown off the seam where the
// two alliances meet, confetti for a win, and numbers that count up to their value.

(function (window) {
  // Frame-rate independent physics: every speed below is in pixels per frame at 60 fps, scaled by how long the last
  // frame actually took (capped, so a stalled tab doesn't teleport everything on its next frame).
  const frameScale = function (now, lastTime) {
    return Math.min(3, (now - lastTime) / (1000 / 60));
  };

  // Sizes the canvas to the window at the device's pixel ratio and returns a context that draws in CSS pixels.
  const fitCanvas = function (canvas) {
    const ratio = window.devicePixelRatio || 1;
    canvas.width = window.innerWidth * ratio;
    canvas.height = window.innerHeight * ratio;
    const context = canvas.getContext("2d");
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    return context;
  };

  // Sparks, one animation loop per canvas so that several bursts on the same canvas draw together instead of wiping
  // each other out.
  const sparkLoops = new Map();

  // Throws a burst of sparks off the diagonal seam: short streaks that shoot out sideways, drop and burn out within
  // about half a second. seamX is where the seam crosses the middle of the screen and halfSkew how far it leans from
  // there to the top and bottom edges (the seam runs from seamX + halfSkew at the top to seamX - halfSkew at the
  // bottom). band limits them to the middle fraction of the seam's height.
  const sparks = function (canvas, {seamX, halfSkew, count = 120, power = 1, band = 1}) {
    if (!canvas) {
      return;
    }
    let loop = sparkLoops.get(canvas);
    if (loop === undefined) {
      loop = {particles: [], running: false, context: null, lastTime: 0};
      sparkLoops.set(canvas, loop);
    }
    if (!loop.running) {
      loop.context = fitCanvas(canvas);
    }

    const height = window.innerHeight;
    const colors = ["#ffffff", "#fff3c4", "#ffd166"];
    for (let i = 0; i < count; i++) {
      const y = height * (0.5 + (Math.random() - 0.5) * band);
      const direction = Math.random() < 0.5 ? -1 : 1;
      const speed = (4 + Math.random() * 15) * power;
      loop.particles.push({
        x: seamX + halfSkew * (1 - (2 * y) / height),
        y: y,
        vx: direction * speed,
        vy: (Math.random() - 0.65) * 7 * power,
        life: 1,
        decay: 0.025 + Math.random() * 0.045,
        width: 1 + Math.random() * 2.2,
        color: colors[i % colors.length],
      });
    }

    if (loop.running) {
      return;
    }
    loop.running = true;
    loop.lastTime = performance.now();
    const drawFrame = function (now) {
      const dt = frameScale(now, loop.lastTime);
      loop.lastTime = now;
      const context = loop.context;
      context.clearRect(0, 0, window.innerWidth, window.innerHeight);
      context.globalCompositeOperation = "lighter";
      context.lineCap = "round";
      loop.particles = loop.particles.filter(function (particle) {
        particle.vy += 0.35 * dt;
        particle.vx *= Math.pow(0.95, dt);
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
        context.moveTo(particle.x - particle.vx * 1.8, particle.y - particle.vy * 1.8);
        context.lineTo(particle.x, particle.y);
        context.stroke();
        return true;
      });
      context.globalAlpha = 1;
      context.globalCompositeOperation = "source-over";
      if (loop.particles.length > 0) {
        requestAnimationFrame(drawFrame);
      } else {
        context.clearRect(0, 0, window.innerWidth, window.innerHeight);
        loop.running = false;
      }
    };
    requestAnimationFrame(drawFrame);
  };

  // Paper confetti drawn on a canvas of its own above everything else on the page, so it keeps falling over whatever
  // screen comes up after the moment that fired it.
  const confetti = (function () {
    let canvas = null;
    let context = null;
    let pieces = [];
    // Pieces waiting for their launch time, so each cannon fires as a short stream rather than a single frame.
    let queued = [];
    let running = false;
    let lastTime = 0;

    const makePiece = function (x, y, angle, speed, colors, scale) {
      return {
        x: x,
        y: y,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed,
        width: (8 + Math.random() * 8) * scale,
        height: (4 + Math.random() * 5) * scale,
        color: colors[Math.floor(Math.random() * colors.length)],
        rotation: Math.random() * Math.PI * 2,
        spin: (Math.random() - 0.5) * 0.25,
        flip: Math.random() * Math.PI * 2,
        flipSpeed: 0.07 + Math.random() * 0.13,
        sway: Math.random() * Math.PI * 2,
        swaySpeed: 0.03 + Math.random() * 0.05,
        // How fast it can fall once it's fluttering rather than flying.
        fallSpeed: (2.2 + Math.random() * 2.2) * scale,
      };
    };

    const drawFrame = function (now) {
      const dt = frameScale(now, lastTime);
      lastTime = now;
      const width = window.innerWidth;
      const height = window.innerHeight;
      const scale = height / 1080;

      queued = queued.filter(function (entry) {
        if (now < entry.at) {
          return true;
        }
        pieces.push(entry.piece);
        return false;
      });

      context.clearRect(0, 0, width, height);
      pieces = pieces.filter(function (piece) {
        piece.vx *= Math.pow(0.98, dt);
        piece.vy += 0.34 * scale * dt;
        if (piece.vy > piece.fallSpeed) {
          piece.vy += (piece.fallSpeed - piece.vy) * Math.min(1, 0.08 * dt);
        }
        piece.sway += piece.swaySpeed * dt;
        piece.x += (piece.vx + Math.sin(piece.sway) * 1.3 * scale) * dt;
        piece.y += piece.vy * dt;
        piece.rotation += piece.spin * dt;
        piece.flip += piece.flipSpeed * dt;
        if (piece.y > height + 40 || piece.x < -80 || piece.x > width + 80) {
          return false;
        }

        // Squashing one axis by the cosine of the flip angle reads as a strip of paper tumbling in 3D; the back face is
        // drawn a little darker.
        const flip = Math.cos(piece.flip);
        context.save();
        context.translate(piece.x, piece.y);
        context.rotate(piece.rotation);
        context.scale(1, flip);
        context.globalAlpha = flip < 0 ? 0.7 : 1;
        context.fillStyle = piece.color;
        context.fillRect(-piece.width / 2, -piece.height / 2, piece.width, piece.height);
        context.restore();
        return true;
      });

      if (pieces.length > 0 || queued.length > 0) {
        requestAnimationFrame(drawFrame);
      } else {
        context.clearRect(0, 0, width, height);
        running = false;
      }
    };

    return {
      // Fires a cannon of confetti up from each bottom corner, followed by a slower fall from the top of the screen.
      fire: function (colors, {amount = 1, delay = 0} = {}) {
        if (canvas === null) {
          canvas = document.createElement("canvas");
          canvas.id = "celebrationConfetti";
          document.body.appendChild(canvas);
        }
        if (!running) {
          context = fitCanvas(canvas);
        }

        const width = window.innerWidth;
        const height = window.innerHeight;
        const scale = height / 1080;
        const start = performance.now() + delay;
        const perCannon = Math.round(95 * amount);
        [-1, 1].forEach(function (side) {
          const x = side < 0 ? width * 0.03 : width * 0.97;
          for (let i = 0; i < perCannon; i++) {
            // Aim up and in towards the middle of the screen, with some fanning out.
            const angle = -Math.PI / 2 - side * (0.18 + Math.random() * 0.5);
            // Tops out around 85% of the way up the screen; any faster and pieces sail far off the top and take the best
            // part of half a minute to drift back down over whatever screen comes next.
            const speed = (17 + Math.random() * 8) * scale;
            queued.push({at: start + Math.random() * 260, piece: makePiece(x, height + 10, angle, speed, colors, scale)});
          }
        });
        for (let i = 0; i < Math.round(110 * amount); i++) {
          const piece = makePiece(Math.random() * width, -20, Math.PI / 2, (1 + Math.random() * 2) * scale, colors, scale);
          queued.push({at: start + 350 + Math.random() * 2600, piece: piece});
        }

        if (!running) {
          running = true;
          lastTime = performance.now();
          requestAnimationFrame(drawFrame);
        }
      },
    };
  })();

  // Counts a number up from zero to the value the element currently displays, fast at first and slowing into place.
  // Gives up quietly if something else rewrites the text part way through (e.g. a corrected score being posted), so
  // the newer value always wins. Runs on a wall-clock backstop as well as frames, so it always finishes on time even if
  // the window stops painting.
  const countUp = function (element, duration, delay = 0) {
    const finalText = element.textContent;
    const target = parseInt(finalText);
    if (isNaN(target) || target <= 0) {
      return Promise.resolve();
    }
    return new Promise(function (resolve) {
      let lastWritten = "0";
      element.textContent = lastWritten;
      let startTime = null;
      let done = false;
      const backstopId = setTimeout(function () {
        if (!done && element.textContent === lastWritten) {
          element.textContent = finalText;
        }
        done = true;
        resolve();
      }, delay + duration + 250);
      const step = function (now) {
        if (done) {
          return;
        }
        if (element.textContent !== lastWritten) {
          done = true;
          clearTimeout(backstopId);
          resolve();
          return;
        }
        if (startTime === null) {
          startTime = now + delay;
        }
        const progress = Math.max(0, Math.min(1, (now - startTime) / duration));
        const eased = progress === 1 ? 1 : 1 - Math.pow(2, -10 * progress);
        lastWritten = progress === 1 ? finalText : String(Math.round(target * eased));
        element.textContent = lastWritten;
        if (progress < 1) {
          requestAnimationFrame(step);
        } else {
          done = true;
          clearTimeout(backstopId);
          resolve();
        }
      };
      requestAnimationFrame(step);
    });
  };

  window.DisplayEffects = {
    sparks: sparks,
    confetti: confetti,
    countUp: countUp,
  };
})(window);
