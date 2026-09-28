// Copyright 2026 Team 254. All Rights Reserved.
//
// Client-side logic for the hub's Event Control page.

var websocket;

const escapeHtml = function (text) {
  return $("<div>").text(text === undefined || text === null ? "" : String(text)).html();
};

const formatAge = function (timestamp) {
  const time = new Date(timestamp);
  if (isNaN(time.getTime()) || time.getFullYear() < 2000) {
    return "never";
  }
  const seconds = Math.round((Date.now() - time.getTime()) / 1000);
  if (seconds < 2) {
    return "now";
  }
  return seconds < 120 ? `${seconds}s ago` : `${Math.round(seconds / 60)} min ago`;
};

// Describes how far a node's clock is from the hub's; small differences don't matter, so they aren't shown.
const formatClockOffset = function (offsetMs) {
  if (Math.abs(offsetMs) < 1000) {
    return "in sync";
  }
  const seconds = (offsetMs / 1000).toFixed(1);
  return `${offsetMs > 0 ? "+" : ""}${seconds} s`;
};

// Copies text to the clipboard. The Clipboard API only works on https:// or localhost pages, and the hub is usually
// opened by its IP address, so fall back to a temporary text box.
const copyText = function (text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise(function (resolve, reject) {
    const textArea = $("<textarea>").val(text).css({position: "fixed", top: 0, left: 0, opacity: 0});
    $("body").append(textArea);
    textArea[0].select();
    const copied = document.execCommand("copy");
    textArea.remove();
    copied ? resolve() : reject();
  });
};

// Renders the per-field overview cards.
const renderFields = function (fields) {
  const container = $("#fields");
  if (!fields || fields.length === 0) {
    container.html(`<div class="col text-body-secondary">No fields have connected yet.</div>`);
    return;
  }
  container.html(fields.map(function (field) {
    const upcoming = (field.Upcoming || []).map(function (match) {
      const time = new Date(match.Time).toLocaleTimeString([], {hour: "numeric", minute: "2-digit"});
      const teams = (match.TeamIds || []).filter(id => id > 0).join(", ");
      return `<tr><td>${escapeHtml(match.ShortName)}</td><td>${time}</td><td class="small">${teams}</td></tr>`;
    }).join("");
    const minutes = Math.floor(field.MatchTimeSec / 60);
    const seconds = String(field.MatchTimeSec % 60).padStart(2, "0");
    const connected = field.Connected
      ? `<span class="badge text-bg-success">connected</span>`
      : `<span class="badge text-bg-danger">offline</span>`;
    return `
      <div class="col-lg-6">
        <div class="card card-body bg-body-tertiary">
          <div class="d-flex justify-content-between align-items-center">
            <h4 class="mb-0">${escapeHtml(field.FieldName)}</h4>
            ${connected}
          </div>
          <div class="display-6 my-2">${escapeHtml(field.CurrentMatchName || "No match loaded")}</div>
          <div>
            <b>${escapeHtml(field.MatchStateName || "")}</b>
            <span class="ms-2">${minutes}:${seconds}</span>
            <span class="ms-2 text-body-secondary">${escapeHtml(field.EarlyLateMessage || "")}</span>
          </div>
          <table class="table table-sm mt-2 mb-0">
            <thead><tr><th>Upcoming</th><th>Time</th><th>Teams</th></tr></thead>
            <tbody>${upcoming || `<tr><td colspan="3" class="text-body-secondary">None assigned</td></tr>`}</tbody>
          </table>
        </div>
      </div>`;
  }).join(""));
};

// Renders the node status table.
let lastNodesHtml = "";
const renderNodes = function (nodes) {
  const rows = (nodes || []).map(function (node) {
    let status;
    if (!node.Approved) {
      status = `<span class="badge text-bg-warning">awaiting approval</span>`;
    } else if (node.Connected) {
      status = `<span class="badge text-bg-success">connected</span>`;
    } else {
      status = `<span class="badge text-bg-danger">disconnected</span>`;
    }
    const clockClass = node.ClockWarning ? "text-danger fw-bold" : "";
    const clockWarning = node.ClockWarning
      ? ` <span title="Set this laptop's clock to match the hub's; match times and team rest use it.">&#9888;</span>`
      : "";
    const behind = (node.VersionsBehind || []).length;
    const mirror = behind === 0
      ? `<span class="text-success">in sync</span>`
      : `<span class="text-warning" title="${escapeHtml(node.VersionsBehind.join(", "))}">updating (${behind})</span>`;
    const duplicate = node.DuplicateFrom
      ? `<div class="text-danger small fw-semibold">Another laptop (${escapeHtml(node.DuplicateFrom)}) also says it's
          Field ${node.FieldId}; check each laptop's Field ID.</div>`
      : "";
    const revoke = `return confirm('Revoke Field ${node.FieldId}? It stops receiving updates and can\\'t send ` +
      `results until it is approved again.');`;
    const approveButton = node.Approved
      ? `<form class="d-inline" method="POST" action="/event_control/nodes/${node.FieldId}/revoke"
          onsubmit="${revoke}"><button type="submit" class="btn btn-sm btn-outline-secondary">Revoke</button></form>`
      : `<form class="d-inline" method="POST" action="/event_control/nodes/${node.FieldId}/approve">
          <button type="submit" class="btn btn-sm btn-success">Approve</button></form>`;
    return `
      <tr>
        <td>${node.FieldId} &middot; ${escapeHtml(node.FieldName)}<br>
          <small class="text-body-secondary">${escapeHtml(node.RemoteAddr)}</small>${duplicate}</td>
        <td>${status}</td>
        <td class="${clockClass}" title="${node.ClockOffsetMs} ms">${formatClockOffset(node.ClockOffsetMs)}${clockWarning}</td>
        <td>${node.OutboxSize}${node.Report && node.Report.RejectedCount > 0
          ? ` <span class="badge text-bg-danger">${node.Report.RejectedCount} rejected</span>` : ""}</td>
        <td>${formatAge(node.LastContact)}</td>
        <td>${mirror}</td>
        <td class="text-nowrap">
          ${approveButton}
          <form class="d-inline" method="POST" action="/event_control/nodes/${node.FieldId}/forget"
            onsubmit="return confirm('Forget Field ${node.FieldId}? It will need to be approved again.');">
            <button type="submit" class="btn btn-sm btn-outline-danger">Forget</button>
          </form>
        </td>
      </tr>`;
  }).join("");
  const html = rows || `<tr><td colspan="7" class="text-body-secondary">No nodes have contacted the hub.</td></tr>`;
  // Status arrives every second, so only redraw when something visible changed; otherwise the buttons would be
  // replaced under the mouse and clicks could be lost.
  if (html !== lastNodesHtml) {
    lastNodesHtml = html;
    $("#nodes").html(html);
  }
};

// Reloads the checklist from the server, at most every couple of seconds, and never while the user is typing in it.
let checklistTimer = null;

// Returns true if the user is filling in or has opened something in the checklist, which a refresh would wipe out.
const checklistInUse = function (checklist) {
  const typing = checklist.find("input").filter(function () {
    return $(this).val() !== "";
  }).length > 0;
  return typing || checklist.find(":focus").length > 0 || checklist.find("details[open]").length > 0 ||
    checklist.find(".secret.revealed").length > 0;
};

const refreshChecklist = function () {
  if (checklistTimer !== null) {
    return;
  }
  checklistTimer = setTimeout(function () {
    checklistTimer = null;
    const checklist = $("#checklist");
    if (checklistInUse(checklist)) {
      return;
    }
    $.get("/event_control/checklist", function (html) {
      // Only touch the page if something visible changed, so that buttons don't move under the user's mouse.
      const normalize = text => text.replace(/\s+/g, " ").trim();
      if (normalize($("<div>").html(html).text()) !== normalize(checklist.text()) && !checklistInUse(checklist)) {
        checklist.html(html);
      }
    });
  }, 2000);
};

const handleHubStatus = function (data) {
  refreshChecklist();
  renderFields(data.Fields);
  renderNodes(data.Nodes);
  const conflicts = (data.Conflicts || []).slice().reverse();
  $("#conflicts").html(
    conflicts.length === 0
      ? `<li class="text-body-secondary">None</li>`
      : conflicts.map(conflict => `<li>${escapeHtml(conflict)}</li>`).join("")
  );
};

$(function () {
  if ($("#fields").length === 0) {
    return;
  }
  $(document).on("click", ".copy-button", function () {
    const button = $(this);
    copyText(button.attr("data-copy")).then(function () {
      button.text("copied");
      setTimeout(() => button.text("copy"), 1500);
    }, function () {
      button.text("select and copy it by hand");
    });
  });
  // Shows or hides the shared secret everywhere on the page.
  $(document).on("click", ".reveal-secret", function () {
    const reveal = $(".secret.revealed").length === 0;
    $(".secret").each(function () {
      $(this).toggleClass("revealed", reveal).text(reveal ? $(this).attr("data-secret") : "••••••••");
    });
    $(".reveal-secret").text(reveal ? "hide" : "show");
  });
  // Also refresh periodically, since some steps (e.g. schedule, awards) change without a hub status update.
  setInterval(refreshChecklist, 10000);
  websocket = new CheesyWebsocket("/event_control/websocket", {
    hubStatus: function (event) {
      handleHubStatus(event.data);
    },
    eventStatus: function (event) {
    },
  });
});
