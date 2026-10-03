// Client-side "match aborted" takeover shared by the audience, field monitor and alliance station displays.
//
// When an abort lands the screen strobes red, a shockwave rolls out from the center and a hazard-striped banner slams
// in and shakes; the banner then holds on screen until the match is reset. Each display picks a variant, which the
// stylesheet uses to size and place the banner for that screen.

const AbortTakeover = (function () {
  // How long the intro runs before settling into the held state.
  const introDurationMs = 2400;

  let root = null;
  let aborted = null;
  let introTimeout = null;

  const build = function (variant) {
    root = $(`
      <div class="abort-takeover">
        <div class="abort-strobe"></div>
        <div class="abort-shockwave"></div>
        <div class="abort-band">
          <div class="abort-band-inner">
            <div class="abort-hazard"></div>
            <div class="abort-band-body">
              <div class="abort-warning"></div>
              <div class="abort-text">
                <div class="abort-title">Match Aborted</div>
                <div class="abort-subtitle"></div>
              </div>
              <div class="abort-warning"></div>
            </div>
            <div class="abort-hazard"></div>
          </div>
        </div>
      </div>
    `).attr("data-variant", variant);
    $("body").append(root);
  };

  // Switches state, restarting the CSS animations if it's already in that state.
  const setState = function (state) {
    if (root.attr("data-state") === state) {
      root.attr("data-state", "off");
      void root[0].offsetWidth;
    }
    root.attr("data-state", state);
  };

  return {
    introDurationMs,

    // Updates the takeover from the match's aborted flag. The intro only plays on the transition into an abort, so a
    // display connecting after the fact goes straight to the held banner. Returns true if the intro was started.
    update: function (isAborted, variant, matchName) {
      if (root === null) {
        build(variant);
      }
      let started = false;
      if (isAborted) {
        root.find(".abort-subtitle").text(matchName || "");
        if (aborted === false) {
          setState("intro");
          clearTimeout(introTimeout);
          introTimeout = setTimeout(function () {
            setState("hold");
          }, introDurationMs);
          started = true;
        } else if (aborted === null) {
          setState("hold");
        }
      } else {
        clearTimeout(introTimeout);
        root.attr("data-state", "off");
      }
      aborted = isAborted;
      return started;
    },
  };
})();
