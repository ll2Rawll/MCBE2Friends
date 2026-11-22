"use strict";

window.javaPanel = (() => {
  const el = (id) => document.getElementById(id);
  const settings = el("java-settings");
  const field = (name) => settings.elements.namedItem(name);
  const phases = {
    offline: "Offline",
    installing: "Installing",
    starting: "Starting",
    running: "Running",
    stopping: "Saving world",
    unreachable: "Check connection",
    error: "Needs attention",
  };
  const numeric = [
    "port",
    "bedrockPort",
    "memory",
    "players",
    "view",
    "simulation",
  ];
  let host,
    library,
    current,
    files,
    mods,
    selected = "",
    edition = "bedrock";
  let locked = false,
    loading = false,
    revision = 0;
  const active = () => edition === "java" && Boolean(host?.licensed);
  const path = (action, id = selected) => "hosting/java/" + id + "/" + action;
  const post = (action, body = {}) => api(path(action), "POST", body);

  function feedback(message, error = false) {
    const box = el("host-feedback");
    box.textContent = message;
    box.hidden = !message;
    box.classList.toggle("error", error);
  }
  function options(node, entries, value) {
    node.replaceChildren(
      ...entries.map(([key, label]) => new Option(label, key)),
    );
    if (value !== undefined) node.value = value;
  }
  function displayEdition() {
    el("edition-bedrock").setAttribute(
      "aria-pressed",
      String(edition === "bedrock"),
    );
    el("edition-java").setAttribute("aria-pressed", String(edition === "java"));
    el("java-controls").hidden = edition !== "java";
    el("bedrock-controls").hidden = edition !== "bedrock";
  }
  function setHost(next) {
    host = next;
    el("edition-bedrock").hidden = !host.hardware.supported;
    el("edition-java").hidden = !host.hardware.javaSupported;
    if (!host.hardware.supported && host.hardware.javaSupported)
      edition = "java";
    if (!host.licensed) {
      current = library = files = mods = null;
      window.fabricMods.setProfile(null, null, true);
      selected = "";
      el("java-instance").hidden = true;
    }
    displayEdition();
  }
  function selectEdition(next) {
    if (locked) return;
    edition = next;
    revision++;
    displayEdition();
    feedback("");
    if (active()) tick();
  }
  el("edition-java").onclick = () => selectEdition("java");
  el("edition-bedrock").onclick = () => selectEdition("bedrock");

  function buttons(form, disabled) {
    form.querySelectorAll("input,select,button").forEach((node) => {
      node.disabled = disabled;
    });
  }
  function updateControls() {
    const busy = Boolean(current?.busy),
      ready = ["running", "unreachable"].includes(current?.phase);
    buttons(el("java-create"), locked);
    const version = library?.versions.find(
      (v) => v.id === el("java-new-version").value,
    );
    if (version) {
      const form = el("java-create");
      for (const [engine, available] of [
        ["paper", version.paper],
        ["crossplay", version.geyser],
      ]) {
        const radio = form.querySelector('input[value="' + engine + '"]');
        radio.disabled = locked || !available;
        if (!available && radio.checked)
          form.querySelector('input[value="vanilla"]').checked = true;
      }
      const detail = ["Java " + version.java + " is set up automatically."];
      if (!version.paper) detail.push("Paper has no build for this release.");
      else if (version.paperChannel !== "STABLE")
        detail.push(
          "Paper marks its build as " +
            version.paperChannel.toLowerCase() +
            ".",
        );
      if (!version.geyser)
        detail.push(
          "Managed cross-play needs a Paper build for 1.20.5 or newer.",
        );
      else if (
        form.querySelector('input[value="crossplay"]').checked &&
        version.id !== library.version
      )
        detail.push(
          "ViaVersion connects current Bedrock players to this older Paper world.",
        );
      el("java-version-detail").textContent = detail.join(" ");
    }
    window.fabricMods.setProfile(current, mods, locked);
    el("java-create-start").disabled = locked || !el("java-new-eula").checked;
    el("java-select").disabled = locked;
    if (!current) return;
    el("java-start").hidden = busy;
    el("java-stop").hidden = !busy;
    el("java-stop").disabled = locked || current.phase === "stopping";
    el("java-fields").disabled = locked || busy;
    el("java-eula-row").hidden = current.terms;
    el("java-start").disabled =
      locked || (!current.terms && !el("java-eula").checked);
    buttons(el("java-player"), locked || !ready);
    buttons(el("java-command"), locked || !ready);
    for (const id of ["java-import-plugin", "java-world-import"])
      buttons(el(id), locked || busy);
    const backedUp = Boolean(files?.backups?.length);
    buttons(el("java-restore"), locked || busy || !backedUp);
    el("java-backup").disabled =
      locked || busy || !(files?.worlds || []).includes(current.config.world);
    el("java-plugins")
      .querySelectorAll("button")
      .forEach((n) => (n.disabled = locked || busy));
  }
  function fill(c) {
    for (const [k, v] of Object.entries(c)) {
      const input = field(k);
      if (!input) continue;
      if (input.type === "checkbox") input.checked = v;
      else input.value = v;
    }
    field("memory").max = library.maxMemory;
  }
  function paintFiles() {
    options(
      el("java-worlds"),
      (files?.worlds || []).map((n) => [n, n]),
    );
    options(
      el("java-backups"),
      (files?.backups || []).map((n) => [n, n]),
    );
    el("java-whitelisted").textContent =
      "Whitelisted players · " +
      ((files?.players || []).join(", ") || "None added");
    const list = el("java-plugins");
    list.replaceChildren();
    for (const plugin of files?.plugins || []) {
      const li = document.createElement("li");
      const title = document.createElement("strong");
      title.textContent = plugin.file.replace(/\.disabled$/, "");
      const detail = document.createElement("div");
      detail.textContent = plugin.managed
        ? "Managed by the cross-play setting"
        : plugin.enabled
          ? "Enabled"
          : "Disabled";
      li.append(title, detail);
      if (!plugin.managed)
        for (const action of [
          plugin.enabled ? "disable" : "enable",
          "remove",
        ]) {
          const b = document.createElement("button");
          b.type = "button";
          b.className = action === "remove" ? "text-btn" : "secondary";
          b.textContent = action[0].toUpperCase() + action.slice(1);
          b.onclick = () => {
            if (
              action === "remove" &&
              !confirm("Remove " + plugin.file + " from this server?")
            )
              return;
            run(async () => {
              await post("plugin", { file: plugin.file, action });
              return "Plugin library updated.";
            });
          };
          li.append(b);
        }
      list.append(li);
    }
    if (!list.childElementCount) {
      const li = document.createElement("li");
      li.textContent = "No plugins installed.";
      list.append(li);
    }
  }
  function paint(reloadSettings) {
    const c = current?.config;
    const versions = el("java-new-version");
    if (!versions.options.length)
      options(
        versions,
        library.versions.map((v) => [v.id, v.id]),
        library.version,
      );
    el("java-version").textContent = c
      ? c.engine[0].toUpperCase() + c.engine.slice(1) + " · " + c.version
      : "Minecraft " + library.versions.at(-1).id + " – " + library.version;
    const rec = library.recommended;
    el("java-recommendation").textContent =
      "Recommended for this computer · " +
      rec.memory +
      " MiB per server · " +
      rec.players +
      " players";
    el("java-select-row").hidden = !current;
    el("java-instance").hidden = !current;
    const entries = (library.profiles || []).map((p) => [
      p.id,
      p.config.name + " · " + phases[p.phase],
    ]);
    const select = el("java-select");
    if (
      select.options.length !== entries.length ||
      entries.some(
        ([id, name], i) =>
          select.options[i]?.value !== id || select.options[i]?.text !== name,
      )
    )
      options(select, entries, selected);
    else select.value = selected;
    if (!c) {
      updateControls();
      return;
    }
    el("java-name").textContent = c.name;
    el("java-phase").textContent = phases[current.phase];
    el("java-message").textContent =
      current.message || "Start this world when you’re ready to play.";
    el("java-progress").hidden = current.phase !== "installing";
    if (current.percent >= 0) el("java-progress").value = current.percent;
    else el("java-progress").removeAttribute("value");
    el("java-cross-settings").hidden =
      c.engine !== "paper" ||
      !library.versions.find((v) => v.id === c.version)?.geyser;
    el("java-plugins-box").hidden = c.engine !== "paper";
    el("java-friends").hidden = el("java-bedrock-join").hidden = !c.geyser;
    const address = host.lan || "this computer’s LAN IP";
    el("java-join").textContent =
      "Java Edition · Multiplayer → Add Server → " +
      address +
      ":" +
      c.port +
      ". On this computer use localhost:" +
      c.port +
      ".";
    el("java-bedrock-join").textContent =
      "Bedrock Edition · Add Server → " +
      address +
      " · port " +
      c.bedrockPort +
      ". Xbox players join the same world through Floodgate.";
    el("java-network-help").textContent =
      "For friends outside your network, forward TCP " +
      c.port +
      (c.geyser
        ? " for Java and TCP and UDP " + c.bedrockPort + " for Bedrock"
        : " for Java") +
      " to this computer and share your public address.";
    el("java-player-edition").options[1].disabled = !c.geyser;
    if (!c.geyser) el("java-player-edition").value = "java";
    if (reloadSettings) fill(c);
    const log = el("java-log"),
      lines =
        (current.log || []).join("\n") || "Server output will appear here.";
    if (log.textContent !== lines) {
      const bottom = log.scrollTop + log.clientHeight >= log.scrollHeight - 6;
      log.textContent = lines;
      if (bottom) log.scrollTop = log.scrollHeight;
    }
    updateControls();
  }
  async function refresh(force = false) {
    const ticket = revision;
    let next = await api(
      "hosting/java?selected=" + encodeURIComponent(selected),
    );
    if (ticket !== revision || !active()) return;
    const profiles = next.profiles || [];
    if (!profiles.some((p) => p.id === selected)) {
      selected = (profiles.find((p) => p.busy) || profiles[0])?.id || "";
      if (selected) next = await api("hosting/java?selected=" + selected);
    }
    if (ticket !== revision || !active()) return;
    const item = (next.profiles || []).find((p) => p.id === selected);
    const changed = item?.id !== current?.id;
    const stopped = current?.busy && !item?.busy;
    const started = current?.phase !== "running" && item?.phase === "running";
    if (item && (force || changed || stopped || started || !files)) {
      const [pending, pendingMods] = await Promise.all([
        api(path("files")),
        item.config.engine === "fabric" ? api(path("mods")) : null,
      ]);
      if (ticket !== revision || !active()) return;
      mods = pendingMods;
      files = pending;
      paintFiles();
    }
    if (!item) files = mods = null;
    if (changed) {
      el("java-eula").checked = false;
      el("java-public").value = "";
      el("java-create-box").open = !item;
    }
    library = next;
    current = item;
    if (view === "hosting" && connected && !closed) {
      el("status-label").textContent = item
        ? "Java · " + phases[item.phase]
        : "Java hosting";
      el("hero-status").className =
        "status-chip" + (item?.phase === "running" ? " live" : "");
    }
    paint(force || changed || stopped);
  }
  async function tick() {
    if (loading || locked || !active()) return;
    loading = true;
    try {
      await refresh();
    } catch (e) {
      if (active()) feedback(e.message, true);
    } finally {
      loading = false;
    }
  }
  async function run(fn) {
    if (locked || !active()) return;
    locked = true;
    revision++;
    feedback("");
    let msg = "",
      error = false;
    try {
      // Let the action read the selected FormData before controls are disabled.
      const pending = fn();
      updateControls();
      msg = await pending;
    } catch (e) {
      msg = e.message;
      error = true;
    }
    try {
      await refresh(true);
    } catch (e) {
      if (!error) {
        msg = e.message;
        error = true;
      }
    }
    locked = false;
    updateControls();
    if (msg) feedback(msg, error);
  }
  function submit(id, fn) {
    el(id).onsubmit = (event) => {
      event.preventDefault();
      run(() => fn(event.submitter));
    };
  }
  function click(id, fn) {
    el(id).onclick = () => run(fn);
  }
  el("java-select").onchange = () => {
    selected = el("java-select").value;
    run(async () => "");
  };
  el("java-new-version").onchange = updateControls;
  el("java-create").addEventListener("change", updateControls);
  window.fabricMods.bind({ run, post, transfer });
  el("java-eula").onchange = el("java-new-eula").onchange = updateControls;
  submit("java-create", async (sender) => {
    const engine = new FormData(el("java-create")).get("engine");
    const start = sender?.value === "start",
      acceptTerms = el("java-new-eula").checked;
    const res = await api("hosting/java", "POST", {
      name: el("java-new-name").value.trim(),
      engine: engine === "crossplay" ? "paper" : engine,
      geyser: engine === "crossplay",
      version: el("java-new-version").value,
    });
    selected = res.id;
    el("java-create").reset();
    el("java-create-box").open = false;
    if (start) await post("start", { acceptTerms });
    return start
      ? "Server created. Follow its startup progress below."
      : "Server created.";
  });
  click("java-start", async () => {
    await post("start", { acceptTerms: el("java-eula").checked });
  });
  click("java-stop", async () => {
    await post("stop");
  });
  click("java-folder", async () => {
    await post("folder", { backups: false });
  });
  click("java-backups-folder", async () => {
    await post("folder", { backups: true });
  });
  click("java-reload", async () => "Loaded settings from server.properties.");
  el("java-preset").onclick = () => {
    for (const k of ["memory", "players", "view", "simulation"])
      field(k).value = library.recommended[k];
    feedback("Review the recommendations and save your settings.");
  };
  submit("java-settings", async () => {
    const c = { ...current.config };
    for (const k of ["name", "world", "mode", "difficulty"])
      c[k] = field(k).value.trim();
    for (const k of numeric) c[k] = Number(field(k).value);
    c.whitelist = field("whitelist").checked;
    c.geyser = !el("java-cross-settings").hidden && field("geyser").checked;
    await post("settings", c);
    return "Saved for the next start.";
  });
  async function transfer(action, body, type, param = "") {
    const res = await fetch("/api/" + path(action) + param, {
      method: "POST",
      headers: { Authorization: "Bearer " + token, "Content-Type": type },
      body,
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "File operation failed.");
  }
  submit("java-import-plugin", async () => {
    await transfer(
      "import-plugin",
      el("java-plugin-file").files[0],
      "application/octet-stream",
    );
    el("java-import-plugin").reset();
    return "Plugin installed for the next start.";
  });
  submit("java-world-import", async () => {
    await transfer(
      "import-world",
      el("java-world-file").files[0],
      "application/octet-stream",
      "?name=" + encodeURIComponent(el("java-import-name").value.trim()),
    );
    el("java-world-import").reset();
    return "World imported. Choose its folder under Settings.";
  });
  click("java-backup", async () => {
    await transfer("backup", "{}", "application/json");
    return "World backup saved.";
  });
  submit("java-restore", async () => {
    await transfer(
      "restore",
      JSON.stringify({
        backup: el("java-backups").value,
        name: el("java-restore-name").value.trim(),
      }),
      "application/json",
    );
    el("java-restore-name").value = "";
    return "Backup restored. Choose its new folder under Settings.";
  });
  submit("java-player", async (sender) => {
    await post("player", {
      name: el("java-player-name").value,
      bedrock: el("java-player-edition").value === "bedrock",
      remove: sender?.value === "remove",
    });
    el("java-player-name").value = "";
    await new Promise((resolve) => setTimeout(resolve, 450));
    return "See the server console for the whitelist result.";
  });
  submit("java-command", async () => {
    await post("command", { command: el("java-command-text").value });
    el("java-command-text").value = "";
    return "Command sent.";
  });
  el("java-broadcast").onclick = () => {
    if (!current?.config.geyser) return;
    if (state.running) {
      changeView("launcher");
      return;
    }
    const address = el("java-public").value.trim() || host.lan;
    if (!address) {
      feedback("Enter the address your friends can reach.", true);
      return;
    }
    editing = true;
    changeView("launcher");
    fillForm({
      ...state.settings,
      host: address,
      port: current.config.bedrockPort,
      name: current.config.name,
    });
    el("host").focus();
  };
  return {
    setHost,
    tick,
    isSelected: active,
    label: () =>
      active()
        ? current
          ? "Java · " + phases[current.phase]
          : "Java hosting"
        : "",
    live: () => active() && current?.phase === "running",
  };
})();
