// Core helpers: escaping templates, API calls, toasts, modals, formatting.

const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
export const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ESC[c]);

// html`...` escapes every interpolated value unless it is itself html`` or raw().
export const raw = (s) => ({ __html: String(s) });
const part = (v) => {
  if (v === null || v === undefined || v === false) return "";
  if (Array.isArray(v)) return v.map(part).join("");
  if (typeof v === "object" && "__html" in v) return v.__html;
  return esc(v);
};
export function html(strings, ...vals) {
  let out = strings[0];
  vals.forEach((v, i) => { out += part(v) + strings[i + 1]; });
  return { __html: out };
}
// mount renders a template; widths come from data-w (inline styles are blocked by the CSP).
export function mount(el, tpl) {
  el.innerHTML = tpl.__html;
  el.querySelectorAll("[data-w]").forEach((e) => { e.style.width = Math.max(0, Math.min(100, +e.dataset.w)) + "%"; });
  return el;
}
export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
export function on(root, type, sel, fn) {
  root.addEventListener(type, (ev) => {
    const t = ev.target.closest(sel);
    if (t && root.contains(t)) fn(ev, t);
  });
}

export class ApiError extends Error {
  constructor(status, msg) { super(msg); this.status = status; }
}
// api calls the hub; every request carries the anti-CSRF header.
export async function api(path, { method = "GET", body } = {}) {
  const opt = { method, headers: { "X-KernelMan": "1" }, credentials: "same-origin" };
  if (body !== undefined) { opt.headers["Content-Type"] = "application/json"; opt.body = JSON.stringify(body); }
  const r = await fetch(path, opt);
  let data = null;
  try { data = await r.json(); } catch { /* empty */ }
  if (!r.ok) {
    if (r.status === 401 && !path.startsWith("/api/login")) window.dispatchEvent(new Event("km:logout"));
    throw new ApiError(r.status, (data && data.error) || r.statusText);
  }
  return data;
}

export function toast(msg, err = false) {
  let box = $(".toasts");
  if (!box) { box = document.createElement("div"); box.className = "toasts"; document.body.append(box); }
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.textContent = msg;
  box.append(t);
  setTimeout(() => t.remove(), err ? 9000 : 4500);
}

// modal opens a dialog; returns {el, close}.
export function modal(tpl, { onClose, wide } = {}) {
  const back = document.createElement("div");
  back.className = "backdrop";
  mount(back, html`<div class="modal ${wide ? "wide" : ""}" role="dialog" aria-modal="true">${tpl}</div>`);
  const close = () => { back.remove(); document.removeEventListener("keydown", esc_); onClose && onClose(); };
  const esc_ = (e) => { if (e.key === "Escape") close(); };
  back.addEventListener("mousedown", (e) => { if (e.target === back) close(); });
  on(back, "click", "[data-close]", close);
  document.addEventListener("keydown", esc_);
  document.body.append(back);
  return { el: back, close };
}

export async function confirmDialog(title, text, okLabel = "OK", danger = false) {
  return new Promise((resolve) => {
    let done = false;
    const m = modal(html`<div class="card-h"><h2>${title}</h2></div><div class="card-b">${text}</div>
      <div class="modal-f"><button data-close>Cancel</button><button class="${danger ? "danger solid" : "primary"}" data-ok>${okLabel}</button></div>`,
    { onClose: () => { if (!done) resolve(false); } });
    m.el.querySelector("[data-ok]").addEventListener("click", () => { done = true; m.close(); resolve(true); });
  });
}

// formatting
export const STATE = { GREEN: "Running", YELLOW: "Partial", RED: "Error", GRAY: "Stopped" };
export const badge = (st, label) => html`<span class="badge s-${st || "GRAY"}">${label || STATE[st] || st || "Unknown"}</span>`;
export const JOBSTATE = { queued: ["Queued", "BLUE"], running: ["Running", "BLUE"], ok: ["Done", "GREEN"], failed: ["Failed", "RED"], cancelled: ["Stopped", "GRAY"], interrupted: ["Interrupted", "YELLOW"] };
export const jobBadge = (s) => { const [l, c] = JOBSTATE[s] || [s, "GRAY"]; return html`<span class="badge s-${c}">${l}</span>`; };
export function ago(t) {
  if (!t || t.startsWith("0001")) return "never";
  const s = (Date.now() - new Date(t)) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + " min ago";
  if (s < 86400) return Math.floor(s / 3600) + " h ago";
  return new Date(t).toLocaleDateString();
}
export const clock = (t) => (t && !t.startsWith("0001") ? new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }) : "");
export const hhmm = (t) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false });
export function duration(a, b) {
  if (!a || a.startsWith("0001")) return "";
  const end = b && !b.startsWith("0001") ? new Date(b) : new Date();
  let s = Math.max(0, Math.round((end - new Date(a)) / 1000));
  const m = Math.floor(s / 60); s %= 60;
  return m ? `${m}m ${String(s).padStart(2, "0")}s` : `${s}s`;
}
export function bytes(b) {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(1) + " GB";
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + " MB";
  if (b >= 1 << 10) return (b / (1 << 10)).toFixed(0) + " KB";
  return b + " B";
}
export const kernelShort = (k) => (k || "").replace(" Patch ", " PL ");

// icons (inline SVG, stroke = currentColor)
const P = {
  grid: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
  server: "M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01",
  layers: "M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  play: "M6 4l14 8-14 8z", stop: "M6 6h12v12H6z", refresh: "M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5",
  list: "M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01", file: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6",
  gear: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 7.1 19.8l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.6 1.6 0 0 0 3.2 14H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.2 7.1l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.6 1.6 0 0 0 9.7 3.2V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.1l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0 1.1 2.7H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1.3z",
  lock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4", unlock: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 7.9-1",
  upload: "M12 16V4M7 9l5-5 5 5M4 20h16", archive: "M3 4h18v4H3zM5 8v12h14V8M10 12h4",
  shield: "M12 2 4 5v6c0 5 3.4 9.4 8 11 4.6-1.6 8-6 8-11V5z", plus: "M12 5v14M5 12h14", trash: "M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14",
  undo: "M3 7v6h6M3 13a9 9 0 1 0 3-7.7L3 8", logout: "M9 21H5V3h4M16 17l5-5-5-5M21 12H9", moon: "M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z",
  cpu: "M6 6h12v12H6zM9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4",
  chevron: "M6 9l6 6 6-6", search: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.3-4.3",
};
export const icon = (name, size = 16) => raw(`<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${P[name] || ""}"/></svg>`);
export const logo = (size = 30) => raw(`<svg width="${size}" height="${size}" viewBox="0 0 32 32" aria-hidden="true"><rect width="32" height="32" rx="8" fill="#2f5bea"/><path d="M9 8v16M9 16l9-8M12.5 13l6.5 11" stroke="#fff" stroke-width="3" fill="none" stroke-linecap="round" stroke-linejoin="round"/><circle cx="24" cy="9" r="3" fill="#5ee08a"/></svg>`);
