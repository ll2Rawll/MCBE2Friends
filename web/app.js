"use strict";

const $ = (selector) => document.querySelector(selector);
const icon = (name) =>
  `<svg class="ico" aria-hidden="true"><use href="#${name}"/></svg>`;
const escapeHTML = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const heading = (symbol, title) =>
  `<div class="section-heading"><span class="tile">${icon(symbol)}</span><h2 tabindex="-1">${title}</h2></div>`;
const secondary = (action, label) =>
  `<button class="secondary" data-action="${action}" type="button">${label}</button>`;
const primary = (action, label) =>
  `<button class="primary" data-action="${action}" type="button">${label}</button>`;
const textButton = (action, label) =>
  `<button class="text-btn" data-action="${action}" type="button">${label}</button>`;
const card = (body, actions = "") =>
  `<div class="card-body">${body}</div>${actions ? `<div class="card-actions">${actions}</div>` : ""}`;
const notice = (type, text) =>
  `<div class="notice ${type}">${icon("alert")} ${escapeHTML(text)}</div>`;
const address = (s) =>
  `${s.host.includes(":") ? "[" + s.host + "]" : s.host}:${s.port}`;

let state;
let view = "launcher";
let editing = false;
let connected = false;
let busy = false;
let closed = false;
let epoch = 0;
let lastCard = "";
let lastEvents = "";
let toastTimer;
let confirmedAction = "";
const nativeWindow = typeof window.runtime?.Quit === "function";
let token = nativeWindow ? "desktop-window" : location.hash.slice(1);
try {
  if (!nativeWindow && token) sessionStorage.setItem("launcher-key", token);
  else if (!nativeWindow) token = sessionStorage.getItem("launcher-key") || "";
} catch {
  /* The current tab can still use the launch URL in private mode. */
}
if (location.hash && token) history.replaceState(null, "", location.pathname);

async function api(path, method = "GET", body) {
  const response = await fetch("/api/" + path, {
    method,
    cache: "no-store",
    headers: {
      Authorization: "Bearer " + token,
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
    signal: AbortSignal.timeout(16000),
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(
      data.error || "The launcher could not complete this action.",
    );
  return data;
}

if (nativeWindow) {
  document.documentElement.classList.add("desktop-app");
  document.addEventListener("click", (event) => {
    const link = event.target.closest?.('a[target="_blank"]');
    if (!link) return;
    event.preventDefault();
    if (link.protocol === "https:") window.runtime.BrowserOpenURL(link.href);
  });
}

function toast(message) {
  clearTimeout(toastTimer);
  $("#toast").textContent = message;
  $("#toast").hidden = false;
  toastTimer = setTimeout(() => {
    $("#toast").hidden = true;
  }, 3500);
}

function fillForm(s) {
  $("#host").value = s.host;
  $("#port").value = s.port || 19132;
  $("#server-name").value = s.name;
  formError("");
}

function formError(message, field) {
  const error = $("#form-error");
  error.textContent = message;
  error.hidden = !message;
  for (const input of $("#server-form").querySelectorAll("input"))
    input.removeAttribute("aria-invalid");
  if (field) {
    field.setAttribute("aria-invalid", "true");
    field.focus();
  }
}

function readPastedPort() {
  const field = $("#host");
  const match = field.value
    .trim()
    .match(/^(?:\[([A-Fa-f0-9:.]+)\]|([^\s/:]+)):(\d+)$/);
  if (match) {
    field.value = match[1] || match[2];
    $("#port").value = match[3];
  }
}

function renderHeader() {
  const phase = state?.phase || "offline";
  const titles = {
    offline: "Broadcast offline",
    preparing: "Setting up",
    connecting: "Connecting",
    signin: "Sign-in needed",
    live: "Broadcast live",
    reconnecting: "Reconnecting",
    stopping: "Stopping",
    error: "Setup needs attention",
  };
  const label = closed
    ? "Launcher closed"
    : !connected
      ? "Disconnected"
      : titles[phase] || "Connecting";
  $("#status-label").textContent = label;
  $("#hero-status").className =
    "status-chip " +
    (!connected || closed
      ? ""
      : phase === "live"
        ? "live"
        : phase === "error"
          ? "fault"
          : phase === "offline"
            ? ""
            : "working");
  $("#page-title").textContent =
    view === "activity"
      ? "Activity log"
      : state?.phase === "live" && connected
        ? "Your broadcast"
        : "Broadcast setup";
  $("#hero-description").textContent =
    view === "activity"
      ? "Follow setup and connection updates from your launcher."
      : "Bring your Bedrock server to the Friends tab in Minecraft.";
  for (const name of ["launcher", "activity", "hosting"]) {
    const element = $("#" + name + "-nav");
    element.classList.toggle("active", view === name);
    if (view === name) element.setAttribute("aria-current", "page");
    else element.removeAttribute("aria-current");
  }
  $("#launcher-page").hidden =
    view !== "launcher" || closed || (!state && !connected);
  $("#activity-page").hidden = view !== "activity" || closed || !state;
  $("#hosting-page").hidden = view !== "hosting" || closed || !state;
  if (view === "hosting") {
    $("#page-title").textContent = "Your game servers";
    $("#hero-description").textContent =
      "Host a world on your PC and manage it from your launcher.";
    if (connected && !closed) {
      $("#status-label").textContent =
        window.premiumPanel?.label() || "Local hosting";
      $("#hero-status").className =
        "status-chip" + (window.premiumPanel?.live() ? " live" : "");
    }
  }
  $(".quit").hidden = !connected || closed;
  const background = connected && !closed ? state?.background : null;
  const warning = background?.warning || "";
  $("#startup-warning").hidden = !warning;
  $("#startup-warning-text").textContent = warning;
  const broadcastPending = background?.broadcast && !state.running;
  const serverPending = background?.server && !state.hostBusy;
  const pendingJava = (background?.java || []).filter(id => !(state?.javaBusy || []).includes(id));
  $("#cancel-java-restart").hidden = !pendingJava.length;
  $("#pending-restart").hidden = !broadcastPending && !serverPending && !pendingJava.length;
  $("#cancel-broadcast-restart").hidden = !broadcastPending;
  $("#cancel-server-restart").hidden = !serverPending;
}

function renderProgress() {
  const saved = Boolean(state.settings.host) && !editing;
  const linked =
    saved &&
    state.phase !== "signin" &&
    (state.accountSaved || (Boolean(state.gamertag) && state.running));
  const done = !saved ? 0 : state.phase === "live" ? 3 : linked ? 2 : 1;
  $("#step-count").textContent = done + " of 3 steps complete";
  document.querySelectorAll(".steps li").forEach((li, index) => {
    const next = index < done ? "done" : index === done ? "current" : "";
    if (li.className !== next || !li.dataset.ready) {
      li.className = next;
      li.dataset.ready = "true";
      li.querySelector(".step-number").innerHTML =
        index < done ? icon("check") : String(index + 1);
      if (index === done) li.setAttribute("aria-current", "step");
      else li.removeAttribute("aria-current");
    }
  });
}

function summary(editable) {
  return `<div class="server-summary">${icon("server")}<div><small>Selected server</small><strong>${escapeHTML(address(state.settings))}</strong></div>${editable ? textButton("edit", "Edit") : ""}</div>`;
}

function accountCard() {
  const saved = state.accountSaved;
  const name =
    saved && state.gamertag
      ? " as <strong>" + escapeHTML(state.gamertag) + "</strong>"
      : "";
  const lead = saved
    ? `Your Xbox sign-in is saved${name}. Start the broadcast when your server is ready.`
    : "Use a dedicated Microsoft account with an Xbox gamertag. Friends will add this account to join your server.";
  return card(
    heading("user", saved ? "Ready to broadcast" : "Connect an Xbox account") +
      `<p class="lead">${lead}</p>` +
      summary(true) +
      `<label class="switch-row"><input type="checkbox" role="switch" id="auto-friend" ${state.settings.autoFriend ? "checked" : ""}>Automatically accept friend requests</label>` +
      (saved
        ? ""
        : '<p class="hint">First launch sets up Java and MCXboxBroadcast, then shows a Microsoft sign-in code.</p>'),
    (saved
      ? textButton("forget", "Disconnect account")
      : `<span class="local-note">${icon("lock")}Sign in with Microsoft</span>`) +
      primary(
        "start",
        saved
          ? `Start broadcast ${icon("radio")}`
          : `Connect &amp; start ${icon("arrow")}`,
      ),
  );
}

function waitCard() {
  const stopping = state.phase === "stopping";
  const label = stopping
    ? "Stopping broadcast"
    : state.phase === "preparing"
      ? "Preparing your launcher"
      : "Connecting to Xbox";
  const helper = stopping
    ? "Your server details will be ready next time you open the launcher."
    : state.phase === "preparing"
      ? "The launcher checks the components on this computer and downloads anything it needs."
      : "Keep this page open for your sign-in code or broadcast confirmation.";
  const progress =
    state.percent >= 0
      ? `<div class="progress-block"><div class="progress-label"><span>${escapeHTML(state.message)}</span><span class="measure" id="percent-text"></span></div><progress id="download-progress" max="100" aria-label="${escapeHTML(state.message)}"></progress></div>`
      : `<p class="busy-line"><span class="spinner" aria-hidden="true"></span>${escapeHTML(state.message)}</p>`;
  return card(
    heading("radio", label) +
      `<p class="lead">${helper}</p>` +
      progress +
      summary(false),
    stopping
      ? ""
      : textButton("activity", "View activity") +
          secondary("stop", "Cancel setup"),
  );
}

function signInCard() {
  return card(
    heading("link", "Sign in with Microsoft") +
      '<p class="lead">Open Microsoft’s sign-in page and enter this code. The broadcast starts after you finish signing in.</p>' +
      `<div class="code-wrap"><div><span class="upper-label">Your sign-in code</span><strong class="code">${escapeHTML(state.code)}</strong></div><button type="button" class="icon-btn" data-action="copy-code" aria-label="Copy sign-in code" title="Copy code">${icon("copy")}</button></div>` +
      '<p class="hint">Use the account you want your friends to add on Xbox.</p>',
    textButton("stop", "Cancel setup") +
      `<a class="primary" href="${escapeHTML(state.signInURL)}" target="_blank" rel="noopener noreferrer">Open Microsoft sign-in ${icon("external")}</a>`,
  );
}

function liveCard() {
  const reconnecting = state.phase === "reconnecting";
  const name = state.gamertag || "the broadcasting account";
  const identity = state.gamertag
    ? `<div class="server-summary">${icon("user")}<div><small>Xbox gamertag</small><strong>${escapeHTML(name)}</strong></div><button class="icon-btn" data-action="copy-gamertag" aria-label="Copy Xbox gamertag">${icon("copy")}</button></div>`
    : "";
  return card(
    heading(
      "radio",
      reconnecting ? "Reconnecting to Xbox" : "Your broadcast is live",
    ) +
      `<p class="lead">${reconnecting ? "MCXboxBroadcast is restoring the Xbox session. Friends may need to wait before joining." : "Xbox Live has confirmed the session for " + escapeHTML(address(state.settings)) + "."}</p>` +
      identity +
      (reconnecting
        ? ""
        : `<ol class="join-steps"><li>Have friends add <strong>${escapeHTML(name)}</strong> on Xbox.</li><li>${state.settings.autoFriend ? "Wait for the friend request to be accepted." : "Accept their requests on the broadcasting account."}</li><li>In Minecraft Bedrock, open <strong>Play → Friends</strong> and select the session.</li></ol>`) +
      (state.serverWarning ? notice("warn", state.serverWarning) : ""),
    `<span class="local-note">${icon("monitor")}Runs in the background. Keep this PC awake.</span>` +
      secondary("stop", icon("stop") + " Stop broadcast"),
  );
}

function errorCard() {
  return card(
    heading("alert", "Broadcast needs attention") +
      notice("problem", state.message) +
      summary(true) +
      (state.accountSaved
        ? textButton("forget", "Disconnect saved account")
        : ""),
    textButton("activity", "View activity") +
      primary("start", "Try again " + icon("arrow")),
  );
}

function renderWork() {
  const server = !state.settings.host || (editing && !state.running);
  $("#loading").hidden = true;
  $("#server-panel").hidden = !server;
  $("#dynamic-panel").hidden = server;
  $("#work-panel").inert = closed || !connected;
  if (server) {
    lastCard = "";
    $("#back-edit").hidden = !state.settings.host;
    $("#server-panel .local-note").hidden = Boolean(state.settings.host);
    return;
  }
  const key = JSON.stringify([
    state.phase,
    state.message,
    state.percent >= 0,
    state.settings,
    state.accountSaved,
    state.gamertag,
    state.code,
    state.signInURL,
    state.serverWarning,
  ]);
  if (key !== lastCard) {
    let html;
    switch (state.phase) {
      case "offline":
        html = accountCard();
        break;
      case "preparing":
      case "connecting":
      case "stopping":
        html = waitCard();
        break;
      case "signin":
        html = signInCard();
        break;
      case "live":
      case "reconnecting":
        html = liveCard();
        break;
      case "error":
        html = errorCard();
        break;
      default:
        html = card(heading("radio", "Connecting to the launcher"));
    }
    $("#dynamic-panel").innerHTML = html;
    lastCard = key;
  }
  if ($("#download-progress")) {
    $("#download-progress").value = state.percent;
    $("#percent-text").textContent = state.percent + "%";
  }
}

const clock = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});
function renderEvents() {
  const events = state.events || [];
  const key = events.length + "-" + events.at(-1)?.time;
  if (key === lastEvents) return;
  lastEvents = key;
  $("#events").innerHTML = [...events]
    .reverse()
    .map((event) => {
      const symbol =
        event.kind === "success"
          ? "check"
          : event.kind === "info"
            ? "activity"
            : "alert";
      return `<li class="event-row"><span aria-label="${escapeHTML(event.kind)}" class="event-icon ${escapeHTML(event.kind)}">${icon(symbol)}</span><span>${escapeHTML(event.text)}</span><time class="event-time" datetime="${escapeHTML(event.time)}">${escapeHTML(clock.format(new Date(event.time)))}</time></li>`;
    })
    .join("");
}

function render() {
  renderHeader();
  if (!state || closed) return;
  renderProgress();
  renderWork();
  if (view === "activity") renderEvents();
  if (view === "hosting" && connected) window.premiumPanel?.tick();
}

function accept(snapshot) {
  if (!state) fillForm(snapshot.settings);
  state = snapshot;
  connected = true;
  $("#connection-notice").hidden = true;
  render();
}

function disconnect(message) {
  connected = false;
  $("#connection-notice").textContent = message;
  $("#connection-notice").hidden = false;
  render();
}

async function refresh() {
  if (busy || closed || !token) return;
  const revision = epoch;
  try {
    const snapshot = await api("state");
    if (revision === epoch && !closed) accept(snapshot);
  } catch (error) {
    if (revision === epoch && !closed)
      disconnect(
        error.message.includes("Reopen")
          ? error.message
          : "Connection to the local launcher was lost. Reopen Mcbe2Friends on this computer to reconnect.",
      );
  }
}

async function poll() {
  await refresh();
  if (!closed && token)
    setTimeout(
      poll,
      document.hidden || (!state?.running && !state?.hostBusy && !state?.javaBusy?.length) ? 2000 : 750,
    );
}

function changeView(name) {
  view = name;
  render();
}

async function perform(operation) {
  if (busy || closed || !connected) return;
  busy = true;
  epoch++;
  $("#work-panel").setAttribute("aria-busy", "true");
  document
    .querySelectorAll("#work-panel button, #work-panel input")
    .forEach((el) => {
      el.disabled = true;
    });
  try {
    await operation();
  } catch (error) {
    if (!$("#server-panel").hidden) formError(error.message);
    else toast(error.message);
  } finally {
    busy = false;
    $("#work-panel").removeAttribute("aria-busy");
    document
      .querySelectorAll("#work-panel button, #work-panel input")
      .forEach((el) => {
        el.disabled = false;
      });
  }
}

function focusCard() {
  const title = $("#server-panel").hidden
    ? $("#dynamic-panel h2")
    : $("#server-title");
  title?.focus({ preventScroll: true });
}

$("#host").addEventListener("change", readPastedPort);
$("#server-form").addEventListener("input", () => formError(""));
$("#server-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  readPastedPort();
  const host = $("#host").value.trim();
  const port = $("#port").value.trim();
  if (!host) return formError("Enter your server’s address.", $("#host"));
  if (!/^\d{1,5}$/.test(port) || Number(port) < 1 || Number(port) > 65535)
    return formError("Use a port from 1 to 65535.", $("#port"));
  const value = {
    host,
    port: Number(port),
    name: $("#server-name").value.trim(),
    autoFriend: state.settings.autoFriend,
  };
  await perform(async () => {
    const snapshot = await api("settings", "POST", value);
    editing = false;
    accept(snapshot);
    focusCard();
  });
});

function openConfirm(title, body, label, action) {
  $("#confirm-title").textContent = title;
  $("#confirm-body").textContent = body;
  $("#confirm-action").textContent = label;
  confirmedAction = action;
  $("#confirm-dialog").showModal();
}

async function quit() {
  if (nativeWindow) {
    window.runtime.Quit();
    return;
  }
  const result = await api("quit", "POST");
  closed = true;
  connected = false;
  renderHeader();
  $("#connection-notice").textContent =
    result.quitting
      ? "You can close this tab. Open Mcbe2Friends whenever you’re ready to play."
      : "You can close this tab. Reopen Mcbe2Friends to manage your background services.";
  $("#connection-notice").className = "notice";
  $("#connection-notice").hidden = false;
}

async function copy(text) {
  try {
    if (nativeWindow) await window.runtime.ClipboardSetText(text);
    else await navigator.clipboard.writeText(text);
    toast("Copied to clipboard.");
  } catch {
    toast("Select the text and use the Copy command.");
  }
}

document.addEventListener("click", async (event) => {
  const element = event.target.closest("[data-action]");
  if (!element) return;
  const action = element.dataset.action;
  switch (action) {
    case "launcher":
    case "activity":
    case "hosting":
      changeView(action);
      break;
    case "guide":
    case "join-guide":
      if (action === "join-guide") $("#join-detail").open = true;
      $("#guide-dialog").showModal();
      break;
    case "close-dialog":
      element.closest("dialog")?.close();
      break;
    case "edit":
      editing = true;
      fillForm(state.settings);
      render();
      $("#host").focus();
      break;
    case "cancel-edit":
      editing = false;
      render();
      focusCard();
      break;
    case "start":
      await perform(async () => {
        const acceptFriends = $("#auto-friend");
        if (
          acceptFriends &&
          acceptFriends.checked !== state.settings.autoFriend
        ) {
          accept(
            await api("settings", "POST", {
              ...state.settings,
              autoFriend: acceptFriends.checked,
            }),
          );
        }
        accept(await api("start", "POST"));
        focusCard();
      });
      break;
    case "stop":
      await perform(async () => accept(await api("stop", "POST")));
      break;
    case "copy-code":
      await copy(state.code);
      break;
    case "copy-gamertag":
      await copy(state.gamertag);
      break;
    case "forget":
      openConfirm(
        "Disconnect this account?",
        "This removes the saved sign-in from this computer. Your server settings stay saved.",
        "Disconnect",
        "forget",
      );
      break;
    case "retry-startup":
      await perform(async () => accept(await api("startup/retry", "POST")));
      break;
    case "cancel-java-restart":
      await perform(async () => {
        const current = await api("state");
        for (const id of current.background.java || []) {
          if (!(current.javaBusy || []).includes(id)) await api("hosting/java/"+id+"/stop", "POST");
        }
        accept(await api("state"));
      });
      break;
    case "cancel-server-restart":
      await perform(async () => {
        await api("hosting/stop", "POST");
        accept(await api("state"));
      });
      break;
    case "quit":
      await perform(quit);
      break;
  }
});

// The guide becomes a bottom sheet on narrow screens. Only its header
// drags, so long help articles scroll normally inside the sheet.
const guide = $("#guide-dialog");
const sheetHeader = guide.querySelector(".dialog-heading");
let sheetDrag;
sheetHeader.addEventListener("pointerdown", (event) => {
  if (
    !matchMedia("(max-width: 760px)").matches ||
    !event.isPrimary ||
    event.button !== 0 ||
    event.target.closest("button")
  )
    return;
  sheetDrag = { id: event.pointerId, startY: event.clientY };
  sheetHeader.setPointerCapture(event.pointerId);
});
sheetHeader.addEventListener("pointermove", (event) => {
  if (sheetDrag?.id !== event.pointerId) return;
  const offset = Math.max(0, event.clientY - sheetDrag.startY);
  guide.style.transform = "translateY(" + offset + "px)";
});
sheetHeader.addEventListener("pointerup", (event) => {
  if (sheetDrag?.id !== event.pointerId) return;
  if (event.clientY - sheetDrag.startY > 70) guide.close();
});
function resetSheet() {
  sheetDrag = undefined;
  guide.style.removeProperty("transform");
}
sheetHeader.addEventListener("lostpointercapture", resetSheet);
guide.addEventListener("close", resetSheet);

$("#confirm-action").addEventListener("click", async () => {
  $("#confirm-dialog").close();
  if (confirmedAction === "forget")
    await perform(async () => {
      accept(await api("forget", "POST"));
      focusCard();
    });
  confirmedAction = "";
});

const back = document.createElement("button");
back.type = "button";
back.className = "text-btn";
back.dataset.action = "cancel-edit";
back.id = "back-edit";
back.textContent = "Cancel edit";
back.hidden = true;
$("#server-panel .card-actions").prepend(back);
if (token) poll();
else
  disconnect(
    "Open the Mcbe2Friends desktop launcher on this computer to begin setup.",
  );
