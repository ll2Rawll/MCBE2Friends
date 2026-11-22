"use strict";

window.premiumPanel = (() => {
  const el = (id) => document.getElementById(id);
  const labels = {
    offline: "Server offline",
    installing: "Installing Bedrock",
    starting: "Starting server",
    running: "Server running",
    stopping: "Saving world",
    error: "Server needs attention",
    unreachable: "Check server connection",
  };
  const form = el("host-settings");
  let host,
    library,
    updating = false,
    working = false,
    lastRead = 0,
    lastLog = "",
    revision = 0;
  const numberFields = ["port", "players", "view", "tick", "threads"];
  function feedback(text, error = false) {
    const node = el("host-feedback");
    node.textContent = text;
    node.hidden = !text;
    node.classList.toggle("error", error);
  }
  function applyStatus(next) {
    host = next;
    const hw = host.hardware;
    const canHost = hw.supported || hw.javaSupported;
    window.javaPanel.setHost(host);
    const ram = hw.ramGiB
      ? " · " + Number(hw.ramGiB.toFixed(1)) + " GiB RAM"
      : "";
    el("host-pc").textContent =
      hw.platform + " · " + hw.cores + " logical CPUs" + ram;
    const rec = host.recommended;
    el("host-capability").textContent =
      window.javaPanel.isSelected()
        ? "The launcher sets up the Java runtime for your selected Minecraft version on first start."
        : ((!host.licensed && hw.javaSupported ? "Java hosting is available on this computer. " : "") + hw.note) +
      (hw.supported
        ? " " +
          hw.tier +
          " preset recommends " +
          rec.players +
          " slots and " +
          rec.view +
          " chunks of view distance."
        : "");
    el("device-code").value = host.device;
    el("host-locked").hidden = host.licensed || !canHost;
    el("host-unlocked").hidden = !host.licensed;
    el("host-buy").hidden = !canHost || !host.device;
    el("activate-premium").hidden = !canHost || !host.device;
    const verifying = working || host.checking;
    el("confirm-premium").disabled = verifying;
    el("confirm-premium").textContent = verifying ? "Verifying…" : "Activate";
    el("retry-license").disabled = verifying;
    el("retry-license").hidden =
      !canHost || !host.savedKey || !host.licenseMessage;
    el("host-phase").textContent = labels[host.phase] || "Server offline";
    el("host-message").textContent =
      host.message ||
      "Start with the recommended settings or adjust them below.";
    el("host-version").textContent = "Bedrock " + host.version;
    el("host-terms-row").hidden = host.terms;
    el("host-start").hidden = host.busy;
    el("host-start").disabled =
      working || !hw.supported || (!host.terms && !el("host-terms").checked);
    el("host-start").textContent = host.installed
      ? "Start server"
      : "Install & start server";
    el("host-stop").hidden = !host.busy;
    el("host-stop").disabled = working || host.phase === "stopping";
    el("host-folder").disabled = !host.installed;
    el("host-progress").hidden = host.phase !== "installing";
    if (host.percent >= 0) el("host-progress").value = host.percent;
    else el("host-progress").removeAttribute("value");
    el("host-config-fields").disabled = host.busy || working;
    for (const id of ["host-pack-import", "host-world-import"]) {
      el(id)
        .querySelectorAll("input,button")
        .forEach((n) => {
          n.disabled = working || host.busy || !host.installed;
        });
    }
    for (const id of ["host-backup", "host-backup-folder"])
      el(id).disabled =
        !host.installed || (id === "host-backup" && (host.busy || working));
    el("host-packs")
      .querySelectorAll("button")
      .forEach((n) => {
        n.disabled = host.busy || working;
      });
    for (const id of ["host-command", "host-player-form"])
      el(id)
        .querySelectorAll("button,input")
        .forEach((n) => {
          n.disabled =
            working || !["running", "unreachable"].includes(host.phase);
        });
    const port = library?.config.port ?? rec.port;
    el("host-local").textContent =
      host.lan && host.phase === "running"
        ? "On your network, join " +
          host.lan +
          " on port " +
          port +
          ". You can close the launcher. Keep this PC awake."
        : "You can close the launcher while friends play. The server resumes at your next sign-in until you choose Stop server.";
    const lines = (host.log || []).join("\n");
    if (lines !== lastLog || !el("host-log").textContent) {
      const box = el("host-log"),
        atEnd = box.scrollTop + box.clientHeight >= box.scrollHeight - 8;
      lastLog = lines;
      box.textContent = lines || "Server output will appear here.";
      if (atEnd) box.scrollTop = box.scrollHeight;
    }
    if (view === "hosting" && connected) {
      el("status-label").textContent = host.licensed
        ? window.javaPanel.label() || labels[host.phase]
        : "Local hosting";
      el("hero-status").className =
        "status-chip" + ((window.javaPanel.isSelected() ? window.javaPanel.live() : host.phase === "running") ? " live" : "");
    }
  }
  function fillSettings(config) {
    for (const [k, v] of Object.entries(config)) {
      const f = form.elements.namedItem(k);
      if (!f) continue;
      if (f.type === "checkbox") f.checked = v;
      else f.value = v;
    }
  }
  function listText(id, items, blank) {
    const list = el(id);
    const nodes = (items?.length ? items : [blank]).map((t) => {
      const li = document.createElement("li");
      li.textContent = t;
      return li;
    });
    list.replaceChildren(...nodes);
  }
  async function readFiles() {
    if (!host?.hardware.supported) return;
    library = await api("hosting/files");
    fillSettings(library.config);
    el("host-world-names").replaceChildren(
      ...(library.worlds || []).map((name) => {
        const opt = document.createElement("option");
        opt.value = name;
        return opt;
      }),
    );
    el("host-allowlist").textContent =
      "Allowed players: " +
      ((library.allowedPlayers || []).join(", ") || "None added yet.");
    const list = el("host-packs");
    list.replaceChildren();
    if (!library.packs?.length)
      listText("host-packs", [], "No player add-ons installed.");
    for (const p of library.packs || []) {
      const li = document.createElement("li"),
        name = document.createElement("strong"),
        desc = document.createElement("div");
      name.textContent = p.name;
      desc.textContent =
        (p.kind === "behavior_packs" ? "Behavior" : "Resource") +
        " · " +
        p.version.join(".") +
        (p.worldOnly ? " · World pack" : "") +
        " · " +
        (p.enabled ? "Enabled in " : "Disabled in ") +
        library.config.world;
      li.append(name, desc);
      for (const action of p.managed ? ["toggle", "remove"] : ["toggle"]) {
        const b = document.createElement("button");
        b.type = "button";
        b.className = action === "toggle" ? "secondary" : "text-btn";
        b.textContent =
          action === "remove" ? "Remove" : p.enabled ? "Disable" : "Enable";
        b.addEventListener("click", () => {
          if (
            action === "remove" &&
            !confirm("Remove " + p.name + " from the server’s add-on library?")
          )
            return;
          run(async () => {
            await post("pack", {
              id: p.id,
              enabled: !p.enabled,
              remove: action === "remove",
            });
            await readFiles();
            feedback("Pack library updated.");
          });
        });
        li.append(b);
      }
      list.append(li);
    }
    listText("host-backups", library.backups, "No world backups yet.");
    applyStatus(host);
  }
  async function tick() {
    if (
      updating ||
      working ||
      !connected ||
      closed ||
      Date.now() - lastRead < 650
    )
      return;
    updating = true;
    const ticket = revision;
    try {
      const next = await api("hosting");
      if (working || ticket !== revision) return;
      const reload =
        next.licensed &&
        (!library ||
          next.installed !== host?.installed ||
          (host?.busy && !next.busy));
      const previous = host?.licenseMessage;
      const wasChecking = host?.checking;
      applyStatus(next);
      if (next.licenseMessage && previous !== next.licenseMessage)
        feedback(next.licenseMessage, true);
      if (next.checking) feedback("Checking the saved key…");
      if (next.licensed && !next.licenseMessage && (previous || wasChecking)) feedback("");
      if (!next.licensed) library = null;
      if (reload && next.hardware.supported) await readFiles();
      if (next.licensed && window.javaPanel.isSelected()) await window.javaPanel.tick();
    } catch (e) {
      feedback(e.message, true);
    } finally {
      updating = false;
      lastRead = Date.now();
    }
  }
  async function run(fn) {
    if (working || !connected || closed) return;
    working = true;
    revision++;
    feedback("");
    if (host) applyStatus(host);
    try {
      await fn();
    } catch (e) {
      feedback(e.message, true);
    } finally {
      working = false;
      if (host) applyStatus(host);
      lastRead = 0;
    }
  }
  async function post(action, data = {}) {
    const next = await api("hosting/" + action, "POST", data);
    applyStatus(next);
  }
  async function upload(action, file, param = "") {
    const reply = await fetch(apiURL("hosting/" + action + param), {
      method: "POST",
      headers: {
        Authorization: "Bearer " + token,
        "Content-Type": "application/octet-stream",
      },
      body: file,
    });
    const data = await reply.json();
    if (!reply.ok) throw new Error(data.error || "Unable to import the file.");
    applyStatus(data);
  }
  function submit(id, fn) {
    el(id).addEventListener("submit", (event) => {
      event.preventDefault();
      const sender = event.submitter;
      run(() => fn(sender));
    });
  }
  function click(id, fn) {
    el(id).addEventListener("click", () => run(fn));
  }
  el("copy-device").addEventListener("click", () =>
    copy(el("device-code").value),
  );
  el("host-terms").addEventListener("change", () => {
    if (host) applyStatus(host);
  });
  submit("activate-premium", async () => {
    const key = el("premium-key").value.trim();
    if (!key) throw new Error("Paste your activation key.");
    await post("activate", { key });
    el("premium-key").value = "";
    await readFiles();
    feedback("Premium activated on this PC.");
  });
  click("retry-license", async () => {
    await post("activate", { key: "" });
    await readFiles();
    feedback("Premium activated.");
  });
  click("host-start", async () => {
    await post("start", { acceptTerms: el("host-terms").checked });
  });
  click("host-stop", async () => {
    await post("stop");
  });
  click("host-folder", () => post("folder", { backups: false }));
  click("host-backup-folder", () => post("folder", { backups: true }));
  click("host-refresh", async () => {
    await readFiles();
    feedback("Loaded the current server files.");
  });
  el("host-preset").addEventListener("click", () => {
    for (const k of ["players", "view", "tick", "threads"])
      form.elements.namedItem(k).value = host.recommended[k];
    feedback("Review the recommendations and save your settings.");
  });
  submit("host-settings", async () => {
    const values = {};
    for (const field of ["name", "world", "mode", "difficulty"])
      values[field] = form.elements.namedItem(field).value.trim();
    for (const field of numberFields)
      values[field] = Number(form.elements.namedItem(field).value);
    for (const field of ["cheats", "allowList"])
      values[field] = form.elements.namedItem(field).checked;
    await post("settings", values);
    await readFiles();
    feedback("Settings saved for the next start.");
  });
  submit("host-pack-import", async () => {
    const f = el("pack-file").files[0];
    if (!f) return;
    feedback("Importing add-on…");
    await upload("import-pack", f);
    el("pack-file").value = "";
    await readFiles();
    feedback("Imported. Enable the packs you want for this world.");
  });
  submit("host-world-import", async () => {
    const f = el("world-file").files[0];
    if (!f) return;
    const name =
      el("world-import-name").value.trim() ||
      f.name.replace(/\.(mcworld|zip)$/i, "");
    feedback("Importing world…");
    await upload("import-world", f, "?name=" + encodeURIComponent(name));
    await readFiles();
    el("host-world-import").reset();
    feedback(
      "Imported " + name + ". Choose it under Server settings & worlds.",
    );
  });
  click("host-backup", async () => {
    feedback("Saving the world backup…");
    const res = await fetch(apiURL("hosting/backup"), {
      method: "POST",
      headers: {
        Authorization: "Bearer " + token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ name: library.config.world }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error);
    applyStatus(data);
    await readFiles();
    feedback("World backup saved.");
  });
  submit("host-command", async () => {
    await post("command", { command: el("host-command-text").value });
    el("host-command-text").value = "";
    feedback("Command sent. See the server console for its response.");
  });
  submit("host-player-form", async (button) => {
    await post("player", {
      name: el("host-gamertag").value,
      remove: button?.value === "remove",
    });
    el("host-gamertag").value = "";
    await new Promise((r) => setTimeout(r, 450));
    await readFiles();
    feedback("Allowlist command sent. See the console for the result.");
  });
  el("host-broadcast").addEventListener("click", () => {
    if (!library || !host) return;
    if (state.running) {
      changeView("launcher");
      return;
    }
    const addr = el("host-public").value.trim() || host.lan;
    if (!addr) {
      feedback("Enter the address players will use to reach this PC.", true);
      return;
    }
    editing = true;
    changeView("launcher");
    fillForm({
      ...state.settings,
      host: addr,
      port: library.config.port,
      name: library.config.name,
    });
    el("host").focus();
  });
  return {
    tick,
    label: () => (host?.licensed ? window.javaPanel.label() || labels[host.phase] : "Local hosting"),
    live: () => window.javaPanel.isSelected() ? window.javaPanel.live() : host?.phase === "running",
    busy: () => Boolean(host?.busy),
  };
})();
