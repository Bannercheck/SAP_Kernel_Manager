// Jobs, job console, archives, settings and the operation dialog.
import { $, ago, api, bytes, clock, confirmDialog, duration, html, icon, jobBadge, kernelShort, modal, mount, on, toast } from "./core.js";
import { S, refresh, rerender, top } from "./app.js";

// ---------------------------------------------------------------- operation dialog
const OP = {
  update: { title: "Kernel Update", text: "Preflight on every host, push the archives, stop the system, back up and extract the kernel on every host (central services first), verify that every instance directory matches the central kernel, then start." },
  backup: { title: "Kernel Backup", text: "Copies every kernel directory next to itself as <name>_<date>. The shared central directory is backed up once." },
  files: { title: "Kernel File Transfer", text: "Pushes the archives to the hosts (or uses archives already placed there today), checks them with cksum and copies them into every kernel directory." },
  stop: { title: "SAP Stop", text: "StopSystem ALL from the central services host, then every host waits for its instances and stops its sapstartsrv." },
  start: { title: "SAP Start", text: "Starts sapstartsrv on every host, StartSystem ALL, and waits until every instance is GREEN." },
  services: { title: "Start SAP services", text: "Starts the SAP Host Agent and every stopped sapstartsrv with its profile. SAP instances are not started." },
  rollback: { title: "Kernel Rollback", text: "Stops the system, copies each directory's latest backup back, verifies the kernel level on every host, then starts." },
};
const NEEDS_SID = { stop: 1, update: 1, rollback: 1 };

export async function openOperation(sys, kind) {
  const op = OP[kind];
  const managed = sys.hosts.filter((h) => h.managed);
  let lib = [];
  if (kind === "update" || kind === "files") { try { lib = await api("/api/library"); } catch (e) { toast(e.message, true); } }
  const sars = lib.filter((a) => a.kind === "sar");
  const m = modal(html`<div class="card-h"><h2>${op.title} · ${sys.sid}</h2><div class="grow"></div><span class="muted small">current kernel ${kernelShort(sys.kernel)}</span><button class="ghost" data-close>✕</button></div>
    <div class="card-b form">
      <div class="note">${op.text}</div>
      <label>Hosts<select name="host"><option value="">All ${managed.length} hosts of ${sys.sid} (central services first)</option>
        ${managed.map((h) => html`<option value="${h.host_id}">only ${h.name} — ${h.role}</option>`)}</select></label>
      ${kind === "update" || kind === "files" ? html`<div><div class="small"><b>Archives from the hub library</b> <span class="muted">(<a href="#/archives" data-close>upload more</a>)</span></div>
        ${lib.length ? html`<table class="table"><tbody>${lib.map((a) => html`<tr><td><label class="check"><input type="checkbox" name="arch" value="${a.name}" checked> <span class="mono">${a.name}</span></label></td>
          <td class="small">${a.kind === "sapcar" ? "SAPCAR" : a.label + (a.full ? " (full kernel)" : "")}</td><td class="small">${a.patch || ""}</td><td class="small muted">${bytes(a.size)}</td></tr>`)}</tbody></table>
          <div class="small muted" data-target></div>`
        : html`<div class="note warn">The library is empty: the hosts must already hold today's archives (Kernel File Transfer finds them).</div>`}</div>` : ""}
      ${kind === "update" || kind === "rollback" ? html`<label class="check"><input type="checkbox" name="start" checked> Start SAP afterwards (only when every host verified)</label>` : ""}
      <div><div class="small"><b>Plan</b> <span class="muted">— exactly what will run, in this order</span></div><div class="plan" data-plan><div class="muted">…</div></div></div>
      <div class="note err hidden" data-err></div>
      ${NEEDS_SID[kind] ? html`<label><span>Type <b>${sys.sid}</b> to confirm</span><input type="text" name="confirm" autocomplete="off" placeholder="${sys.sid}"></label>` : ""}
    </div>
    <div class="modal-f"><button data-close>Cancel</button><button class="${NEEDS_SID[kind] ? "danger solid" : "primary"}" data-run disabled>${icon("play", 14)} Run ${op.title}</button></div>`);
  const el = m.el;
  const req = () => {
    const r = { kind, sid: sys.sid, host_id: el.querySelector("[name=host]").value };
    const boxes = [...el.querySelectorAll("[name=arch]")];
    if (boxes.length) r.archives = boxes.filter((b) => b.checked).map((b) => b.value);
    const st = el.querySelector("[name=start]");
    if (st) r.start = st.checked;
    const c = el.querySelector("[name=confirm]");
    if (c) r.confirm = c.value.trim();
    return r;
  };
  let planOK = false;
  const check = () => {
    const c = el.querySelector("[name=confirm]");
    el.querySelector("[data-run]").disabled = !planOK || (c && c.value.trim().toUpperCase() !== sys.sid);
  };
  const plan = async () => {
    const r = req();
    const t = el.querySelector("[data-target]");
    if (t) {
      const sel = sars.filter((a) => (r.archives || []).includes(a.name));
      const p = Math.max(0, ...sel.map((a) => a.patch || 0));
      t.textContent = sel.length ? `Target level after the update: patch ${p} (now ${kernelShort(sys.kernel)})` : "No archive selected: archives already placed on the hosts today are used.";
    }
    const err = el.querySelector("[data-err]");
    try {
      const j = await api("/api/plan", { method: "POST", body: r });
      const groups = [];
      for (const s of j.steps) {
        const phase = s.kind === "verify" ? s.title : s.title.split(" on ")[0];
        const g = groups[groups.length - 1];
        if (g && g.phase === phase && g.kind === s.kind) g.hosts.push(s.host_name); else groups.push({ phase, kind: s.kind, hosts: s.kind === "verify" ? [] : [s.host_name] });
      }
      mount(el.querySelector("[data-plan]"), html`${groups.map((g) => html`<div><span><b>${g.phase}</b>${g.hosts.length ? html`<span class="chips plan-hosts">${g.hosts.map((h, i) => html`<span class="chip">${g.hosts.length > 1 ? i + 1 + ". " : ""}${h}</span>`)}</span>` : ""}</span><span class="tag">${g.kind}</span></div>`)}
        <div class="muted small plan-total"><span></span>${j.steps.length} steps in total · one host at a time · the job stops at the first failure</div>`);
      err.classList.add("hidden"); planOK = true;
    } catch (e) {
      mount(el.querySelector("[data-plan]"), html``);
      err.textContent = e.message; err.classList.remove("hidden"); planOK = false;
    }
    check();
  };
  el.addEventListener("change", plan);
  el.addEventListener("input", (e) => { if (e.target.name === "confirm") check(); });
  el.querySelector("[data-run]").addEventListener("click", async () => {
    try {
      const j = await api("/api/jobs", { method: "POST", body: req() });
      m.close();
      location.hash = "#/job/" + j.id;
    } catch (e) { const err = el.querySelector("[data-err]"); err.textContent = e.message; err.classList.remove("hidden"); }
  });
  plan();
}

// ---------------------------------------------------------------- jobs
export function jobsPage(el) {
  top("Jobs", html`<a href="#/">Overview</a>`);
  let alive = true;
  api("/api/jobs").then((jobs) => {
    if (!alive) return;
    mount(el, html`<div class="card">${jobs.length ? html`<table class="table"><thead><tr><th>Job</th><th>System</th><th>State</th><th>Steps</th><th>Started</th><th>Duration</th></tr></thead><tbody>
      ${jobs.map((j) => html`<tr class="click" data-href="#/job/${j.id}"><td><b>${j.title}</b></td><td>${j.sid ? html`<span class="chip">${j.sid}</span>` : ""}</td><td>${jobBadge(j.state)}</td>
        <td class="small">${j.steps.filter((s) => s.state === "ok").length}/${j.steps.length}</td><td class="small muted">${ago(j.started || j.created)}</td><td class="small">${duration(j.started, j.ended)}</td></tr>`)}
      </tbody></table>` : html`<div class="empty">No jobs yet. Open a system and start an operation.</div>`}</div>`);
    on(el, "click", "[data-href]", (_, r) => { location.hash = r.dataset.href; });
  });
  return () => { alive = false; };
}
jobsPage.live = true;

const LINE_CLASS = (t) => (/\.\.\. ok\b|^\s*✔/.test(t) ? "okl" : /FAIL|✘|error|failed/i.test(t) ? "bad" : /^\s*!|warning/i.test(t) ? "warn" : "");

export function jobPage(el, id) {
  top("Job", html`<a href="#/jobs">Jobs</a>`);
  mount(el, html`<div class="card card-b" data-head><span class="spin"></span></div>
    <div class="grid jobcols">
      <div class="card"><div class="card-h"><h2>Console</h2><div class="grow"></div><label class="check small"><input type="checkbox" data-follow checked> follow</label><button class="sm" data-dl>Download log</button></div><div class="console" data-console></div></div>
      <div class="card"><div class="card-h"><h2>Steps</h2></div><div class="steps" data-steps></div></div></div>`);
  const con = $("[data-console]", el);
  const lines = [];
  let job = null;
  const head = () => {
    if (!job) return;
    top(job.title, html`<a href="#/jobs">Jobs</a>${job.sid ? html` / <a href="#/system/${job.sid}">${job.sid}</a>` : ""}`);
    const running = job.state === "running" || job.state === "queued";
    const done = job.steps.filter((s) => s.state === "ok").length;
    mount($("[data-head]", el), html`<div class="jobhead">${jobBadge(job.state)}<span>${done}/${job.steps.length} steps</span><span class="muted">started ${clock(job.started) || "—"} · ${duration(job.started, job.ended)}</span>
      <span class="grow"></span>${running ? html`<button class="danger sm" data-cancel>${icon("stop", 13)} Stop after the current step</button>` : ""}</div>
      <div class="meter"><i data-w="${job.steps.length ? (done * 100) / job.steps.length : 0}"></i></div>`);
    mount($("[data-steps]", el), html`${job.steps.map((s, i) => html`<div class="step ${s.state}"><span class="ic">${s.state === "ok" ? "✓" : s.state === "failed" ? "✕" : s.state === "skipped" ? "–" : i + 1}</span>
      <div><div class="t">${s.title}</div><div class="d">${s.kind}${s.started ? " · " + duration(s.started, s.ended) : ""}</div>${s.detail ? html`<div class="err">${s.detail}</div>` : ""}</div></div>`)}`);
  };
  on(el, "click", "[data-cancel]", async () => {
    if (await confirmDialog("Stop this job?", html`<p>The running step finishes — an extraction or backup is never cut in half — and every following step is skipped.</p>`, "Stop after this step", true)) {
      try { await api(`/api/jobs/${id}/cancel`, { method: "POST" }); } catch (e) { toast(e.message, true); }
    }
  });
  on(el, "click", "[data-dl]", () => {
    const text = lines.map((l) => `${l.t} ${l.host || "-"} ${l.text}`).join("\n");
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `kernelman-job-${id}.log`;
    a.click();
  });
  const add = (l) => {
    lines.push(l);
    const d = document.createElement("div");
    if (l.kind === "step") { d.className = "cstep"; d.textContent = "▶ " + (l.host ? l.host + " · " : "") + l.text; } else {
      d.className = "l " + l.kind;
      const ts = document.createElement("span"); ts.className = "ts"; ts.textContent = clock(l.t);
      const h = document.createElement("span"); h.className = "h"; h.textContent = l.host || "hub";
      const x = document.createElement("span"); x.className = "x " + LINE_CLASS(l.text); x.textContent = l.text;
      d.append(ts, h, x);
    }
    con.append(d);
  };
  const es = new EventSource(`/api/jobs/${id}/events?after=0`);
  es.addEventListener("line", (e) => {
    add(JSON.parse(e.data));
    if ($("[data-follow]", el).checked) con.scrollTop = con.scrollHeight;
  });
  es.addEventListener("job", (e) => { job = JSON.parse(e.data); head(); });
  es.addEventListener("end", () => { es.close(); refresh().then(head); }); // nav, counters and header only: re-rendering this page would reopen the stream
  es.onerror = () => { if (job && !["running", "queued"].includes(job.state)) es.close(); };
  return () => es.close();
}

// ---------------------------------------------------------------- archives
export function archivesPage(el) {
  top("Archives", html`<a href="#/">Overview</a>`);
  const draw = (lib) => {
    mount(el, html`<div class="card card-b"><div class="drop" data-drop>${icon("upload", 26)}<div><b>Drop SAP kernel archives here</b> or click to choose</div>
        <div class="small">SAPEXE_*.SAR, SAPEXEDB_*.SAR, dw_*.sar, igsexe_*.sar … and SAPCAR. Download them from the SAP Software Center on this PC, the hub pushes them to the hosts.</div>
        <input type="file" multiple data-file class="hidden" accept=".sar,.SAR,.exe,.EXE"></div><div class="form hidden" data-up></div></div>
      <div class="card">${lib.length ? html`<table class="table"><thead><tr><th>Archive</th><th>Component</th><th>Patch</th><th>Size</th><th>Uploaded</th><th>Checksums</th><th></th></tr></thead><tbody>
        ${lib.map((a) => html`<tr><td class="mono"><b>${a.name}</b></td><td>${a.kind === "sapcar" ? "SAPCAR" : a.label}${a.full ? html` <span class="tag">full kernel</span>` : ""}</td><td>${a.patch || ""}</td>
          <td>${bytes(a.size)}</td><td class="small muted">${ago(a.uploaded)}</td><td class="mono small muted" title="SHA-256 ${a.sha256}">cksum ${a.cksum.split(" ")[0]}<br>sha256 ${a.sha256.slice(0, 16)}…</td>
          <td><button class="sm danger" data-del="${a.name}">${icon("trash", 13)}</button></td></tr>`)}</tbody></table>`
        : html`<div class="empty">The library is empty.</div>`}</div>`);
  };
  const load = () => api("/api/library").then(draw).catch((e) => toast(e.message, true));
  on(el, "click", "[data-drop]", (e) => { if (!e.target.matches("input")) $("[data-file]", el).click(); });
  on(el, "change", "[data-file]", (_, inp) => upload([...inp.files]));
  el.addEventListener("dragover", (e) => { const d = e.target.closest("[data-drop]"); if (d) { e.preventDefault(); d.classList.add("over"); } });
  el.addEventListener("dragleave", (e) => { const d = e.target.closest("[data-drop]"); if (d) d.classList.remove("over"); });
  el.addEventListener("drop", (e) => { const d = e.target.closest("[data-drop]"); if (d) { e.preventDefault(); d.classList.remove("over"); upload([...e.dataTransfer.files]); } });
  on(el, "click", "[data-del]", async (_, b) => {
    if (await confirmDialog("Delete " + b.dataset.del + "?", html`<p>The file is removed from the hub library. Copies on the hosts stay.</p>`, "Delete", true)) {
      await api("/api/library/" + encodeURIComponent(b.dataset.del), { method: "DELETE" }); load(); rerender();
    }
  });
  function upload(files) {
    if (!files.length) return;
    const box = $("[data-up]", el);
    box.classList.remove("hidden");
    mount(box, html`<div class="small"><b>Uploading ${files.length} file(s)</b> — <span data-pct>0%</span></div><div class="progress"><i data-bar></i></div>`);
    const fd = new FormData();
    files.forEach((f) => fd.append("file", f, f.name));
    const x = new XMLHttpRequest();
    x.open("POST", "/api/library");
    x.setRequestHeader("X-KernelMan", "1");
    x.upload.onprogress = (e) => { if (e.lengthComputable) { const p = Math.round((e.loaded / e.total) * 100); $("[data-pct]", el).textContent = p + "%"; $("[data-bar]", el).style.width = p + "%"; } };
    x.onload = () => {
      let r = null; try { r = JSON.parse(x.responseText); } catch { /* */ }
      if (x.status !== 200) toast((r && r.error) || "upload failed", true); else toast(r.length + " file(s) stored and checksummed.");
      load(); rerender();
    };
    x.onerror = () => toast("upload failed", true);
    x.send(fd);
  }
  load();
}

// ---------------------------------------------------------------- settings
export function settingsPage(el) {
  top("Settings", html`<a href="#/">Overview</a>`);
  api("/api/settings").then((st) => {
    mount(el, html`<div class="grid two"><div class="grid">
      <form class="card" data-f="settings"><div class="card-h"><h2>Remote directory</h2></div><div class="card-b form">
        <label>Archives pushed from the hub land in<input type="text" name="remote_dir" value="${st.remote_dir}"></label>
        <div class="muted small">When the login user cannot write there, <code>~/.kernelman/download</code> is used.</div>
        <div><button class="primary">Save</button></div></div></form>
      <form class="card" data-f="password"><div class="card-h"><h2>Hub password</h2></div><div class="card-b form">
        <label>Current password<input type="password" name="current" required autocomplete="current-password"></label>
        <label>New password (10+ characters)<input type="password" name="new" required minlength="10" autocomplete="new-password"></label>
        <div class="muted small">Stored SSH passwords are re-encrypted with the new password.</div><div><button class="primary">Change password</button></div></div></form></div>
      <div class="card"><div class="card-h"><h2>${icon("shield")} Safety rules the hub enforces</h2></div><div class="att">
        ${["Every SAP system is read-only until you unlock it; an unlock ends after 8 hours.",
          "Stop, Kernel Update and Rollback need the SID typed again and show their plan first.",
          "A Kernel Update refuses to run while an application server of the system is not in the hub.",
          "Preflight on every host before anything changes: system present, kernel directory known, free space.",
          "After extracting, every instance directory is compared with the central kernel; SAP is not started when one differs.",
          "One job at a time; a stop request waits for the running step — SAPCAR and backups are never cut in half.",
          "Archives are checked with cksum on the host after the push; host keys are pinned on first contact.",
          "SSH passwords are encrypted with AES-256-GCM using a key derived from the hub password."].map((t) => html`<div class="warn"><span class="i">✓</span><div>${t}</div></div>`)}
      </div><div class="card-b muted small">KernelMan ${S.session.version} (${S.session.commit}) · data in <code>${S.session.data_dir}</code></div></div></div>`);
    on(el, "submit", "form", async (e, f) => {
      e.preventDefault();
      const d = Object.fromEntries(new FormData(f));
      try {
        if (f.dataset.f === "settings") { await api("/api/settings", { method: "PUT", body: { remote_dir: d.remote_dir } }); toast("Saved."); }
        else { await api("/api/password", { method: "POST", body: { current: d.current, new: d.new } }); f.reset(); toast("Password changed."); }
      } catch (ex) { toast(ex.message, true); }
    });
  });
}
