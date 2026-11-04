// KernelMan Hub dashboard: session, layout, router, polling.
import { $, api, html, icon, logo, mount, on, toast } from "./core.js";
import { overviewPage, systemPage, hostsPage } from "./pages.js";
import { jobsPage, jobPage, archivesPage, settingsPage } from "./pages2.js";

const app = document.getElementById("app");
export const S = { session: null, overview: null, cleanup: null, update: null, timer: null, route: "" };

const routes = [
  [/^#?\/?$/, overviewPage, "overview"],
  [/^#\/system\/([A-Z0-9]{3})$/, systemPage, "systems"],
  [/^#\/hosts$/, hostsPage, "hosts"],
  [/^#\/jobs$/, jobsPage, "jobs"],
  [/^#\/job\/([a-f0-9]+)$/, jobPage, "jobs"],
  [/^#\/archives$/, archivesPage, "archives"],
  [/^#\/settings$/, settingsPage, "settings"],
];

const theme = () => { try { return localStorage.getItem("km-theme") || ""; } catch { return ""; } };
function applyTheme(t) {
  if (t) document.documentElement.dataset.theme = t; else delete document.documentElement.dataset.theme;
  try { localStorage.setItem("km-theme", t); } catch { /* private mode */ }
}

async function boot() {
  applyTheme(theme());
  try { S.session = await api("/api/session"); } catch (e) { mount(app, html`<div class="empty">Hub not reachable: ${e.message}</div>`); return; }
  if (!S.session.logged_in) return loginPage();
  shell();
  window.addEventListener("hashchange", route);
  await refresh();
  route();
}
window.addEventListener("km:logout", () => { if (S.session) { S.session.logged_in = false; stopPolling(); loginPage(); } });

function loginPage() {
  const setup = S.session.setup;
  mount(app, html`<div class="login"><form class="card" autocomplete="on">
    <div class="brand">${logo(36)}<div><b>KernelMan Hub</b><span>SAP kernel updates for every server</span></div></div>
    <div><h2>${setup ? "Create the administrator" : "Sign in"}</h2>
      <div class="muted small">${setup ? "This account protects the hub. Its password also encrypts the stored SSH passwords." : "Host " + S.session.hub_host}</div></div>
    ${S.session.demo ? html`<div class="note">Demo mode: user <b>admin</b>, password <b>kernelman1</b> — simulated SAP hosts, nothing real is touched.</div>` : ""}
    <label>User<input type="text" name="user" value="${setup ? "admin" : ""}" autocomplete="username" required></label>
    <label>Password<input type="password" name="password" autocomplete="${setup ? "new-password" : "current-password"}" required minlength="${setup ? 10 : 1}"></label>
    ${setup ? html`<label>Repeat password<input type="password" name="again" autocomplete="new-password" required></label>` : ""}
    <div class="note err hidden" id="err"></div>
    <button class="primary" type="submit">${setup ? "Create and sign in" : "Sign in"}</button>
    <div class="muted small">KernelMan ${S.session.version}</div></form></div>`);
  const f = $("form", app);
  f.addEventListener("submit", async (e) => {
    e.preventDefault();
    const d = Object.fromEntries(new FormData(f));
    const err = $("#err", app);
    if (setup && d.password !== d.again) { err.textContent = "The passwords differ."; err.classList.remove("hidden"); return; }
    try {
      await api(setup ? "/api/setup" : "/api/login", { method: "POST", body: { user: d.user, password: d.password } });
      location.hash = location.hash || "#/";
      boot();
    } catch (ex) { err.textContent = ex.message; err.classList.remove("hidden"); }
  });
  ($('input[name="user"]', app).value ? $('input[name="password"]', app) : $('input[name="user"]', app)).focus();
}

function shell() {
  mount(app, html`<div class="shell"><aside class="side">
      <div class="brand">${logo(32)}<div><b>KernelMan Hub</b><span>${S.session.hub_host}</span></div></div>
      <nav class="nav" id="nav"></nav>
      <div class="side-foot"><div>${icon("shield", 13)} ${S.session.user}${S.session.demo ? " · demo" : ""}</div>
        <div class="actions"><button class="sm" data-act="theme">${icon("moon", 13)} Theme</button><button class="sm" data-act="logout">${icon("logout", 13)} Sign out</button></div>
        <div class="muted">KernelMan ${S.session.version}</div></div>
    </aside><main class="main"><header class="top" id="top"></header><section class="content" id="content"></section></main></div>`);
  on(app, "click", "[data-act=theme]", () => {
    const dark = document.documentElement.dataset.theme === "dark" || (!theme() && matchMedia("(prefers-color-scheme: dark)").matches);
    applyTheme(dark ? "light" : "dark");
  });
  on(app, "click", "[data-act=logout]", async () => { try { await api("/api/logout", { method: "POST" }); } catch { /* gone */ } location.reload(); });
}

function nav() {
  const o = S.overview || { systems: [], hosts: [], kpi: {} };
  const cur = S.route;
  const a = (href, ic, label, key, count) => html`<a href="${href}" class="${cur === key ? "on" : ""}">${icon(ic)} ${label}${count !== undefined ? html`<span class="count">${count}</span>` : ""}</a>`;
  mount($("#nav"), html`${a("#/", "grid", "Overview", "overview")}
    ${a(o.systems.length ? "#/system/" + o.systems[0].sid : "#/", "layers", "SAP Systems", "systems", o.systems.length)}
    <div class="nav-sub">${o.systems.map((s) => html`<a href="#/system/${s.sid}" class="${location.hash === "#/system/" + s.sid ? "on" : ""}"><span class="dot ${s.status}"></span> ${s.sid}</a>`)}</div>
    ${a("#/hosts", "server", "Hosts", "hosts", o.hosts.length)}
    ${a("#/jobs", "list", "Jobs", "jobs", o.kpi.jobs_today || undefined)}
    ${a("#/archives", "archive", "Archives", "archives", o.kpi.archives)}
    ${a("#/settings", "gear", "Settings", "settings")}`);
}

// fresh swaps an element for an empty copy, dropping the listeners the previous render bound.
function fresh(sel) {
  const old = $(sel);
  const el = old.cloneNode(false);
  old.replaceWith(el);
  return el;
}

// top sets the page header.
export function top(title, crumbs, actions) {
  mount(fresh("#top"), html`<div><div class="crumbs">${crumbs || "KernelMan Hub"}</div><h1>${title}</h1></div><div class="grow"></div>
    ${S.overview && S.overview.busy ? html`<span class="badge s-BLUE"><span class="spin"></span> job running</span>` : ""}${actions || ""}`);
}

export async function refresh() {
  try { S.overview = await api("/api/overview"); } catch (e) { if (e.status !== 401) toast(e.message, true); return; }
  nav();
}

let current = null;
function route() {
  if (S.cleanup) { S.cleanup(); S.cleanup = null; }
  S.update = null;
  document.querySelectorAll(".backdrop").forEach((b) => b.remove()); // a dialog belongs to the page it was opened on
  const hash = location.hash || "#/";
  for (const [re, page, key] of routes) {
    const m = hash.match(re);
    if (m) {
      S.route = key;
      current = { page, args: m.slice(1) };
      nav();
      render();
      startPolling();
      return;
    }
  }
  location.hash = "#/";
}

function render() {
  if (!current) return;
  if (S.cleanup) { S.cleanup(); S.cleanup = null; }
  const el = fresh("#content");
  const r = current.page(el, ...current.args);
  S.update = null;
  if (typeof r === "function") S.cleanup = r;
  else if (r && typeof r === "object" && ("update" in r || "cleanup" in r)) { S.cleanup = r.cleanup || null; S.update = r.update || null; }
}

// Live pages re-render with fresh data; the job page streams on its own.
function startPolling() {
  stopPolling();
  const tick = async () => {
    const live = current && current.page.live;
    await refresh();
    // pages with their own update keep filters, focus and open cards; others redraw
    if (live && !document.querySelector(".backdrop")) { if (S.update) S.update(); else render(); }
    S.timer = setTimeout(tick, S.overview && S.overview.busy ? 3000 : 10000);
  };
  S.timer = setTimeout(tick, S.overview && S.overview.busy ? 3000 : 10000);
}
function stopPolling() { clearTimeout(S.timer); }

export function rerender() { refresh().then(() => (S.update ? S.update() : render())); }

boot();
