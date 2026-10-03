// Qualification standings table shared by the audience and wall displays, so both show the same thing the same way.
// It fills a panel element with a header, a page of teams and a footer, and rolls through the pages when there are more
// teams than fit on one. Where the panel sits and how it comes and goes is up to each display.

const StandingsTable = (function () {
  const columns = ["Rank", "Team", "Name", "RP", "Match Pts", "W-L-T", "Played"];

  const create = function (panel, {rowsPerPage = 12, pageMs = 9000} = {}) {
    let pages = [];
    let pageIndex = 0;
    let pageTimer = null;

    $(panel).addClass("standings-panel").html(`
      <div class="standings-header"><span>Standings</span><span class="standings-as-of"></span></div>
      <table class="standings-table">
        <thead><tr></tr></thead>
        <tbody></tbody>
      </table>
      <div class="standings-footer"></div>
    `);
    const headerRow = $(panel).find("thead tr");
    columns.forEach(function (column) {
      $("<th>").text(column).toggleClass("standings-name", column === "Name").appendTo(headerRow);
    });
    const rows = $(panel).find("tbody");

    // Brings each row in from the left, one after another.
    const cascadeIn = function (delay) {
      return Promise.all(rows.children().toArray().map(function (row, i) {
        return row.animate([
          {opacity: 0, transform: "translateX(-30px)"},
          {opacity: 1, transform: "none"},
        ], {duration: 450, delay: delay + i * 40, easing: "cubic-bezier(0.16, 1, 0.3, 1)", fill: "backwards"}).finished
          .catch(function () {});
      }));
    };

    const cascadeOut = function () {
      return Promise.all(rows.children().toArray().map(function (row, i) {
        return row.animate([{opacity: 1}, {opacity: 0, transform: "translateX(30px)"}],
          {duration: 300, delay: i * 20, easing: "cubic-bezier(0.7, 0, 0.84, 0)", fill: "forwards"}).finished
          .catch(function () {});
      }));
    };

    const table = {
      // Fetches the latest qualification rankings and splits them into pages.
      load: function () {
        return $.getJSON("/api/rankings").then(function (data) {
          pages = [];
          for (let i = 0; i < data.Rankings.length; i += rowsPerPage) {
            pages.push(data.Rankings.slice(i, i + rowsPerPage));
          }
          $(panel).find(".standings-as-of").text(data.HighestPlayedMatch ? `As of ${data.HighestPlayedMatch}` : "");
        }, function () {
          pages = [];
        });
      },

      // Fills the table with one page of teams and brings the rows in.
      render: function (index = 0, delay = 0) {
        pageIndex = index;
        rows.empty();
        const footer = $(panel).find(".standings-footer");
        if (pages.length === 0) {
          $("<tr>").append(
            $("<td class='standings-empty'>").attr("colspan", columns.length)
              .text("Standings will appear after the first qualification match.")
          ).appendTo(rows);
          footer.text("");
          return Promise.resolve();
        }
        pages[index].forEach(function (ranking) {
          $("<tr>").append(
            $("<td>").text(ranking.Rank),
            $("<td>").text(ranking.TeamId),
            $("<td class='standings-name'>").text(ranking.Nickname),
            $("<td>").text(ranking.RankingPoints),
            $("<td>").text(ranking.MatchPoints),
            $("<td>").text(`${ranking.Wins}-${ranking.Losses}-${ranking.Ties}`),
            $("<td>").text(ranking.Played),
          ).appendTo(rows);
        });
        const first = index * rowsPerPage + 1;
        const total = pages.reduce(function (sum, page) {
          return sum + page.length;
        }, 0);
        footer.text(pages.length > 1 ? `Teams ${first}–${first + pages[index].length - 1} of ${total}` : "");
        return cascadeIn(delay);
      },

      // Rolls on to the next page every so often while there's more than one.
      startCycle: function () {
        table.stopCycle();
        if (pages.length < 2) {
          return;
        }
        pageTimer = setInterval(async function () {
          await cascadeOut();
          table.render((pageIndex + 1) % pages.length);
        }, pageMs);
      },

      stopCycle: function () {
        clearInterval(pageTimer);
        pageTimer = null;
      },

      // Reloads and starts again from the first page, e.g. after a new score is posted.
      refresh: async function () {
        await table.load();
        table.render(0);
        table.startCycle();
      },
    };
    return table;
  };

  return {create};
})();
