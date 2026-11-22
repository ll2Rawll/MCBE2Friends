"use strict";

window.fabricMods = (() => {
  const el = (id) => document.getElementById(id);
  const box = el("java-mods-box");
  const pageSize = 10;
  let profile,
    inventory,
    actions,
    locked = false,
    project = null,
    term = "",
    offset = 0,
    total = 0;
  let searchTicket = 0,
    versionTicket = 0,
    searched = false;
  const element = (tag, content) => {
    const n = document.createElement(tag);
    n.textContent = content;
    return n;
  };
  function button(text, fn, kind = "secondary") {
    const b = element("button", text);
    b.type = "button";
    b.className = kind;
    b.onclick = fn;
    return b;
  }
  function status(msg, error = false) {
    el("mod-status").textContent = msg;
    el("mod-status").classList.toggle("error", error);
  }
  function controls() {
    const busy = locked || Boolean(profile?.busy);
    el("mod-install").disabled = busy || !el("mod-version").value || !project;
    el("mod-installed")
      .querySelectorAll("[data-change]")
      .forEach((b) => (b.disabled = busy));
    el("mod-prev").disabled = offset <= 0;
    el("mod-next").disabled = offset + pageSize >= total || offset >= 10000;
    el("mod-context").textContent = profile
      ? "Fabric for Minecraft " +
        profile.config.version +
        ". " +
        (profile.busy
          ? "Stop this server to change its mods."
          : "Required Modrinth dependencies install with your selection.")
      : "";
  }
  function clearVersion() {
    versionTicket++;
    project = null;
    el("mod-version-box").hidden = true;
    el("mod-version").replaceChildren();
  }
  function setProfile(next, list, lock) {
    next = next?.config.engine === "fabric" ? next : null;
    locked = lock;
    if (next?.id !== profile?.id) {
      searchTicket++;
      clearVersion();
      searched = false;
      term = "";
      offset = total = 0;
      el("mod-query").value = "";
      el("mod-results").replaceChildren();
      el("mod-pages").hidden = true;
      box.open = false;
      status("");
    }
    profile = next;
    box.hidden = !profile;
    if (inventory !== list) {
      inventory = list;
      paintInstalled();
    }
    controls();
  }
  const endpoint = (action) => "hosting/java/" + profile.id + "/mods" + action;
  function change(id, version, action, title) {
    if (locked || profile?.busy) return;
    if (
      action === "remove" &&
      !confirm("Remove " + title + " from this server?")
    )
      return;
    actions.run(async () => {
      try {
        status(
          action === "install"
            ? "Checking dependencies and downloading verified files…"
            : "Updating installed mods…",
        );
        await actions.transfer(
          "mods/change",
          JSON.stringify({ project: id, version, action }),
          "application/json",
        );
        clearVersion();
        status(
          action === "install"
            ? title + " installed for the next start."
            : "Installed mods updated.",
        );
      } catch (e) {
        status(e.message, true);
        throw e;
      }
    });
  }
  function paintInstalled() {
    const list = el("mod-installed");
    list.replaceChildren();
    for (const m of inventory?.managed || []) {
      const li = document.createElement("li");
      li.append(
        element("strong", m.title),
        element("div", m.number + " · " + (m.enabled ? "Enabled" : "Disabled")),
      );
      if (m.requires?.length) {
        const names = m.requires.map(
          (d) =>
            inventory.managed.find((x) => x.project === d.project)?.title ||
            d.project,
        );
        li.append(element("small", "Requires " + names.join(", ")));
      }
      const row = document.createElement("div");
      row.className = "host-action-row";
      row.append(
        button("Versions", () => choose(m.project, m.title, m.version)),
      );
      for (const action of [m.enabled ? "disable" : "enable", "remove"]) {
        const b = button(
          action[0].toUpperCase() + action.slice(1),
          () => change(m.project, "", action, m.title),
          "text-btn",
        );
        b.dataset.change = action;
        row.append(b);
      }
      li.append(row);
      list.append(li);
    }
    for (const file of inventory?.manual || []) {
      const li = element("li", file);
      li.append(element("div", "Added manually · manage in the mods folder"));
      list.append(li);
    }
    if (!list.childElementCount)
      list.append(element("li", "No mods installed yet."));
  }
  async function choose(id, title, currentVersion = "") {
    if (!profile) return;
    clearVersion();
    const ticket = versionTicket;
    status("Loading versions for " + title + "…");
    try {
      const versions = await api(
        endpoint("/versions?project=" + encodeURIComponent(id)),
      );
      if (ticket !== versionTicket) return;
      if (!versions.length) {
        status("No dedicated-server Fabric files for this Minecraft version.");
        return;
      }
      project = { id, title };
      const select = el("mod-version");
      select.replaceChildren(
        ...versions.map(
          (v) =>
            new Option(
              v.number +
                " · " +
                v.kind +
                (v.id === currentVersion ? " · installed" : ""),
              v.id,
            ),
        ),
      );
      el("mod-version-title").textContent = title + " · choose a version";
      el("mod-version-box").hidden = false;
      status("");
      controls();
      select.focus();
      el("mod-version-box").scrollIntoView({ block: "nearest" });
    } catch (e) {
      if (ticket === versionTicket) status(e.message, true);
    }
  }
  async function search(nextOffset) {
    if (!profile) return;
    const ticket = ++searchTicket;
    clearVersion();
    searched = true;
    const query = el("mod-query").value.trim();
    el("mod-results").replaceChildren();
    el("mod-pages").hidden = true;
    status("Searching Modrinth…");
    try {
      const data = await api(
        endpoint(
          "/search?q=" + encodeURIComponent(query) + "&offset=" + nextOffset,
        ),
      );
      if (ticket !== searchTicket) return;
      term = query;
      offset = nextOffset;
      total = data.total_hits;
      for (const hit of data.hits || []) {
        const li = document.createElement("li");
        li.append(
          element("strong", hit.title),
          element("small", " · " + hit.author),
          element("p", hit.description),
          button("Choose version", () => choose(hit.project_id, hit.title)),
        );
        const link = element("a", "View on Modrinth");
        link.href =
          "https://modrinth.com/mod/" + encodeURIComponent(hit.project_id);
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        link.className = "text-btn";
        li.append(link);
        el("mod-results").append(li);
      }
      status(
        total
          ? total.toLocaleString() + " compatible projects"
          : "No matches for Fabric " +
              profile.config.version +
              ". Try a shorter search.",
      );
      el("mod-pages").hidden = total <= pageSize;
      el("mod-page").textContent =
        offset + 1 + "–" + Math.min(offset + pageSize, total);
      controls();
    } catch (e) {
      if (ticket === searchTicket) {
        searched = false;
        status(e.message, true);
      }
    }
  }
  box.addEventListener("toggle", () => {
    if (box.open && !searched) search(0);
  });
  el("mod-search").onsubmit = (e) => {
    e.preventDefault();
    search(0);
  };
  el("mod-prev").onclick = () => {
    el("mod-query").value = term;
    search(Math.max(0, offset - pageSize));
  };
  el("mod-next").onclick = () => {
    el("mod-query").value = term;
    search(offset + pageSize);
  };
  el("mod-install").onclick = () => {
    if (project)
      change(project.id, el("mod-version").value, "install", project.title);
  };
  el("mod-cancel").onclick = () => {
    clearVersion();
    status("");
  };
  el("mod-folder").onclick = () =>
    actions.run(async () => {
      await actions.post("mods/folder");
    });
  return { setProfile, bind: (callbacks) => (actions = callbacks) };
})();
