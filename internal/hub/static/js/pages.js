// Overview (system info cards with filters), system and hosts pages.
import { $, ago, api, badge, confirmDialog, duration, hhmm, html, icon, jobBadge, kernelShort, modal, mount, on, toast } from "./core.js";
import { S, rerender, top } from "./app.js";
import { openOperation } from "./pages2.js";

const refreshAll = async (btn) => {
  btn.disabled = true;
  try { await api("/api/refresh", { method: "POST" }); toast("Reading the status of every host…"); } catch (e) { toast(e.message, true); }
  setTimeout(rerender, 2500);
};
const refreshBtn = html`<button data-act="refresh-all">${icon("refresh")} Refresh all</button>`;
const bindRefresh = (root) => on(root, "click", "[data-act=refresh-all]", (_, b) => refreshAll(b));

const chip = (in_) => html`<span class="chip ${in_.exe_differs && in_.exe_differs.length ? "warn" : ""}" title="instance ${in_.nr} · ${in_.status} · sapstartsrv ${in_.sapstartsrv}"><span class="dot ${in_.status}"></span>${in_.name}</span>`;
const lockTag = (s) => (s.unlocked_until
  ? html`<span class="tag open">${icon("unlock", 11)} changes allowed until ${hhmm(s.unlocked_until)}</span>`
  : html`<span class="tag">${icon("lock", 11)} read-only</span>`);
const disk = (d) => {
  const cls = d.use_pct >= 90 ? "hi" : d.use_pct >= 80 ? "mid" : "";
  return html`<div class="diskbar" title="${d.path}: ${d.use_pct}% used"><span class="mono">${d.path}</span><span class="meter"><i class="${cls}" data-w="${d.use_pct}"></i></span>${d.use_pct}%</div>`;
};
const uniq = (a) => [...new Set(a.filter(Boolean))];
const instances = (s) => s.hosts.flatMap((h) => h.instances.map((i) => ({ ...i, host: h.name })));

// ---------------------------------------------------------------- overview
const F = { q: "", status: "all", type: "", kernel: "" }; // survives the 10 s refresh

const STATUS_TABS = [["all", "All"], ["GREEN", "Running"], ["YELLOW", "Partial"], ["GRAY", "Stopped"], ["attention", "Attention"]];
function matches(s) {
  if (F.status === "attention" ? !(s.warnings && s.warnings.length) : F.status !== "all" && s.status !== F.status && !(F.status === "YELLOW" && s.status === "RED")) return false;
  if (F.type && s.type !== F.type) return false;
  if (F.kernel && s.kernel !== F.kernel) return false;
  if (!F.q) return true;
  const hay = [s.sid, s.type, s.db, s.kernel, kernelShort(s.kernel), ...s.hosts.flatMap((h) => [h.name, h.hostname, h.os, h.label, h.role]),
    ...instances(s).flatMap((i) => [i.name, i.nr])].join(" ").toLowerCase();
  return F.q.toLowerCase().split(/\s+/).filter(Boolean).every((t) => hay.includes(t));
}

export function overviewPage(el) {
  top("Overview", `KernelMan Hub on ${S.session.hub_host}`, refreshBtn);
  bindRefresh($("#top"));
  if (S.overview && !S.overview.hosts.length) {
    return mount(el, html`<div class="card card-b"><h2>Welcome to KernelMan Hub</h2>
      <p class="muted">Add your SAP servers. The hub connects over SSH — as root, <code>&lt;sid&gt;adm</code> or your domain user — installs the KernelMan agent and reads the status. Nothing else changes until you start an operation on a system you have unlocked.</p>
      <a class="btn primary" href="#/hosts">${icon("plus")} Add the first host</a></div>`);
  }
  mount(el, html`<div class="grid kpis" data-kpis></div>
    <div class="card toolbar"><label class="search"><span class="ic">${icon("search", 15)}</span><input type="search" data-q placeholder="Search SID, hostname, instance, kernel, OS…" value="${F.q}" aria-label="Search"></label>
      <div class="seg" data-status></div><select data-type aria-label="System type"></select><select data-kernel aria-label="Kernel level"></select><span class="found" data-found></span></div>
    <div class="grid cards" data-cards></div>
    <div class="grid two" data-bottom></div>`);
  const draw = () => {
    const o = S.overview;
    if (!o) return;
    drawKPIs($("[data-kpis]", el), o.kpi);
    const all = o.systems;
    mount($("[data-status]", el), html`${STATUS_TABS.map(([k, label]) => {
      const n = k === "all" ? all.length : k === "attention" ? all.filter((s) => s.warnings && s.warnings.length).length : all.filter((s) => s.status === k || (k === "YELLOW" && s.status === "RED")).length;
      return html`<button data-st="${k}" class="${F.status === k ? "on" : ""}">${label} <span class="n">${n}</span></button>`;
    })}`);
    const sel = (name, values, cur, label) => html`<option value="">${label}</option>${values.map((v) => html`<option value="${v}" ${v === cur ? "selected" : ""}>${name === "kernel" ? kernelShort(v) : v}</option>`)}`;
    mount($("[data-type]", el), sel("type", uniq(all.map((s) => s.type)).sort(), F.type, "All types"));
    mount($("[data-kernel]", el), sel("kernel", uniq(all.map((s) => s.kernel)).sort(), F.kernel, "All kernels"));
    const shown = all.filter(matches);
    $("[data-found]", el).textContent = `${shown.length} of ${all.length} systems`;
    mount($("[data-cards]", el), shown.length ? html`${shown.map(systemCard)}` : html`<div class="card empty">No system matches the filter.</div>`);
    mount($("[data-bottom]", el), html`<div class="card"><div class="card-h"><h2>Needs attention</h2><div class="grow"></div><span class="tag">${o.attention.length}</span></div>
        ${o.attention.length ? html`<div class="att">${o.attention.slice(0, 10).map((a) => html`<div class="${a.level}"><span class="i">!</span><div>${a.sid ? html`<a href="#" data-open="${a.sid}"><b>${a.sid}</b></a> · ` : ""}${a.text}</div></div>`)}</div>`
          : html`<div class="empty">${icon("shield")} Everything looks fine.</div>`}</div>
      ${jobsCard(o.jobs, "Recent jobs")}`);
  };
  draw();
  on(el, "input", "[data-q]", (_, i) => { F.q = i.value.trim(); draw(); });
  on(el, "click", "[data-st]", (_, b) => { F.status = b.dataset.st; draw(); });
  on(el, "change", "[data-type]", (_, s) => { F.type = s.value; draw(); });
  on(el, "change", "[data-kernel]", (_, s) => { F.kernel = s.value; draw(); });
  on(el, "click", "[data-open]", (e, c) => { e.preventDefault(); openSystem(c.dataset.open); });
  on(el, "keydown", "[data-open]", (e, c) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); openSystem(c.dataset.open); } });
  on(el, "click", "[data-href]", (_, r) => { location.hash = r.dataset.href; });
  return { update: draw };
}
overviewPage.live = true;

// systemCard shows only what matters at a glance: SID, state, kernel. Every card has the same size.
function systemCard(s) {
  const warn = s.warnings && s.warnings.length;
  return html`<div class="card sys ${warn ? "attn" : ""}" data-open="${s.sid}" role="button" tabindex="0" title="${s.sid}: open details">
    <div class="sys-top"><span class="sid">${s.sid}</span>${badge(s.status)}</div>
    <div class="sys-kernel"><span class="muted">Kernel</span><b class="mono">${kernelShort(s.kernel) || "unknown"}</b></div></div>`;
}

// openSystem shows a system's details and operations in a window over the overview.
function openSystem(sid) {
  const s = S.overview && S.overview.systems.find((x) => x.sid === sid);
  if (!s) return;
  const m = modal(html`<div class="card-h"><span class="sid">${s.sid}</span><span class="muted">${s.type} · ${s.db}</span><div class="grow"></div>${lockTag(s)} ${badge(s.status)}
      <button class="ghost" data-close aria-label="Close">✕</button></div>
    <div class="card-b">${systemInfo(s)}${systemDetail(s, true)}</div>`, { wide: true });
  bindSystemActions(m.el, () => S.overview.systems, async () => { m.close(); await rerender(); openSystem(sid); });
}

function systemInfo(s) {
  const insts = instances(s);
  return html`<dl class="info wide">
    <dt>Kernel</dt><dd><b class="mono">${kernelShort(s.kernel) || "unknown"}</b> · ${s.running}/${s.instances} instances running</dd>
    <dt>Hostnames</dt><dd class="mono wrap">${s.hosts.map((h) => h.hostname || h.name).join(", ")}</dd>
    <dt>Instances</dt><dd class="mono wrap">${insts.map((i) => `${i.name} (nr ${i.nr})`).join(", ")}</dd>
    <dt>OS</dt><dd class="wrap">${uniq(s.hosts.map((h) => h.os)).join(" · ") || "—"}</dd></dl>`;
}

function drawKPIs(box, k) {
  const pct = (a, b) => (b ? Math.round((a / b) * 100) : 0);
  const partial = k.systems - k.systems_running - k.systems_stopped;
  mount(box, html`
    <div class="card kpi"><span class="label">SAP systems</span><span class="value">${k.systems}</span>
      <span class="sub"><span><span class="dot GREEN"></span> ${k.systems_running} running</span>${partial ? html`<span><span class="dot YELLOW"></span> ${partial} partial</span>` : ""}<span><span class="dot GRAY"></span> ${k.systems_stopped} stopped</span></span></div>
    <div class="card kpi"><span class="label">Hosts reachable</span><span class="value">${k.hosts_ok}<small> / ${k.hosts}</small></span><div class="meter"><i data-w="${pct(k.hosts_ok, k.hosts)}"></i></div></div>
    <div class="card kpi"><span class="label">Instances running</span><span class="value">${k.instances_running}<small> / ${k.instances}</small></span><div class="meter"><i data-w="${pct(k.instances_running, k.instances)}"></i></div></div>
    <div class="card kpi"><span class="label">Kernel levels</span><span class="value">${(k.kernels || []).length}<small> in use</small></span>
      <span class="sub">${(k.kernels || []).map((v) => html`<span class="chip">${kernelShort(v)}</span>`)}</span></div>`);
}

const OPS = [
  ["update", "Kernel Update", "primary", "play"], ["backup", "Backup", "", "archive"], ["files", "Transfer files", "", "upload"],
  ["stop", "Stop", "danger", "stop"], ["start", "Start", "", "play"], ["services", "Start services", "", "refresh"], ["rollback", "Rollback", "danger", "undo"],
];

// systemDetail is shared by the overview's open card and the system page.
function systemDetail(s, inCard) {
  const locked = !s.unlocked_until;
  const busy = S.overview && S.overview.busy;
  return html`<div class="detail">
    <div class="actions">${OPS.map(([k, label, cls, ic]) => html`<button class="${cls} sm" data-op="${k}" data-sid="${s.sid}" ${locked || busy ? "disabled" : ""}>${icon(ic, 13)} ${label}</button>`)}
      <span class="grow"></span>${locked ? html`<button class="sm" data-unlock="${s.sid}">${icon("unlock", 13)} Unlock for changes</button>` : html`<button class="sm" data-lock="${s.sid}">${icon("lock", 13)} Lock now</button>`}
      ${inCard && !location.hash.startsWith("#/system/") ? html`<a class="btn sm" href="#/system/${s.sid}">Full page →</a>` : ""}</div>
    <div class="muted small">${locked ? "Read-only: unlock the system to run an operation. Every operation shows its plan and asks for the SID first." : busy ? "A job is running — new operations wait for it." : "Unlocked: operations run in the order below, central services first."}</div>
    ${s.warnings && s.warnings.length ? html`<div class="att inner">${s.warnings.map((w) => html`<div class="warn"><span class="i">!</span><div>${w}</div></div>`)}</div>` : ""}
    <div class="inner"><table class="table"><thead><tr><th>Host</th><th>Operating system</th><th>Role</th><th>Instances</th><th>Local exe = central</th><th>Disk</th><th>Checked</th></tr></thead><tbody>
    ${s.hosts.map((h) => html`<tr><td class="hostcell"><b class="mono">${h.hostname || h.name}</b>${h.label ? html`<div class="muted small">${h.label}</div>` : ""}</td>
      <td class="small">${h.os || "—"}</td>
      <td>${h.managed ? (h.reachable ? h.role : html`<span class="badge s-RED">unreachable</span><div class="muted small">${h.error}</div>`) : html`<span class="badge s-YELLOW">not in hub</span><div class="small"><a href="#/hosts">add host</a></div>`}</td>
      <td><div class="chips">${h.instances.map(chip)}</div><div class="muted small mono">nr ${h.instances.map((i) => i.nr).join(", ")}</div></td>
      <td>${h.instances.some((i) => i.exe_differs && i.exe_differs.length) ? html`<span class="badge s-RED">differs</span><div class="muted small">${h.instances.filter((i) => i.exe_differs && i.exe_differs.length).map((i) => i.name + ": " + i.exe_differs.join(", ")).join(" · ")}</div>`
        : h.managed ? html`<span class="badge s-GREEN">in sync</span>` : "—"}</td>
      <td>${(h.disk || []).map(disk)}</td><td class="muted small">${h.managed ? ago(h.checked) : ""}</td></tr>`)}
    </tbody></table></div></div>`;
}

function bindSystemActions(root, systems, after = rerender) {
  const find = (sid) => systems().find((x) => x.sid === sid);
  on(root, "click", "[data-op]", (_, b) => openOperation(find(b.dataset.sid), b.dataset.op));
  on(root, "click", "[data-unlock]", async (_, b) => {
    const sid = b.dataset.unlock;
    const ok = await confirmDialog("Unlock " + sid + " for changes?",
      html`<p>For the next 8 hours the hub may stop, start, back up and update <b>${sid}</b>. Every operation still shows its plan and asks for the SID before it runs.</p><p class="muted">Keep production locked until its maintenance window.</p>`, "Unlock " + sid);
    if (ok) { await api(`/api/systems/${sid}/lock`, { method: "POST", body: { unlock: true } }); after(); }
  });
  on(root, "click", "[data-lock]", async (_, b) => { await api(`/api/systems/${b.dataset.lock}/lock`, { method: "POST", body: { unlock: false } }); after(); });
}

export function jobsCard(jobs, title) {
  return html`<div class="card"><div class="card-h"><h2>${title}</h2><div class="grow"></div><a href="#/jobs" class="small">All jobs</a></div>
    ${jobs && jobs.length ? html`<table class="table"><tbody>${jobs.map((j) => html`<tr class="click" data-href="#/job/${j.id}">
      <td><b>${j.title}</b><div class="muted small">${ago(j.created)}${j.started ? " · " + duration(j.started, j.ended) : ""}</div></td><td>${jobBadge(j.state)}</td></tr>`)}</tbody></table>`
      : html`<div class="empty">No jobs yet.</div>`}</div>`;
}

// ---------------------------------------------------------------- system page
export function systemPage(el, sid) {
  let alive = true;
  let current = null;
  const load = () => api("/api/systems/" + sid).then((d) => { if (alive) { current = d.system; drawSystem(el, d.system, d.jobs); } }).catch((e) => {
    top(sid, "SAP Systems");
    mount(el, html`<div class="card empty">${e.message}</div>`);
  });
  bindSystemActions(el, () => (current ? [current] : []), () => load());
  on(el, "click", "[data-href]", (_, c) => { location.hash = c.dataset.href; });
  load();
  return { cleanup: () => { alive = false; }, update: load };
}
systemPage.live = true;

function drawSystem(el, s, jobs) {
  top(s.sid, html`<a href="#/">Overview</a> / SAP Systems`, refreshBtn);
  bindRefresh($("#top"));
  const central = s.hosts.find((h) => h.central);
  mount(el, html`<div class="card"><div class="card-h"><span class="sid">${s.sid}</span>
      <span class="muted">${s.type} · ${s.db} · ${s.hosts.length} host${s.hosts.length > 1 ? "s" : ""}${central ? " · central services on " + (central.hostname || central.name) : ""}</span>
      <div class="grow"></div>${lockTag(s)} ${badge(s.status)}</div>
    <div class="card-b">${systemInfo(s)}${systemDetail(s, true)}</div></div>
    ${jobsCard(jobs, "Jobs for " + s.sid)}`);
}

// ---------------------------------------------------------------- hosts
const BECOME = { "": "login user", root: "root via sudo", sidadm: "<sid>adm via sudo" };

export function hostsPage(el) {
  top("Hosts", html`<a href="#/">Overview</a>`, html`${refreshBtn}<button class="primary" data-act="add">${icon("plus")} Add host</button>`);
  bindRefresh($("#top"));
  $("#top [data-act=add]").addEventListener("click", () => hostForm());
  const hosts = (S.overview && S.overview.hosts) || [];
  mount(el, html`<div class="note">Adding a host only reads its status: the hub logs in over SSH — as root, <code>&lt;sid&gt;adm</code> or your domain user — copies the KernelMan agent and runs <code>kernelman status</code>. Host keys are pinned on first contact; passwords are stored encrypted with your hub password.</div>
  <div class="card">${hosts.length ? html`<table class="table"><thead><tr><th>Host</th><th>Login</th><th>SAP</th><th>Status</th><th>Host key</th><th></th></tr></thead><tbody>
    ${hosts.map((h) => html`<tr><td><b class="mono">${h.name}</b>${h.label ? html`<div class="muted small">${h.label}</div>` : ""}</td>
      <td class="small"><b>${h.user}</b> · port ${h.port}<div class="muted">${h.auth === "key" ? "SSH key" : "password"} · operations as ${BECOME[h.become || ""]}${h.sudo_pw ? " (sudo password)" : ""}</div></td>
      <td>${h.sids.length ? html`<div class="chips">${h.sids.map((x) => html`<a class="chip" href="#/system/${x}">${x}</a>`)}</div>` : html`<span class="muted">—</span>`}
        <div class="muted small">${h.hostname || ""}${h.state.os ? " · " + h.state.os + " " + h.state.machine : ""}</div></td>
      <td>${h.state.refreshing ? html`<span class="badge s-BLUE"><span class="spin"></span> checking</span>` : h.state.ok ? html`<span class="badge s-GREEN">reachable</span>`
        : h.state.checked && !h.state.checked.startsWith("0001") ? html`<span class="badge s-RED">error</span><div class="small">${h.state.error}</div>` : html`<span class="badge s-GRAY">not checked</span>`}
        <div class="muted small">${ago(h.state.checked)}</div></td>
      <td class="mono small muted">${h.state.fingerprint || "—"}</td>
      <td><div class="actions"><button class="sm" data-test="${h.id}">${icon("refresh", 13)} Test</button><button class="sm" data-edit="${h.id}">Edit</button><button class="sm danger" data-del="${h.id}">${icon("trash", 13)}</button></div></td></tr>`)}
    </tbody></table>` : html`<div class="empty">No hosts yet. <a href="#" data-act="add2">Add the first one</a>.</div>`}</div>`);
  on(el, "click", "[data-act=add2]", (e) => { e.preventDefault(); hostForm(); });
  on(el, "click", "[data-test]", async (_, b) => {
    b.disabled = true; b.innerHTML = '<span class="spin"></span> Testing';
    try { const r = await api(`/api/hosts/${b.dataset.test}/refresh`, { method: "POST" }); r.error ? toast(r.error, true) : toast("Connected, status read."); } catch (e) { toast(e.message, true); }
    rerender();
  });
  on(el, "click", "[data-edit]", (_, b) => hostForm(hosts.find((h) => h.id === b.dataset.edit)));
  on(el, "click", "[data-del]", async (_, b) => {
    const h = hosts.find((x) => x.id === b.dataset.del);
    if (await confirmDialog("Remove " + h.name + "?", html`<p>The hub forgets the host, its stored password and its pinned host key. Nothing changes on the server.</p>`, "Remove", true)) {
      await api("/api/hosts/" + h.id, { method: "DELETE" }); rerender();
    }
  });
}
hostsPage.live = true;

function hostForm(h) {
  let edit = !!h;
  h = h || { name: "", port: 22, user: "", auth: "password", become: "", sudo_pw: false, label: "" };
  const m = modal(html`<form><div class="card-h"><h2>${edit ? "Edit " + h.name : "Add an SAP host"}</h2><div class="grow"></div><button type="button" class="ghost" data-close>✕</button></div>
    <div class="card-b form">
      <div class="row"><label>Host name or IP<input type="text" name="name" value="${h.name}" required placeholder="sapdev01.example.com"></label><label>SSH port<input type="number" name="port" value="${h.port}" min="1" max="65535"></label></div>
      <label>Login user<input type="text" name="user" value="${h.user}" required placeholder="devadm, your domain user, or root"></label>
      <div><div class="seg"><button type="button" data-auth="password" class="${h.auth !== "key" ? "on" : ""}">Password</button><button type="button" data-auth="key" class="${h.auth === "key" ? "on" : ""}">SSH key</button></div></div>
      <label data-for="password" class="${h.auth === "key" ? "hidden" : ""}">Password<input type="password" name="secret_pw" autocomplete="off" placeholder="${edit && h.has_secret ? "unchanged" : ""}"></label>
      <div data-for="key" class="form ${h.auth === "key" ? "" : "hidden"}"><label>Private key file on the hub machine<input type="text" name="key_path" value="${h.key_path || ""}" placeholder="/home/hub/.ssh/id_ed25519"></label>
        <label>Key passphrase (optional)<input type="password" name="secret_key" autocomplete="off" placeholder="${edit && h.has_secret ? "unchanged" : ""}"></label></div>
      <label>Run operations as<select name="become">
        <option value="" ${!h.become ? "selected" : ""}>the login user (root or &lt;sid&gt;adm logins)</option>
        <option value="sidadm" ${h.become === "sidadm" ? "selected" : ""}>&lt;sid&gt;adm via sudo (domain user with sudo -u &lt;sid&gt;adm)</option>
        <option value="root" ${h.become === "root" ? "selected" : ""}>root via sudo (domain user with sudo rights)</option></select></label>
      <label class="check"><input type="checkbox" name="sudo_pw" ${h.sudo_pw ? "checked" : ""}> sudo asks for my password (the login password is given to sudo -S, never on a command line)</label>
      <label>Label (optional)<input type="text" name="label" value="${h.label || ""}" placeholder="DEV · Central Services"></label>
      <div class="note">As <code>&lt;sid&gt;adm</code> every SAP step runs; only root steps (chown, saproot.sh, SAP Host Agent restart) are reported as "run as root later". With sudo the agent lives in <code>/var/tmp/kernelman-&lt;user&gt;</code> so that &lt;sid&gt;adm can run it.</div>
      <div class="note err hidden" data-err></div>
    </div>
    <div class="modal-f"><span class="muted small grow" data-wait></span><button type="button" data-close>Cancel</button><button class="primary" type="submit">${edit ? "Save and test" : "Add and test"}</button></div></form>`);
  let auth = h.auth === "key" ? "key" : "password";
  on(m.el, "click", "[data-auth]", (_, b) => {
    auth = b.dataset.auth;
    m.el.querySelectorAll("[data-auth]").forEach((x) => x.classList.toggle("on", x === b));
    m.el.querySelector("[data-for=password]").classList.toggle("hidden", auth !== "password");
    m.el.querySelector("[data-for=key]").classList.toggle("hidden", auth !== "key");
  });
  m.el.querySelector("form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = Object.fromEntries(new FormData(e.target));
    const body = { name: f.name.trim(), port: +f.port || 22, user: f.user.trim(), auth, key_path: f.key_path || "", become: f.become || "", sudo_pw: !!f.sudo_pw && !!f.become,
      label: f.label || "", secret: auth === "key" ? f.secret_key || "" : f.secret_pw || "" };
    const err = m.el.querySelector("[data-err]"), wait = m.el.querySelector("[data-wait]"), btn = m.el.querySelector("button[type=submit]");
    btn.disabled = true; err.classList.add("hidden"); wait.innerHTML = '<span class="spin"></span> connecting, installing the agent, reading the status…';
    try {
      const r = await api(edit ? "/api/hosts/" + h.id : "/api/hosts", { method: edit ? "PUT" : "POST", body });
      if (r.error) { err.textContent = "Saved, but the test failed: " + r.error; err.classList.remove("hidden"); edit = true; h.id = r.id; }
      else { m.close(); toast(body.name + " connected."); }
      rerender();
    } catch (ex) { err.textContent = ex.message; err.classList.remove("hidden"); }
    btn.disabled = false; wait.textContent = "";
  });
  m.el.querySelector("input[name=name]").focus();
}
