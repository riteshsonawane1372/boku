"use strict";
// Boku web UI. No build step and no dependencies: every node is created with
// h(), which only ever sets text, so content that came from the web (topics,
// findings, source titles) is never parsed as HTML.

// ---------- helpers ----------

const $ = (sel, root = document) => root.querySelector(sel);

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false || k === "value") continue;
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "data") Object.assign(el.dataset, v);
    else el.setAttribute(k, v === true ? "" : v);
  }
  add(el, kids);
  if (attrs && attrs.value != null) el.value = attrs.value;
  return el;
}
function add(el, kids) {
  for (const k of kids.flat(Infinity)) {
    if (k == null || k === false || k === "") continue;
    el.append(k.nodeType ? k : document.createTextNode(String(k)));
  }
  return el;
}
function fill(el, ...kids) { el.replaceChildren(); return add(el, kids); }

const ICONS = {
  plus: '<path d="M12 5v14M5 12h14"/>',
  runs: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  doc: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.8 1.2V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-2.9-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.7 1.7 0 0 0 3 14H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.2-2.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.7 1.7 0 0 0 10 3V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 2.9 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1A1.7 1.7 0 0 0 21 10h0a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  chev: '<path d="M9 6l6 6-6 6"/>',
  check: '<path d="M5 12l5 5 9-10"/>',
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
  bang: '<path d="M12 7v6M12 17h.01"/>',
  play: '<path d="M7 5l12 7-12 7z"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="1.5"/>',
  refresh: '<path d="M20 11a8 8 0 1 0 -2.3 6.3M20 5v6h-6"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h9"/>',
  open: '<path d="M14 4h6v6M20 4l-9 9M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5"/>',
  down: '<path d="M12 4v12M6 11l6 6 6-6M5 20h14"/>',
};
function icon(name) {
  const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  s.setAttribute("class", "i");
  s.setAttribute("viewBox", "0 0 24 24");
  s.setAttribute("aria-hidden", "true");
  s.innerHTML = ICONS[name]; // static markup from the table above
  return s;
}

async function api(method, url, body) {
  const opt = { method, headers: { "X-Boku": "1" } };
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(url, opt);
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch { data = { error: text.trim() || res.statusText }; }
  if (!res.ok) {
    const err = new Error(data.error || res.statusText);
    err.errors = data.errors && data.errors.length ? data.errors : [err.message];
    throw err;
  }
  return data;
}

function toast(msg, kind = "") {
  const t = h("div", { class: "toast " + kind }, msg);
  $("#toasts").append(t);
  setTimeout(() => t.remove(), kind === "err" ? 9000 : 4000);
}
const oops = (e) => toast(e.errors ? e.errors.join("\n") : String(e), "err");

async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast("Copied", "ok"); }
  catch { toast("Could not copy; select the text instead", "err"); }
}

const clone = (v) => JSON.parse(JSON.stringify(v));
const blank = (v) => v == null || v === "" || (Array.isArray(v) && !v.length) || (typeof v === "object" && !Array.isArray(v) && !Object.keys(v).length);
const eq = (a, b) => (blank(a) && blank(b)) || JSON.stringify(a) === JSON.stringify(b);
const getPath = (o, p) => p.split(".").reduce((x, k) => (x == null ? undefined : x[k]), o);
function setPath(o, p, v) {
  const ks = p.split(".");
  const last = ks.pop();
  for (const k of ks) o = o[k] && typeof o[k] === "object" ? o[k] : (o[k] = {});
  o[last] = v;
}
function delPath(o, p) {
  const ks = p.split(".");
  const chain = [o];
  for (const k of ks.slice(0, -1)) { o = o[k]; if (!o) return; chain.push(o); }
  delete o[ks[ks.length - 1]];
  for (let i = chain.length - 1; i > 0; i--) if (!Object.keys(chain[i]).length) delete chain[i - 1][ks[i - 1]];
}
function merge(base, over) {
  for (const [k, v] of Object.entries(over || {})) {
    if (v && typeof v === "object" && !Array.isArray(v) && base[k] && typeof base[k] === "object" && !Array.isArray(base[k])) merge(base[k], v);
    else base[k] = clone(v);
  }
  return base;
}
// leaves lists [path, value] for every setting in a nested document.
function leaves(o, prefix = "") {
  const out = [];
  for (const [k, v] of Object.entries(o || {})) {
    const p = prefix ? prefix + "." + k : k;
    if (v && typeof v === "object" && !Array.isArray(v) && p !== "agents.role_models") out.push(...leaves(v, p));
    else out.push([p, v]);
  }
  return out;
}
const show = (v) => (Array.isArray(v) ? v.join(", ") || "none" : v && typeof v === "object" ? Object.entries(v).map(([k, x]) => `${k}=${x || "default"}`).join(", ") || "none" : v === "" ? '""' : String(v));

const usd = (n) => "$" + (n || 0).toFixed(2);
function bytes(n) {
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(0) + " KB";
  return (n / 1048576).toFixed(1) + " MB";
}
function dur(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m " + String(s % 60).padStart(2, "0") + "s";
  return Math.floor(s / 3600) + "h " + String(Math.floor((s % 3600) / 60)).padStart(2, "0") + "m";
}
// goDur parses a Go duration string ("3m28s") into milliseconds.
function goDur(s) {
  let ms = 0;
  for (const [, n, u] of String(s || "").matchAll(/([\d.]+)(h|ms|m|s)/g)) ms += parseFloat(n) * { h: 3600000, m: 60000, s: 1000, ms: 1 }[u];
  return ms;
}
function ago(t) {
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + " min ago";
  if (s < 86400) return Math.floor(s / 3600) + " h ago";
  if (s < 86400 * 14) return Math.floor(s / 86400) + " d ago";
  return new Date(t).toLocaleDateString();
}
const when = (t) => new Date(t).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
const shq = (s) => (/^[\w./:@%+=,-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'");

function yamlScalar(v) {
  if (typeof v !== "string") return String(v);
  const plain = /^[\w./:@~-]+$/.test(v) && !/^(true|false|null|yes|no|on|off|~|-?\d+(\.\d+)?)$/i.test(v);
  return plain ? v : JSON.stringify(v);
}
function toYAML(o, ind = "") {
  let out = "";
  for (const [k, v] of Object.entries(o)) {
    if (Array.isArray(v)) out += `${ind}${k}: [${v.map(yamlScalar).join(", ")}]\n`;
    else if (v && typeof v === "object") {
      const inner = toYAML(v, ind + "  ");
      out += inner ? `${ind}${k}:\n${inner}` : `${ind}${k}: {}\n`;
    } else if (v != null) out += `${ind}${k}: ${yamlScalar(v)}\n`;
  }
  return out;
}

function store(key, value) {
  try {
    if (value === undefined) return JSON.parse(localStorage.getItem(key));
    localStorage.setItem(key, JSON.stringify(value));
  } catch { /* private window or blocked storage: the UI works without it */ }
  return null;
}

function dialog(title, body, actions) {
  const d = h("dialog", {},
    h("h3", {}, title),
    h("div", { class: "body" }, body),
    h("div", { class: "actions" },
      h("button", { class: "btn", type: "button", onclick: () => d.close() }, "Cancel"),
      actions.map((a) => h("button", { class: "btn " + (a.cls || "primary"), type: "button", onclick: async () => { if ((await a.fn()) !== false) d.close(); } }, a.label))));
  d.addEventListener("close", () => d.remove());
  document.body.append(d);
  d.showModal();
  return d;
}

// ---------- state ----------

const MODES = [
  { id: "full", name: "Full report", desc: "Full research programme, fact-checking and a typeset report with cover and contents." },
  { id: "short", name: "Short", desc: "A search report of a few pages: fewer workstreams, compact layout." },
  { id: "quick", name: "Quick (local)", desc: "Everything on the local Ollama model. No Claude Code tokens, no fact-check." },
  { id: "explainer", name: "Explainer", desc: "Visual explainer of a topic or a local codebase: concepts, diagrams, walkthroughs." },
  { id: "whitepaper", name: "Whitepaper", desc: "Academic paper format: abstract, numbered sections, figures, reference list." },
];
const STAGE_LABEL = { plan: "Plan", research: "Research", factcheck: "Fact-check", synthesis: "Synthesis", editorial: "Write & edit", report: "Quality gates", render: "Render & publish" };
const STAGE_COLOR = { plan: "var(--violet)", research: "var(--accent)", factcheck: "var(--amber)", synthesis: "var(--green)", editorial: "var(--pink)", report: "var(--faint)", render: "var(--faint)" };

const S = {
  meta: null,
  baselines: {}, // mode → configuration before the user's changes
  doctor: null,
  runs: [],
  runsDir: "",
  draft: { topic: "", mode: "full", subject: "topic", codebase: "", focus: "", overrides: {} },
  check: { errors: [], min_sources: 0 },
  open: new Set(["research", "report"]),
};

// ---------- settings form ----------

// Flags that express a setting on the command line; null when the value has
// no flag form and must go in boku.yaml.
const FLAGS = {
  "research.depth": (v) => ["--depth", v],
  "agents.max_agents": (v) => (v > 0 ? ["--agents", v] : null),
  "agents.max_parallel": (v) => (v > 0 ? ["--parallel", v] : null),
  "agents.max_cost_usd": (v) => ["--max-cost", v],
  "research.freshness_days": (v) => ["--freshness", v + "d"],
  "research.sources": (v) => (v.length ? ["--sources", v.join(",")] : null),
  "agents.model": (v) => (v ? ["--model", v] : null),
  "research.max_iterations": (v) => ["--iterations", v],
  "research.min_sources": (v) => (v > 0 ? ["--min-sources", v] : null),
  "report.formats": (v) => (v.length ? ["--format", v.join(",")] : null),
  "report.include_references": (v) => (v ? ["--save-ref"] : null),
  "local.model": (v) => (v ? ["--local-model", v] : null),
  "output.directory": (v) => (v ? ["--output", v] : null),
};
const FLAG_NAME = {
  "research.depth": "--depth", "agents.max_agents": "--agents", "agents.max_parallel": "--parallel", "agents.max_cost_usd": "--max-cost",
  "research.freshness_days": "--freshness", "research.sources": "--sources", "agents.model": "--model", "research.max_iterations": "--iterations",
  "research.min_sources": "--min-sources", "report.formats": "--format", "report.include_references": "--save-ref", "local.model": "--local-model",
  "output.directory": "--output",
};

const remote = (c) => c.agents.provider !== "claude-code";
const SECTIONS = [
  { id: "general", title: "General", desc: "Project name and the default report mode", only: "settings", fields: [
    { p: "project.name", t: "text", l: "Project name" },
    { p: "report.mode", t: "seg", o: MODES.map((m) => m.id), l: "Default mode", h: "The mode `boku report` uses when none is given." },
  ] },
  { id: "research", title: "Research", desc: "How much to look up and how fresh it must be", fields: [
    { p: "research.depth", t: "seg", o: ["quick", "standard", "deep"], l: "Depth", h: "Sets how many distinct sources the research gate expects: quick 8, standard 20, deep 35." },
    { p: "agents.max_agents", t: "int", min: 1, max: 20, l: "Research workstreams", h: "The most parallel research agents the planner may create." },
    { p: "research.freshness_days", t: "int", min: 1, l: "Freshness window (days)", h: "Dated information inside this window counts as current.", presets: [[30, "30d"], [90, "90d"], [180, "6m"], [365, "1y"], [730, "2y"]] },
    { p: "research.min_sources", t: "int", min: 0, l: "Minimum sources", h: "Distinct sources the research gate requires. 0 derives it from depth." },
    { p: "research.fact_check", t: "bool", l: "Independent fact-check", h: "A separate agent reviews every finding and can reject weak claims. Quick mode turns it off." },
    { p: "research.max_iterations", t: "int", min: 0, max: 10, l: "Fact-check follow-up rounds", h: "Rounds of extra research the fact-checker may request. 0 checks once and marks what stays unverified." },
    { p: "research.sources", t: "list", wide: true, l: "Preferred sources", ph: "arxiv.org, official documentation, SEC filings", h: "Domains or kinds of source the planner should favour, comma-separated." },
  ] },
  { id: "report", title: "Report", desc: "Layout, formats and what goes on the page", fields: [
    { p: "report.formats", t: "multi", o: ["pdf", "html", "md"], l: "Formats", h: "PDF needs Chrome or Chromium." },
    { p: "report.layout", t: "seg", o: ["auto", "full", "compact", "paper"], l: "Layout", h: "auto lets the editor choose; full has a cover and contents; compact a title block; paper is the academic layout." },
    { p: "report.page_size", t: "seg", o: ["A4", "Letter"], l: "Page size" },
    { p: "report.author", t: "text", l: "Author", ph: "Shown on the cover" },
    { p: "report.include_references", t: "bool", l: "Reference list in the report", h: "Print the numbered sources and evidence register. They are always saved to <report>.references.json." },
    { p: "report.citations", t: "bool", l: "Inline citations" },
    { p: "report.charts", t: "bool", l: "Charts", h: "Data charts drawn from cited findings." },
    { p: "report.diagrams", t: "bool", l: "Diagrams" },
    { p: "report.chrome", t: "text", mono: true, wide: true, l: "Chrome path", ph: "Empty = autodetect", h: "Browser used to print the PDF." },
  ] },
  { id: "agents", title: "Agents & models", desc: "Which runtime does the work, and its limits", fields: [
    { p: "agents.provider", t: "seg", o: ["claude-code", "ollama", "openai"], wide: true, l: "Provider", h: "claude-code drives the Claude Code CLI; ollama a local model; openai any OpenAI-compatible endpoint (vLLM, LM Studio, OpenRouter)." },
    { p: "agents.model", t: "text", mono: true, l: "Model", ph: "Provider default, e.g. opus, sonnet", h: "Used by every agent unless a role overrides it. Required for ollama and openai." },
    { p: "agents.command", t: "text", mono: true, l: "Claude Code command", show: (c) => !remote(c), h: "The Claude Code executable." },
    { p: "agents.endpoint", t: "text", mono: true, l: "Endpoint", show: remote, ph: "http://localhost:8000/v1", h: "Base URL of the model server. Ollama falls back to the local endpoint." },
    { p: "agents.api_key_env", t: "text", mono: true, l: "API key variable", show: (c) => c.agents.provider === "openai", ph: "OPENAI_API_KEY", h: "Name of the environment variable holding the key; the key itself is never stored." },
    { p: "agents.context_tokens", t: "int", min: 0, l: "Context tokens", show: (c) => c.agents.provider === "ollama", h: "Context window requested from Ollama. 0 uses the local model's setting." },
    { p: "agents.price_input_per_mtok", t: "float", min: 0, l: "Input price (USD / M tokens)", show: remote, h: "For cost tracking. 0 = free." },
    { p: "agents.price_output_per_mtok", t: "float", min: 0, l: "Output price (USD / M tokens)", show: remote },
    { p: "agents.max_parallel", t: "int", min: 1, max: 32, l: "Agents at once", h: "Concurrently running agents." },
    { p: "agents.max_cost_usd", t: "float", min: 0, l: "Budget cap (USD)", h: "Stop launching agents once spend reaches this. 0 = unlimited." },
    { p: "agents.max_retries", t: "int", min: 0, max: 10, l: "Retries per agent call" },
    { p: "agents.timeout", t: "text", mono: true, l: "Timeout per agent call", ph: "20m", h: "A duration such as 90s, 20m or 1h." },
    { p: "agents.role_models", t: "rolemap", wide: true, l: "Model per role", h: "Override the model for single roles, for example opus for editorial and sonnet for research." },
    { p: "agents.env_passthrough", t: "list", wide: true, mono: true, l: "Environment passthrough", ph: "AWS_PROFILE, AWS_REGION", h: "Extra environment variables agents may see, for example for Claude Code on Bedrock." },
  ] },
  { id: "local", title: "Local model", desc: "Ollama, for quick mode, formatting and chosen roles", fields: [
    { p: "local.model", t: "text", mono: true, l: "Ollama model", ph: "llama3.1:8b" },
    { p: "local.endpoint", t: "text", mono: true, l: "Ollama endpoint" },
    { p: "local.context_tokens", t: "int", min: 1024, l: "Context tokens" },
    { p: "local.format", t: "bool", l: "Local formatting pass", h: "Fix style problems on the local model instead of another editorial call. Skipped when Ollama is unreachable." },
    { p: "local.roles", t: "roles", wide: true, l: "Roles on the local model", h: "These roles run on Ollama instead of the main provider. Quick mode routes all of them." },
  ] },
  { id: "search", title: "Web search", desc: "Boku's own retrieval, for ollama, openai and local roles", fields: [
    { p: "search.engine", t: "seg", o: ["duckduckgo", "searxng"], l: "Engine", h: "Claude Code searches by itself; this applies to the other providers." },
    { p: "search.searxng_url", t: "text", mono: true, l: "SearXNG URL", show: (c) => c.search.engine === "searxng", ph: "http://localhost:8080" },
    { p: "search.results_per_query", t: "int", min: 1, max: 50, l: "Results per query" },
    { p: "search.max_pages", t: "int", min: 1, max: 100, l: "Pages per research task" },
    { p: "search.page_chars", t: "int", min: 500, l: "Characters kept per page" },
  ] },
  { id: "output", title: "Output", desc: "Where reports and prompts live", fields: [
    { p: "output.directory", t: "text", mono: true, l: "Reports directory" },
    { p: "output.runs_directory", t: "text", mono: true, l: "Runs directory", only: "settings", h: "One directory per run, with every intermediate artifact." },
    { p: "output.prompts_directory", t: "text", mono: true, wide: true, l: "Prompts directory", ph: "Empty = built-in prompts", h: "Override the embedded prompts with files from this directory." },
  ] },
];

const typeDefault = { text: "", list: [], multi: [], roles: [], rolemap: {}, int: 0, float: 0, bool: false, seg: "" };

// renderForm draws every settings section into root. ctx supplies the values:
//   cfg()      effective configuration
//   base(p)    the value a setting has when the user leaves it alone
//   set(p, v)  store a change        reset(p)  drop it
//   settings   true on the settings page (shows settings-only fields)
function renderForm(root, ctx) {
  const marks = [];
  const val = (f) => { const v = getPath(ctx.cfg(), f.p); return v == null ? clone(typeDefault[f.t]) : v; };
  const changed = (f) => !eq(val(f), ctx.base(f.p));
  const redraw = () => {
    const key = document.activeElement && document.activeElement.dataset.k;
    renderForm(root, ctx);
    if (key) { const el = root.querySelector(`[data-k="${CSS.escape(key)}"]`); if (el) el.focus(); }
  };
  // soft: the value changed but the layout did not, so keep the DOM (and the caret).
  const soft = (f, v) => { ctx.set(f.p, v); marks.forEach((m) => m()); };
  const hard = (f, v) => { ctx.set(f.p, v); redraw(); };

  function control(f) {
    const v = val(f);
    switch (f.t) {
      case "seg":
        return h("div", { class: "seg", role: "group", "aria-label": f.l }, f.o.map((o) =>
          h("button", { type: "button", "aria-pressed": String(v === o), data: { k: f.p + "=" + o }, onclick: () => hard(f, o) }, o)));
      case "bool":
        return h("label", { class: "switch" },
          h("input", { type: "checkbox", checked: !!v, data: { k: f.p }, onchange: (e) => hard(f, e.target.checked) }), h("i"),
          h("span", { class: "muted" }, v ? "On" : "Off"));
      case "multi":
        return h("div", { class: "chips" }, f.o.map((o) =>
          h("button", { type: "button", class: "chip", "aria-pressed": String(v.includes(o)), data: { k: f.p + "=" + o },
            onclick: () => hard(f, f.o.filter((x) => (x === o ? !v.includes(o) : v.includes(x)))) }, o)));
      case "roles": {
        const all = v.includes("*");
        const toggle = (r) => hard(f, r === "*" ? (all ? [] : ["*"]) : S.meta.roles.filter((x) => (x === r ? !v.includes(r) : v.includes(x))));
        return h("div", { class: "chips" },
          h("button", { type: "button", class: "chip", "aria-pressed": String(all), data: { k: f.p + "=*" }, onclick: () => toggle("*") }, "all roles"),
          S.meta.roles.map((r) => h("button", { type: "button", class: "chip", disabled: all, "aria-pressed": String(all || v.includes(r)), data: { k: f.p + "=" + r }, onclick: () => toggle(r) }, r)));
      }
      case "rolemap": {
        const base = ctx.base(f.p) || {};
        return h("div", { class: "rolemap" }, S.meta.roles.filter((r) => r !== "formatter").map((r) =>
          h("label", {}, r, h("input", { type: "text", class: "mono", value: v[r] || "", placeholder: getPath(ctx.cfg(), "agents.model") || "default", data: { k: f.p + "." + r },
            oninput: (e) => {
              const next = { ...val(f) };
              const m = e.target.value.trim();
              // An empty entry is kept only to cancel one from boku.yaml.
              if (m || base[r]) next[r] = m; else delete next[r];
              soft(f, next);
            } }))));
      }
      case "list":
        return h("input", { type: "text", class: f.mono ? "mono" : "", value: v.join(", "), placeholder: f.ph, data: { k: f.p },
          oninput: (e) => soft(f, e.target.value.split(",").map((s) => s.trim()).filter(Boolean)) });
      case "int": case "float":
        return h("input", { type: "number", class: "mono", value: v, min: f.min, max: f.max, step: f.t === "int" ? 1 : "any", data: { k: f.p },
          oninput: (e) => { const n = f.t === "int" ? parseInt(e.target.value, 10) : parseFloat(e.target.value); if (!Number.isNaN(n)) soft(f, n); } });
      default:
        return h("input", { type: "text", class: f.mono ? "mono" : "", value: v, placeholder: f.ph, spellcheck: "false", data: { k: f.p }, oninput: (e) => soft(f, e.target.value.trim()) });
    }
  }

  fill(root, SECTIONS.filter((s) => !s.only || ctx.settings).map((sec) => {
    const fields = sec.fields.filter((f) => (!f.only || ctx.settings) && (!f.show || f.show(ctx.cfg())));
    const count = h("span", { class: "n" });
    marks.push(() => { const n = fields.filter(changed).length; count.hidden = !n; count.textContent = n + " changed"; });
    return h("details", { class: "section", open: S.open.has(sec.id), ontoggle: (e) => (e.target.open ? S.open.add(sec.id) : S.open.delete(sec.id)) },
      h("summary", {}, h("h3", {}, sec.title), h("span", { class: "desc" }, sec.desc), count, h("span", { class: "chev" }, icon("chev"))),
      h("div", { class: "fields" }, fields.map((f) => {
        const dot = h("span", { class: "mod", title: "Changed" });
        const reset = h("button", { class: "reset", type: "button", data: { k: f.p + ":reset" }, onclick: () => { ctx.reset(f.p); redraw(); } }, "reset");
        marks.push(() => { dot.hidden = reset.hidden = !changed(f); });
        return h("div", { class: "field" + (f.wide ? " wide" : "") },
          h("div", { class: "lab" }, f.l, dot, reset, FLAG_NAME[f.p] && !ctx.settings && h("span", { class: "flag" }, FLAG_NAME[f.p])),
          control(f),
          f.presets && h("div", { class: "presets" }, f.presets.map(([n, label]) => h("button", { type: "button", data: { k: f.p + "~" + n }, onclick: () => hard(f, n) }, label))),
          f.h && h("div", { class: "help" }, f.h));
      })));
  }));
  marks.forEach((m) => m());
}

// ---------- new report ----------

const draftCfg = () => merge(clone(S.baselines[S.draft.mode] || S.meta.base), S.draft.overrides);
const byCodebase = () => S.draft.mode === "explainer" && S.draft.subject === "codebase";
const draftRequest = () => ({
  topic: byCodebase() ? "" : S.draft.topic,
  mode: S.draft.mode,
  codebase: byCodebase() ? S.draft.codebase : "",
  focus: byCodebase() ? S.draft.focus : "",
  overrides: S.draft.overrides,
});

// cliFor returns the command that starts the same run from a terminal, and
// the boku.yaml fragment for settings that have no flag.
function cliFor(d) {
  const parts = ["boku"];
  const subject = d.mode === "explainer" && d.subject === "codebase"
    ? [shq(d.codebase || "<directory>"), d.focus && shq(d.focus)].filter(Boolean)
    : [shq(d.topic.trim() || "<topic>")];
  if (d.mode === "explainer") parts.push("explain");
  else if (d.mode === "whitepaper") parts.push("whitepaper");
  else parts.push("report");
  parts.push(...subject);
  if (d.mode === "short" || d.mode === "quick") parts.push("--" + d.mode);
  const rest = {};
  for (const [p, v] of leaves(d.overrides)) {
    const flag = FLAGS[p] && FLAGS[p](v);
    if (flag) parts.push(flag.map((x) => shq(String(x))).join(" "));
    else setPath(rest, p, v);
  }
  return { cmd: parts.join(" "), yaml: toYAML(rest) };
}

function pageNew(main) {
  const d = S.draft;
  const save = () => store("boku-draft", d);
  const form = h("div", { class: "stack" });
  const aside = h("div", { class: "aside" });
  const subject = h("div", { class: "stack" });
  const modes = h("div", { class: "modes" });
  let timer;

  const validate = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      try { S.check = await api("POST", "/api/resolve", draftRequest()); } catch (e) { S.check = { errors: e.errors, min_sources: 0 }; }
      drawAside();
    }, 250);
  };
  const changed = () => { save(); drawAside(); validate(); };
  const ctx = {
    cfg: draftCfg,
    base: (p) => getPath(S.baselines[d.mode], p),
    set: (p, v) => { if (eq(v, ctx.base(p))) delPath(d.overrides, p); else setPath(d.overrides, p, v); changed(); },
    reset: (p) => { delPath(d.overrides, p); changed(); },
  };
  const drawForm = () => renderForm(form, ctx);

  function drawModes() {
    fill(modes, MODES.map((m) => {
      const b = S.baselines[m.id];
      const facts = b ? [b.research.depth, "≤" + b.agents.max_agents + " agents", b.research.fact_check ? b.research.max_iterations + " recheck" : "no fact-check"].join(" · ") : "";
      return h("button", { type: "button", class: "mode", "aria-pressed": String(d.mode === m.id), onclick: () => {
        d.mode = m.id;
        // Drop changes the new mode already implies.
        for (const [p, v] of leaves(d.overrides)) if (eq(v, getPath(S.baselines[m.id], p))) delPath(d.overrides, p);
        drawModes(); drawSubject(); drawForm(); changed();
      } }, h("b", {}, m.name), h("span", {}, m.desc), h("small", {}, facts));
    }));
  }

  function drawSubject() {
    const topic = h("div", { class: "topic" }, h("textarea", {
      value: d.topic, rows: 3, "aria-label": "Research topic",
      placeholder: d.mode === "explainer" ? "What should be explained? e.g. How Raft reaches consensus" : d.mode === "whitepaper" ? "Paper topic, e.g. Sparse attention for long-context transformers" : "What should Boku research? e.g. How Kubernetes is being used for AI infrastructure",
      oninput: (e) => { d.topic = e.target.value; changed(); },
    }));
    if (d.mode !== "explainer") return fill(subject, topic);
    fill(subject,
      h("div", { class: "seg", role: "group", "aria-label": "Explainer subject" }, [["topic", "A topic"], ["codebase", "A local codebase"]].map(([id, label]) =>
        h("button", { type: "button", "aria-pressed": String(d.subject === id), onclick: () => { d.subject = id; drawSubject(); changed(); } }, label))),
      d.subject === "topic" ? topic : h("div", { class: "card stack" },
        h("div", { class: "field" }, h("div", { class: "lab" }, "Repository directory"),
          h("input", { type: "text", class: "mono", value: d.codebase, placeholder: S.meta.cwd, spellcheck: "false", oninput: (e) => { d.codebase = e.target.value; changed(); } }),
          h("div", { class: "help" }, "A path on this machine. Agents read it with read-only file tools and cite file paths as sources. Needs the claude-code provider.")),
        h("div", { class: "field" }, h("div", { class: "lab" }, "Focus ", h("span", { class: "faint" }, "(optional)")),
          h("input", { type: "text", value: d.focus, placeholder: "e.g. how a request is handled", oninput: (e) => { d.focus = e.target.value; changed(); } }))));
  }

  function drawAside() {
    const c = draftCfg();
    const mods = leaves(d.overrides);
    const { cmd, yaml } = cliFor(d);
    const errors = [...(S.check.errors || [])];
    if (!byCodebase() && !d.topic.trim()) errors.unshift("Enter a research topic.");
    if (byCodebase() && !d.codebase.trim()) errors.unshift("Enter the repository directory.");
    const local = (c.local.roles || []).length > 0;
    const sys = (n) => (S.doctor || []).find((x) => x.name === n);
    const notes = [];
    if (c.report.formats.includes("pdf") && sys("chrome") && !sys("chrome").ok) notes.push("Chrome was not found, so the PDF cannot be printed. Install Chrome, set its path under Report, or choose html/md.");
    if (local && sys("ollama") && !sys("ollama").ok) notes.push("This run needs the local model, but Ollama is not reachable: " + sys("ollama").detail);
    if (c.agents.provider === "claude-code" && !(local && c.local.roles.includes("*")) && sys("claude code") && !sys("claude code").ok) notes.push("Claude Code was not found: " + sys("claude code").detail);
    const presets = store("boku-presets") || {};

    fill(aside,
      h("div", { class: "card" },
        h("h3", {}, "This run"),
        h("dl", { class: "kv", style: "margin-top:10px" },
          h("dt", {}, "Mode"), h("dd", {}, d.mode),
          h("dt", {}, "Runtime"), h("dd", {}, local && c.local.roles.includes("*") ? "ollama · " + c.local.model : c.agents.provider + (c.agents.model ? " · " + c.agents.model : "")),
          h("dt", {}, "Depth"), h("dd", {}, c.research.depth),
          h("dt", {}, "Workstreams"), h("dd", {}, "up to " + c.agents.max_agents + ", " + c.agents.max_parallel + " at once"),
          h("dt", {}, "Source gate"), h("dd", {}, S.check.min_sources ? "≥ " + S.check.min_sources + " sources" : "–"),
          h("dt", {}, "Fact-check"), h("dd", {}, c.research.fact_check ? "on, " + c.research.max_iterations + " follow-up" : "off"),
          h("dt", {}, "Output"), h("dd", {}, c.report.formats.join(", ") + " · " + c.report.layout),
          h("dt", {}, "Budget"), h("dd", {}, c.agents.max_cost_usd > 0 ? usd(c.agents.max_cost_usd) + " cap" : "no cap")),
        errors.length ? h("div", { class: "errors", style: "margin-top:14px" }, h("b", {}, "Before this can start"), h("ul", {}, errors.map((e) => h("li", {}, e)))) : null,
        notes.map((n) => h("div", { class: "note", style: "margin-top:10px" }, n)),
        h("button", { class: "btn primary big", type: "button", style: "margin-top:14px", disabled: errors.length > 0, onclick: start }, icon("play"), "Start run")),
      h("div", { class: "card" },
        h("div", { class: "row" }, h("h3", {}, "Changed settings"), h("span", { class: "spacer" }),
          mods.length ? h("button", { class: "btn ghost", type: "button", onclick: () => { d.overrides = {}; drawForm(); changed(); } }, "Reset all") : null),
        mods.length
          ? h("div", { class: "changes", style: "margin-top:8px" }, mods.map(([p, v]) => h("div", {}, h("span", {}, h("span", { class: "faint" }, p + " "), show(v)),
              h("button", { type: "button", title: "Reset " + p, "aria-label": "Reset " + p, onclick: () => { delPath(d.overrides, p); drawForm(); changed(); } }, "×"))))
          : h("p", { class: "faint small", style: "margin-top:6px" }, "None. The run uses " + (S.meta.config_exists ? S.meta.config_path : "the built-in defaults") + " and what the mode implies."),
        h("div", { class: "code-head" }, "Presets"),
        h("div", { class: "row" },
          h("select", { style: "flex:1", "aria-label": "Preset", onchange: (e) => {
            const p = presets[e.target.value];
            if (!p) return;
            d.mode = p.mode; d.overrides = clone(p.overrides);
            drawModes(); drawSubject(); drawForm(); changed();
          } }, h("option", { value: "" }, Object.keys(presets).length ? "Load a preset…" : "No presets saved"), Object.keys(presets).map((n) => h("option", { value: n }, n))),
          h("button", { class: "btn", type: "button", onclick: savePreset }, "Save"))),
      h("div", { class: "card" },
        h("h3", {}, "Same run from a terminal"),
        h("div", { class: "code-head" }, "Command", h("button", { type: "button", onclick: () => copy(cmd) }, "Copy")),
        h("pre", { class: "code" }, cmd),
        yaml ? [h("div", { class: "code-head" }, "Add to boku.yaml", h("button", { type: "button", onclick: () => copy(yaml) }, "Copy")), h("pre", { class: "code" }, yaml),
          h("p", { class: "faint small", style: "margin-top:6px" }, "These settings have no command-line flag.")] : null));
  }

  function savePreset() {
    const name = h("input", { type: "text", placeholder: "e.g. Deep, opus editor, $5 cap" });
    dialog("Save preset", [h("p", {}, "Saves the mode and the changed settings in this browser."), name], [{ label: "Save", fn: () => {
      const n = name.value.trim();
      if (!n) return false;
      const all = store("boku-presets") || {};
      all[n] = { mode: d.mode, overrides: clone(d.overrides) };
      store("boku-presets", all);
      drawAside();
      toast("Preset saved", "ok");
    } }]);
    name.focus();
  }

  async function start() {
    try {
      const { id } = await api("POST", "/api/runs", draftRequest());
      d.topic = ""; save();
      refreshRuns();
      location.hash = "#/runs/" + id;
    } catch (e) { oops(e); }
  }

  fill(main, h("div", { class: "page" },
    h("div", { class: "head" }, h("div", {}, h("h1", {}, "New report"),
      h("p", { class: "sub" }, "Describe what to research, pick a mode, and change any setting. Agents plan, research in parallel, fact-check, and write a cited report."))),
    h("div", { class: "compose" },
      h("div", { class: "stack" }, modes, subject, form),
      aside)));
  drawModes(); drawSubject(); drawForm(); drawAside(); validate();
  document.addEventListener("boku:doctor", drawAside);
  return () => { clearTimeout(timer); document.removeEventListener("boku:doctor", drawAside); };
}

// ---------- runs ----------

function statePill(v) {
  return h("span", { class: "pill " + v.state }, v.state === "running" ? h("span", { class: "dot pulse", style: "background:currentColor" }) : null, v.state);
}
const stageStrip = (v) => h("div", { class: "strip", title: v.stages.map((s) => `${STAGE_LABEL[s.name] || s.name}: ${s.status}`).join("\n") }, v.stages.map((s) => h("i", { class: s.status })));
const modeOf = (v) => (v.config && v.config.report && v.config.report.mode) || "full";
const agentTime = (v) => Object.values(v.tasks || {}).reduce((n, t) => n + goDur(t.duration), 0);

async function refreshRuns() {
  try {
    const r = await api("GET", "/api/runs");
    S.runs = r.runs; S.runsDir = r.runs_dir;
  } catch { /* server stopped; the page keeps its last state */ }
  drawNav();
  document.dispatchEvent(new Event("boku:runs"));
}

function pageRuns(main) {
  let q = "", state = "all";
  const chips = h("div", { class: "chips" });
  const rows = h("div");
  const dir = h("span", { class: "faint small mono" });
  // The filter box is drawn once, so new data never takes the caret.
  const draw = () => {
    const states = ["all", ...new Set(S.runs.map((r) => r.state))];
    if (!states.includes(state)) state = "all";
    dir.textContent = S.runsDir;
    fill(chips, states.map((s) => h("button", { type: "button", class: "chip", "aria-pressed": String(s === state), data: { k: s }, onclick: () => { state = s; draw(); chips.querySelector(`[data-k="${s}"]`).focus(); } },
      s + " " + (s === "all" ? S.runs.length : S.runs.filter((r) => r.state === s).length))));
    const shown = S.runs.filter((r) => (state === "all" || r.state === state) && (!q || (r.topic + " " + r.id).toLowerCase().includes(q)));
    fill(rows, !S.runs.length
      ? h("div", { class: "empty" }, h("b", {}, "No runs yet"), "Start one from ", h("a", { href: "#/new" }, "New report"), ".")
      : !shown.length ? h("div", { class: "empty" }, "No runs match this filter.")
      : h("div", { class: "table-wrap" }, h("table", {},
          h("thead", {}, h("tr", {}, h("th", {}, "Run"), h("th", {}, "Status"), h("th", {}, "Pipeline"), h("th", {}, "Mode"), h("th", { class: "num" }, "Cost"), h("th", { class: "num" }, "Agent time"), h("th", {}, "Started"), h("th", {}))),
          h("tbody", {}, shown.map((r) => h("tr", { class: "click", onclick: (e) => { if (!e.target.closest("a")) location.hash = "#/runs/" + r.id; } },
            h("td", {}, h("a", { class: "t1", href: "#/runs/" + r.id }, r.topic), h("span", { class: "t2" }, r.id)),
            h("td", {}, statePill(r)),
            h("td", {}, stageStrip(r)),
            h("td", {}, h("span", { class: "pill kind" }, modeOf(r))),
            h("td", { class: "num usd" }, usd(r.cost_usd)),
            h("td", { class: "num" }, dur(agentTime(r))),
            h("td", { title: when(r.created_at) }, ago(r.created_at)),
            h("td", {}, r.outputs && r.outputs.pdf ? h("a", { class: "btn ghost", href: `/api/runs/${r.id}/output/pdf`, target: "_blank", rel: "noopener" }, icon("open"), "PDF") : null)))))));
  };
  fill(main, h("div", { class: "page" },
    h("div", { class: "head" }, h("div", {}, h("h1", {}, "Runs"), h("p", { class: "sub" }, "Every run keeps its plan, evidence, drafts and log, so it can be inspected, resumed or re-rendered.")),
      h("a", { class: "btn primary", href: "#/new" }, icon("plus"), "New report")),
    h("div", { class: "toolbar" },
      h("input", { type: "search", placeholder: "Filter by topic", "aria-label": "Filter runs", oninput: (e) => { q = e.target.value.toLowerCase(); draw(); } }),
      chips, h("span", { class: "spacer" }), dir),
    rows));
  draw();
  let sig = "";
  const on = () => {
    const next = JSON.stringify(S.runs.map((r) => [r.id, r.state, r.updated_at]));
    if (next !== sig) { sig = next; draw(); }
  };
  document.addEventListener("boku:runs", on);
  return () => document.removeEventListener("boku:runs", on);
}

// ---------- run detail ----------

const TABS = [["overview", "Overview"], ["report", "Report"], ["log", "Log"], ["plan", "Plan"], ["evidence", "Evidence"], ["factcheck", "Fact-check"], ["files", "Files"], ["config", "Settings"]];
const fileURL = (id, path, download) => `/api/runs/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}${download ? "&download=1" : ""}`;

function parseLog(lines, into) {
  for (const line of lines) {
    let m;
    if ((m = line.match(/^(\S+T\S+) \[(\d+\/\d+)\] (.*)$/))) into.push({ ts: m[1], lv: "STEP", msg: `[${m[2]}] ${m[3]}` });
    else if ((m = line.match(/^(\S+T\S+) (DEBUG|INFO|WARN|ERROR)\s+(.*)$/))) into.push({ ts: m[1], lv: m[2], msg: m[3] });
    else if (into.length) into[into.length - 1].msg += "\n" + line; // continuation of a multi-line message
    else into.push({ ts: "", lv: "INFO", msg: line });
  }
}
const clock = (ts) => (ts ? new Date(ts).toLocaleTimeString([], { hour12: false }) : "");

function pageRun(main, id, tab) {
  const R = { id, v: null, tab: tab || "overview", log: [], files: null, cache: {}, sel: null, levels: new Set(["STEP", "INFO", "WARN", "ERROR"]), q: "", follow: true };
  const head = h("div");
  const tabs = h("div", { class: "tabs", role: "tablist" });
  const body = h("div");
  fill(main, h("div", { class: "page" }, head, tabs, body));

  const json = async (path) => {
    if (!(path in R.cache)) {
      R.cache[path] = fetch(fileURL(id, path)).then((r) => (r.ok ? r.json() : null)).catch(() => null);
    }
    return R.cache[path];
  };
  const loadFiles = async () => { R.files = (await api("GET", `/api/runs/${id}/files`)).files; return R.files; };
  const has = (path) => (R.files || []).some((f) => f.path === path);

  function drawHead() {
    const v = R.v;
    const can = { resume: !v.active && v.state !== "completed", render: !v.active && has("report/document.json"), cancel: v.active, del: !v.active };
    const outs = v.outputs || {};
    fill(head,
      h("div", { class: "head" },
        h("div", { style: "min-width:0;flex:1" },
          h("div", { class: "row", style: "margin-bottom:8px" }, h("a", { href: "#/runs", class: "muted small" }, "← Runs"), statePill(v), h("span", { class: "pill kind" }, modeOf(v)),
            v.active && v.action !== "run" ? h("span", { class: "pill" }, v.action) : null),
          h("h1", {}, v.topic),
          h("p", { class: "sub mono small" }, v.dir)),
        h("div", { class: "row" },
          ["pdf", "html", "md"].filter((f) => outs[f]).map((f) => h("a", { class: "btn" + (f === "pdf" ? " primary" : ""), href: `/api/runs/${id}/output/${f}`, target: "_blank", rel: "noopener" }, icon("open"), f.toUpperCase())),
          can.cancel ? h("button", { class: "btn danger", type: "button", onclick: cancel }, icon("stop"), "Cancel") : null,
          can.resume ? h("button", { class: "btn primary", type: "button", onclick: () => reopen("resume") }, icon("play"), "Resume") : null,
          can.render ? h("button", { class: "btn", type: "button", onclick: () => reopen("render") }, icon("refresh"), "Re-render") : null,
          h("button", { class: "btn", type: "button", onclick: () => reuse(v) }, icon("copy"), "Duplicate"),
          can.del ? h("button", { class: "btn danger", type: "button", "aria-label": "Delete run", title: "Delete run", onclick: del }, icon("trash")) : null)));
    fill(tabs, TABS.map(([t, label]) => h("button", { type: "button", role: "tab", "aria-selected": String(R.tab === t), onclick: () => go(t) }, label)));
  }
  function go(t) {
    R.tab = t;
    history.replaceState(null, "", `#/runs/${id}/${t}`);
    drawHead(); drawTab();
  }

  async function cancel() {
    try { await api("POST", `/api/runs/${id}/cancel`, {}); toast("Cancelling. Agents already running are stopped."); } catch (e) { oops(e); }
  }

  function del() {
    dialog("Delete this run?", [
      h("p", {}, "This permanently removes the run directory with its plan, evidence, drafts and log:"),
      h("pre", { class: "code" }, R.v.dir),
      h("p", {}, "Reports already published to the reports directory are kept."),
    ], [{ label: "Delete run", cls: "danger", fn: async () => {
      try { await api("DELETE", `/api/runs/${id}`); toast("Run deleted", "ok"); refreshRuns(); location.hash = "#/runs"; } catch (e) { oops(e); return false; }
    } }]);
  }

  // reopen asks for the settings `boku resume` and `boku render` accept.
  function reopen(action) {
    const c = R.v.config;
    const o = { formats: [...c.report.formats], output: c.output.directory, max_cost_usd: c.agents.max_cost_usd, max_parallel: c.agents.max_parallel, include_references: c.report.include_references };
    const chips = h("div", { class: "chips" });
    const drawChips = () => fill(chips, ["pdf", "html", "md"].map((f) => h("button", { type: "button", class: "chip", "aria-pressed": String(o.formats.includes(f)),
      onclick: () => { o.formats = ["pdf", "html", "md"].filter((x) => (x === f ? !o.formats.includes(f) : o.formats.includes(x))); drawChips(); } }, f)));
    drawChips();
    const field = (label, control, help) => h("div", { class: "field" }, h("div", { class: "lab" }, label), control, help && h("div", { class: "help" }, help));
    dialog(action === "resume" ? "Resume run" : "Re-render report", [
      h("p", {}, action === "resume"
        ? "Continues from the last finished step with the run's recorded settings. Finished agent work is not repeated."
        : "Rebuilds the report from the saved draft and evidence. No agents run, so it costs nothing."),
      field("Formats", chips),
      field("Reports directory", h("input", { type: "text", class: "mono", value: o.output, oninput: (e) => (o.output = e.target.value.trim()) })),
      field("Reference list in the report", h("label", { class: "switch" }, h("input", { type: "checkbox", checked: o.include_references, onchange: (e) => (o.include_references = e.target.checked) }), h("i"))),
      action === "resume" ? [
        field("Budget cap (USD)", h("input", { type: "number", class: "mono", min: 0, step: "any", value: o.max_cost_usd, oninput: (e) => (o.max_cost_usd = parseFloat(e.target.value) || 0) }),
          `Total for the run, including the ${usd(R.v.cost_usd)} already spent. 0 = unlimited.`),
        field("Agents at once", h("input", { type: "number", class: "mono", min: 1, step: 1, value: o.max_parallel, oninput: (e) => (o.max_parallel = parseInt(e.target.value, 10) || 1) })),
      ] : null,
    ], [{ label: action === "resume" ? "Resume" : "Re-render", fn: async () => {
      try { await api("POST", `/api/runs/${id}/${action}`, o); R.cache = {}; refreshRuns(); } catch (e) { oops(e); return false; }
    } }]);
  }

  // ----- tabs -----

  function tabOverview() {
    const v = R.v;
    const tasks = Object.values(v.tasks || {}).sort((a, b) => new Date(a.finished) - new Date(b.finished));
    const byStage = {};
    for (const t of tasks) byStage[t.stage] = (byStage[t.stage] || 0) + t.cost_usd;
    const spent = Object.entries(byStage).filter(([, c]) => c > 0);
    const mark = (s) => (s.status === "running" ? h("span", { class: "spin" }) : icon(s.status === "done" ? "check" : s.status === "failed" ? "x" : s.status === "blocked" ? "bang" : "chev"));
    const stageDur = (s) => (s.started_at ? dur(new Date(s.finished_at || Date.now()) - new Date(s.started_at)) : "");
    const gates = h("div", {}, h("p", { class: "faint small" }, "No gate has run yet."));
    json("report/gates.json").then((g) => {
      if (!g || !g.length || R.tab !== "overview") return;
      fill(gates, g.map((x) => h("div", { class: "gate" },
        h("div", { class: "row" }, h("b", {}, x.name), h("span", { class: "pill " + (x.passed ? "pass" : "fail") }, x.passed ? "passed" : "failed"),
          (x.warnings || []).length ? h("span", { class: "pill warn" }, x.warnings.length + " warning" + (x.warnings.length > 1 ? "s" : "")) : null),
        (x.errors || []).length ? h("ul", { class: "e" }, x.errors.map((e) => h("li", {}, e))) : null,
        (x.warnings || []).length ? h("details", {}, h("summary", { class: "small muted", style: "cursor:pointer" }, "Show warnings"), h("ul", {}, x.warnings.map((e) => h("li", {}, e)))) : null)));
    });
    return h("div", { class: "stack" },
      v.error && v.state !== "running" ? h("div", { class: "errors" }, h("b", {}, v.state === "blocked" ? "A quality gate refused publication" : v.state === "interrupted" ? "Interrupted" : "Run failed"),
        h("pre", { style: "margin:6px 0 0;white-space:pre-wrap;font:12.5px var(--mono)" }, v.error)) : null,
      v.state === "interrupted" && !v.error ? h("div", { class: "note" }, "This run is marked running but is not running here. It was stopped, or the CLI is running it in another terminal. Resume continues from the last finished step.") : null,
      h("div", { class: "stats" },
        h("div", { class: "stat cost" }, h("div", { class: "k" }, "Cost"), h("div", { class: "v" }, usd(v.cost_usd))),
        h("div", { class: "stat" }, h("div", { class: "k" }, "Agent time"), h("div", { class: "v" }, dur(agentTime(v)))),
        h("div", { class: "stat" }, h("div", { class: "k" }, "Agent calls"), h("div", { class: "v" }, tasks.length)),
        h("div", { class: "stat" }, h("div", { class: "k" }, "Runtime"), h("div", { class: "v sm" }, v.provider + (v.model ? " · " + v.model : ""))),
        h("div", { class: "stat" }, h("div", { class: "k" }, "Started"), h("div", { class: "v sm" }, when(v.created_at)))),
      h("div", { class: "grid2" },
        h("div", { class: "stack" },
          h("div", { class: "card" }, h("h3", {}, "Pipeline"),
            h("ol", { class: "timeline", style: "margin-top:8px" }, v.stages.map((s) => h("li", { class: s.status },
              h("span", { class: "mark" }, mark(s)),
              h("div", { style: "min-width:0" }, h("div", { class: "name" }, STAGE_LABEL[s.name] || s.name),
                s.detail ? h("div", { class: "detail" }, s.detail) : null, s.error ? h("div", { class: "err" }, s.error) : null),
              h("span", { class: "dur" }, stageDur(s)))))),
          v.active ? h("div", { class: "card" }, h("div", { class: "row" }, h("h3", {}, "Live activity"), h("span", { class: "spacer" }),
            h("button", { class: "btn ghost", type: "button", onclick: () => go("log") }, "Full log")), h("div", { class: "feed", id: "feed", style: "margin-top:8px" })) : null),
        h("div", { class: "stack" },
          h("div", { class: "card" }, h("h3", {}, "Cost by stage"),
            spent.length ? [h("div", { class: "bar", style: "margin-top:12px", role: "img", "aria-label": spent.map(([s, c]) => `${STAGE_LABEL[s] || s} ${usd(c)}`).join(", ") },
              spent.map(([s, c]) => h("span", { style: `flex:${c};background:${STAGE_COLOR[s] || "var(--faint)"}`, title: `${STAGE_LABEL[s] || s} ${usd(c)}` }))),
              h("div", { class: "legend" }, spent.map(([s, c]) => h("span", {}, h("i", { style: `background:${STAGE_COLOR[s] || "var(--faint)"}` }), `${STAGE_LABEL[s] || s} ${usd(c)}`)))]
              : h("p", { class: "faint small", style: "margin-top:6px" }, "Nothing spent yet.")),
          h("div", { class: "card" }, h("h3", {}, "Quality gates"), gates),
          (v.warnings || []).length ? h("div", { class: "card" }, h("h3", {}, "Warnings"), h("ul", { class: "plain" }, v.warnings.map((w) => h("li", {}, w)))) : null)),
      tasks.length ? h("div", {}, h("h3", { style: "font-size:15px;margin:6px 0 10px" }, "Agent calls"),
        h("div", { class: "table-wrap" }, h("table", {},
          h("thead", {}, h("tr", {}, h("th", {}, "Task"), h("th", {}, "Role"), h("th", {}, "Stage"), h("th", {}, "Status"), h("th", { class: "num" }, "Attempts"), h("th", { class: "num" }, "Cost"), h("th", { class: "num" }, "Duration"), h("th", {}, "Artifact"))),
          h("tbody", {}, tasks.map((t) => h("tr", {},
            h("td", { class: "mono" }, t.id), h("td", {}, t.role), h("td", {}, STAGE_LABEL[t.stage] || t.stage),
            h("td", {}, h("span", { class: "pill " + (t.status === "ok" ? "ok" : "failed"), title: t.error || "" }, t.status)),
            h("td", { class: "num" }, t.attempts), h("td", { class: "num usd" }, usd(t.cost_usd)), h("td", { class: "num" }, t.duration),
            h("td", {}, t.artifact ? h("button", { class: "btn ghost mono small", type: "button", onclick: () => { R.sel = t.artifact; go("files"); } }, t.artifact) : "")))))) ) : null);
  }
  function drawFeed() {
    const feed = $("#feed", body);
    if (feed) fill(feed, R.log.filter((l) => l.lv !== "DEBUG").slice(-7).map((l) => h("div", {}, h("span", { class: "faint" }, clock(l.ts) + " "), l.msg.split("\n")[0])));
  }

  function tabReport() {
    const outs = R.v.outputs || {};
    const choices = [];
    if (outs.pdf) choices.push(["PDF", `/api/runs/${id}/output/pdf`, "frame"]);
    if (outs.html || has("report/report.html")) choices.push(["HTML", outs.html ? `/api/runs/${id}/output/html` : fileURL(id, "report/report.html"), "frame"]);
    if (outs.md || has("report/report.md")) choices.push(["Markdown", outs.md ? `/api/runs/${id}/output/md` : fileURL(id, "report/report.md"), "text"]);
    if (outs.refs) choices.push(["References", `/api/runs/${id}/output/refs`, "text"]);
    if (!choices.length) return h("div", { class: "empty" }, h("b", {}, "No report yet"), R.v.active ? "It appears here once the editor has written it." : "This run stopped before a report was written.");
    const view = h("div");
    let cur = 0;
    const seg = h("div", { class: "seg" });
    const draw = () => {
      fill(seg, choices.map(([label], i) => h("button", { type: "button", "aria-pressed": String(i === cur), onclick: () => { cur = i; draw(); } }, label)));
      const [label, url, kind] = choices[cur];
      if (kind === "frame") fill(view, h("iframe", { class: "preview", src: url, title: label + " report", sandbox: label === "HTML" ? "" : null }));
      else { fill(view, h("pre", { class: "view" }, "Loading…")); fetch(url).then((r) => r.text()).then((t) => fill(view, h("pre", { class: "view" }, t))); }
      fill(link, h("a", { class: "btn", href: url, target: "_blank", rel: "noopener" }, icon("open"), "Open in new tab"));
    };
    const link = h("span");
    const out = h("div", { class: "stack" },
      !Object.keys(outs).length ? h("div", { class: "note info" }, "This is the working draft inside the run directory; it has not been published.") : null,
      h("div", { class: "row" }, seg, h("span", { class: "spacer" }), link), view);
    draw();
    return out;
  }

  function tabLog() {
    const box = h("div", { class: "log", tabindex: "0", "aria-label": "Run log" });
    const line = (l) => h("div", { class: "l " + l.lv }, h("span", { class: "ts" }, clock(l.ts)), h("span", { class: "lv" }, l.lv === "STEP" ? "" : l.lv), h("span", { class: "m" }, l.msg));
    const pass = (l) => R.levels.has(l.lv) && (!R.q || l.msg.toLowerCase().includes(R.q));
    const draw = () => { fill(box, R.log.filter(pass).slice(-5000).map(line)); if (R.follow) box.scrollTop = box.scrollHeight; };
    R.onLog = (fresh) => { add(box, fresh.filter(pass).map(line)); if (R.follow) box.scrollTop = box.scrollHeight; };
    const levels = h("div", { class: "chips" });
    const drawLevels = () => fill(levels, ["INFO", "WARN", "ERROR", "DEBUG"].map((lv) => h("button", { type: "button", class: "chip", "aria-pressed": String(R.levels.has(lv)),
      onclick: () => { R.levels.has(lv) ? R.levels.delete(lv) : R.levels.add(lv); drawLevels(); draw(); } }, lv + " " + R.log.filter((l) => l.lv === lv).length)));
    drawLevels();
    const out = h("div", {},
      h("div", { class: "toolbar" },
        h("input", { type: "search", placeholder: "Filter log", value: R.q, "aria-label": "Filter log", oninput: (e) => { R.q = e.target.value.toLowerCase(); draw(); } }),
        levels, h("span", { class: "spacer" }),
        h("label", { class: "switch small" }, h("input", { type: "checkbox", checked: R.follow, onchange: (e) => { R.follow = e.target.checked; draw(); } }), h("i"), "Follow"),
        h("a", { class: "btn", href: fileURL(id, "logs/boku.log", true) }, icon("down"), "Download")),
      box);
    draw();
    queueMicrotask(() => { if (R.follow) box.scrollTop = box.scrollHeight; });
    return out;
  }

  const later = (promise, render, empty) => {
    const slot = h("div", {}, h("p", { class: "muted" }, "Loading…"));
    promise.then((data) => fill(slot, data ? render(data) : h("div", { class: "empty" }, h("b", {}, empty), R.v.active ? "It appears here as the run progresses." : "This run did not get that far.")));
    return slot;
  };
  const list = (title, items) => (items && items.length ? h("div", { class: "card" }, h("h3", {}, title), h("ul", { class: "plain" }, items.map((x) => h("li", {}, typeof x === "string" ? x : JSON.stringify(x))))) : null);

  function tabPlan() {
    return later(json("plan.json"), (p) => h("div", { class: "stack" },
      h("div", { class: "card" }, h("h3", { style: "font-size:18px" }, p.title), p.subtitle ? h("p", { class: "muted", style: "margin-top:4px" }, p.subtitle) : null,
        h("dl", { class: "kv", style: "margin-top:14px;grid-template-columns:140px 1fr" },
          [["Objective", p.objective], ["Audience", p.audience], ["Report type", p.report_type], ["Time sensitivity", p.time_sensitivity], ["Shape", p.report_shape]].filter(([, x]) => x)
            .map(([k, x]) => [h("dt", {}, k), h("dd", { style: "text-align:left;font-family:var(--sans);font-size:13.5px" }, x)]))),
      h("h3", { style: "font-size:15px" }, `Workstreams (${(p.workstreams || []).length})`),
      h("div", { class: "ws" }, (p.workstreams || []).map((w) => h("div", { class: "card" },
        h("div", { class: "row" }, h("b", { class: "mono" }, w.id), h("span", { class: "pill kind" }, w.role)),
        h("p", { class: "muted", style: "margin-top:8px" }, w.objective),
        (w.questions || []).length ? h("ul", { class: "plain small" }, w.questions.map((q) => h("li", {}, q))) : null))),
      list("Research questions", p.research_questions), list("Outline", p.report_outline), list("Required sources", p.required_sources), list("Deliverables", p.deliverables)), "No plan yet");
  }

  function tabEvidence() {
    return later(Promise.all([json("evidence/sources.json"), json("evidence/findings.json")]).then(([s, f]) => ((s && s.length) || (f && f.length) ? { s: s || [], f: f || [] } : null)), ({ s, f }) => {
      let which = "findings", q = "";
      const out = h("div");
      const href = (u) => (/^https?:\/\//i.test(u || "") ? u : null);
      const draw = () => {
        const match = (o) => !q || JSON.stringify(o).toLowerCase().includes(q);
        const rows = (which === "findings" ? f : s).filter(match);
        fill($("#ev", out), !rows.length ? h("div", { class: "empty" }, "Nothing matches.") : which === "findings"
          ? h("div", { class: "card" }, rows.slice(0, 400).map((x) => h("div", { class: "item" },
              h("div", {}, h("b", { class: "mono faint" }, x.id + " "), x.claim),
              h("div", { class: "meta" },
                x.review && x.review.status ? h("span", { class: "pill " + (x.review.status === "accepted" ? "ok" : x.review.status === "rejected" ? "failed" : "warn") }, x.review.status) : null,
                x.confidence != null ? h("span", {}, "confidence " + Math.round(x.confidence * 100) + "%") : null,
                x.kind ? h("span", {}, x.kind) : null, x.workstream ? h("span", {}, x.workstream) : null, x.as_of ? h("span", {}, "as of " + x.as_of) : null,
                (x.source_ids || []).length ? h("span", {}, x.source_ids.length + " source" + (x.source_ids.length > 1 ? "s" : "") + ": " + x.source_ids.slice(0, 8).join(" ") + (x.source_ids.length > 8 ? " …" : "")) : null),
              x.evidence || x.notes ? h("details", {}, h("summary", {}, "Evidence"), h("p", { style: "margin-top:6px" }, x.evidence), x.notes ? h("p", { class: "faint", style: "margin-top:6px" }, "Note: " + x.notes) : null) : null)))
          : h("div", { class: "table-wrap" }, h("table", {},
              h("thead", {}, h("tr", {}, h("th", {}, "ID"), h("th", {}, "Source"), h("th", {}, "Type"), h("th", { class: "num" }, "Tier"), h("th", {}, "Published"), h("th", {}, "Found by"))),
              h("tbody", {}, rows.slice(0, 600).map((x) => h("tr", {},
                h("td", { class: "mono" }, x.id),
                h("td", {}, href(x.url) ? h("a", { class: "t1", href: href(x.url), target: "_blank", rel: "noopener noreferrer" }, x.title || x.url) : h("span", { class: "t1" }, x.title || x.url),
                  h("span", { class: "t2" }, [x.publisher, x.url].filter(Boolean).join(" · "))),
                h("td", {}, x.source_type || ""), h("td", { class: "num" }, x.tier != null ? x.tier : ""),
                h("td", { class: "mono small" }, (x.published_at || "").slice(0, 10)), h("td", { class: "mono small" }, (x.found_by || []).join(", "))))))));
      };
      const seg = h("div", { class: "seg" });
      const drawSeg = () => fill(seg, [["findings", `Findings ${f.length}`], ["sources", `Sources ${s.length}`]].map(([k, label]) => h("button", { type: "button", "aria-pressed": String(which === k), onclick: () => { which = k; drawSeg(); draw(); } }, label)));
      add(out, [h("div", { class: "toolbar" }, seg, h("input", { type: "search", placeholder: "Search evidence", "aria-label": "Search evidence", oninput: (e) => { q = e.target.value.toLowerCase(); draw(); } })), h("div", { id: "ev" })]);
      drawSeg(); draw();
      return out;
    }, "No evidence yet");
  }

  function tabFactcheck() {
    const rounds = (async () => {
      const out = [];
      for (let n = 1; n < 20; n++) { const r = await json(`factcheck/round-${n}.json`); if (!r) break; out.push(r); }
      return out.length ? out : null;
    })();
    return later(rounds, (rs) => h("div", { class: "stack" }, rs.map((r, i) => h("div", { class: "card" },
      h("div", { class: "row" }, h("h3", {}, "Round " + (i + 1)), h("span", { class: "pill " + (r.status === "approved" || r.status === "pass" ? "ok" : "warn") }, r.status || "")),
      r.summary ? h("p", { class: "muted", style: "margin-top:8px" }, r.summary) : null,
      (r.issues || []).map((x) => h("div", { class: "item" },
        h("div", { class: "row" }, h("span", { class: "pill " + (x.severity === "critical" ? "failed" : x.severity === "major" ? "warn" : "") }, x.severity || "issue"), x.type ? h("span", { class: "faint small mono" }, x.type) : null,
          (x.finding_ids || []).length ? h("span", { class: "faint small mono" }, x.finding_ids.join(" ")) : null),
        h("p", { style: "margin-top:6px" }, x.description), x.recommendation ? h("p", { class: "muted small", style: "margin-top:4px" }, "Recommendation: " + x.recommendation) : null)),
      (r.verdicts || []).length ? h("p", { class: "faint small", style: "margin-top:8px" }, r.verdicts.length + " verdicts recorded; see factcheck/round-" + (i + 1) + ".json under Files.") : null)),
      rs.map((r, i) => [list(`Missing sources (round ${i + 1})`, r.missing_sources), list(`Claims to verify (round ${i + 1})`, r.claims_to_verify)])),
      R.v.config.research.fact_check ? "No fact-check yet" : "Fact-checking is off for this run");
  }

  function tabFiles() {
    const view = h("div", { class: "card", style: "min-height:200px" }, h("p", { class: "muted" }, "Select a file."));
    const treeEl = h("div", { class: "card tree" }, "Loading…");
    const showFile = async (f) => {
      R.sel = f.path;
      for (const b of treeEl.querySelectorAll("button")) b.classList.toggle("on", b.dataset.path === f.path);
      const ext = (f.path.match(/\.([a-z0-9]+)$/i) || [, ""])[1].toLowerCase();
      const url = fileURL(id, f.path);
      const bar = h("div", { class: "row", style: "margin-bottom:12px" }, h("b", { class: "mono", style: "overflow-wrap:anywhere" }, f.path), h("span", { class: "faint small mono" }, bytes(f.size) + " · " + when(f.modified)), h("span", { class: "spacer" }),
        h("button", { class: "btn ghost", type: "button", onclick: () => copy(R.v.dir + "/" + f.path) }, icon("copy"), "Path"),
        h("a", { class: "btn", href: fileURL(id, f.path, true) }, icon("down"), "Download"));
      if (ext === "pdf" || ext === "html") return fill(view, bar, h("iframe", { class: "preview", src: url, title: f.path, sandbox: ext === "html" ? "" : null }));
      if (ext === "png" || ext === "jpg" || ext === "svg") return fill(view, bar, h("img", { src: url, alt: f.path, style: "max-width:100%" }));
      if (!["json", "md", "log", "txt", "yaml"].includes(ext)) return fill(view, bar, h("p", { class: "muted" }, "No preview for this file type."));
      fill(view, bar, h("pre", { class: "view" }, "Loading…"));
      let text = await fetch(url).then((r) => r.text()).catch(() => "Could not load the file.");
      if (ext === "json" && text.length < 2e6) { try { text = JSON.stringify(JSON.parse(text), null, 2); } catch { /* show as is */ } }
      const cut = text.length > 400000;
      if (R.sel === f.path) fill(view, bar, cut ? h("div", { class: "note", style: "margin-bottom:10px" }, "Showing the first 400,000 characters. Download for the whole file.") : null, h("pre", { class: "view" }, cut ? text.slice(0, 400000) : text));
    };
    loadFiles().then((files) => {
      const root = {};
      for (const f of files) {
        const parts = f.path.split("/");
        let node = root;
        for (const p of parts.slice(0, -1)) node = node[p + "/"] || (node[p + "/"] = {});
        node[parts[parts.length - 1]] = f;
      }
      const drawNode = (node, depth) => Object.keys(node).sort((a, b) => b.endsWith("/") - a.endsWith("/") || a.localeCompare(b)).map((k) => (k.endsWith("/")
        ? h("details", { open: depth === 0 && k !== "agents/" }, h("summary", {}, k), h("div", { class: "kids" }, drawNode(node[k], depth + 1)))
        : h("button", { type: "button", data: { path: node[k].path }, onclick: () => showFile(node[k]) }, h("span", {}, k), h("small", {}, bytes(node[k].size)))));
      fill(treeEl, files.length ? drawNode(root, 0) : "No files.");
      const sel = files.find((f) => f.path === R.sel);
      if (sel) showFile(sel);
    }).catch(oops);
    return h("div", { class: "files" }, treeEl, view);
  }

  function tabConfig() {
    const v = R.v;
    return h("div", { class: "stack" },
      h("div", { class: "row" }, h("p", { class: "muted" }, "The settings this run was created with. A resume uses them again."), h("span", { class: "spacer" }),
        h("button", { class: "btn", type: "button", onclick: () => reuse(v) }, icon("copy"), "Use for a new report"),
        h("button", { class: "btn", type: "button", onclick: () => copy("boku resume " + shq(v.dir)) }, icon("copy"), "Copy resume command")),
      h("pre", { class: "view" }, toYAML(v.config)),
      h("div", { class: "card" }, h("h3", {}, "Provenance"),
        h("dl", { class: "kv", style: "margin-top:10px" }, h("dt", {}, "Run ID"), h("dd", {}, v.id), h("dt", {}, "Boku version"), h("dd", {}, v.boku_version || "–"),
          h("dt", {}, "Created"), h("dd", {}, when(v.created_at)), h("dt", {}, "Updated"), h("dd", {}, when(v.updated_at)),
          Object.entries(v.prompt_versions || {}).map(([role, ver]) => [h("dt", {}, "prompt · " + role), h("dd", {}, ver)]))));
  }

  function drawTab() {
    R.onLog = null;
    const fn = { overview: tabOverview, report: tabReport, log: tabLog, plan: tabPlan, evidence: tabEvidence, factcheck: tabFactcheck, files: tabFiles, config: tabConfig }[R.tab] || tabOverview;
    fill(body, fn());
    if (R.tab === "overview") drawFeed();
  }

  // ----- live updates -----
  let es, first = true;
  const onRun = async (v) => {
    const was = R.v;
    R.v = v;
    const finished = was && was.active && !v.active;
    if (first || finished || (was && was.updated_at !== v.updated_at && R.tab !== "files")) {
      // Artifacts appear as stages finish; forget what was missing before.
      R.cache = {};
      await loadFiles().catch(() => {});
    }
    drawHead();
    if (first || R.tab === "overview" || (finished && R.tab === "report")) drawTab();
    if (finished) { toast(v.state === "completed" ? "Report ready" : "Run " + v.state, v.state === "completed" ? "ok" : "err"); refreshRuns(); }
    first = false;
  };
  api("GET", `/api/runs/${id}`).then((v) => {
    onRun(v);
    es = new EventSource(`/api/runs/${id}/events`);
    let opened = false;
    // A reconnect replays the log from the start.
    es.onopen = () => { if (opened) { R.log = []; if (R.tab === "log") drawTab(); } opened = true; };
    es.addEventListener("run", (e) => onRun(JSON.parse(e.data)));
    es.addEventListener("log", (e) => {
      const n = R.log.length;
      parseLog(JSON.parse(e.data), R.log);
      if (R.onLog) R.onLog(R.log.slice(n));
      drawFeed();
    });
  }).catch((e) => fill(main, h("div", { class: "page" }, h("div", { class: "empty" }, h("b", {}, "Run not found"), e.message, h("p", { style: "margin-top:10px" }, h("a", { href: "#/runs" }, "Back to runs"))))));
  const tick = setInterval(() => { if (R.v && R.v.active && R.tab === "overview") { const y = scrollY; drawTab(); scrollTo(0, y); } }, 5000);
  return () => { if (es) es.close(); clearInterval(tick); };
}

// reuse opens the composer with a run's topic and settings.
function reuse(v) {
  const mode = modeOf(v);
  const base = S.baselines[mode] || S.meta.base;
  const overrides = {};
  for (const [p, val] of leaves(v.config)) {
    if (["report.mode", "output.runs_directory", "research.codebase"].includes(p)) continue;
    if (!eq(val, getPath(base, p))) setPath(overrides, p, val);
  }
  const codebase = (v.config.research && v.config.research.codebase) || "";
  const focus = codebase ? (v.topic.split(", focusing on: ")[1] || "") : "";
  S.draft = { topic: codebase ? "" : v.topic, mode, subject: codebase ? "codebase" : "topic", codebase, focus, overrides };
  store("boku-draft", S.draft);
  location.hash = "#/new";
}

// ---------- reports ----------

function pageReports(main) {
  const body = h("div", {}, h("p", { class: "muted" }, "Loading…"));
  fill(main, h("div", { class: "page" },
    h("div", { class: "head" }, h("div", {}, h("h1", {}, "Reports"), h("p", { class: "sub" }, "Published files in the reports directory."))), body));
  api("GET", "/api/reports").then(({ files, directory }) => {
    const groups = new Map();
    for (const f of files) {
      const stem = f.path.replace(/\.references\.json$/, "").replace(/\.[^.]+$/, "");
      if (!groups.has(stem)) groups.set(stem, []);
      groups.get(stem).push(f);
    }
    const kind = (p) => (p.endsWith(".references.json") ? "References" : p.split(".").pop().toUpperCase());
    const url = (p, dl) => "/api/reports/file?name=" + encodeURIComponent(p) + (dl ? "&download=1" : "");
    fill(body,
      h("p", { class: "faint small mono", style: "margin-bottom:14px" }, directory),
      !groups.size ? h("div", { class: "empty" }, h("b", {}, "No reports yet"), "Finished runs publish here.")
        : h("div", { class: "reports" }, [...groups].map(([stem, fs]) => {
            const runId = (fs.find((f) => f.run) || {}).run;
            const run = S.runs.find((r) => r.id === runId);
            const main2 = fs.find((f) => f.path.endsWith(".pdf")) || fs.find((f) => f.path.endsWith(".html")) || fs[0];
            return h("div", { class: "card report" },
              h("h3", {}, run ? run.topic : stem.replace(/-/g, " ")),
              h("div", { class: "meta" }, ago(main2.modified) + " · " + bytes(main2.size) + (run ? " · " + usd(run.cost_usd) : "")),
              h("div", { class: "row" },
                fs.sort((a, b) => kind(a.path).localeCompare(kind(b.path))).map((f) => h("a", { class: "btn" + (f === main2 ? " primary" : ""), href: url(f.path), target: "_blank", rel: "noopener", title: f.path }, kind(f.path))),
                h("span", { class: "spacer" }),
                runId ? h("a", { class: "btn ghost", href: "#/runs/" + runId }, "Run") : null));
          })));
  }).catch(oops);
}

// ---------- settings ----------

function pageSettings(main) {
  const W = clone(S.meta.base);
  const form = h("div", { class: "stack" });
  const side = h("div", { class: "aside" });
  const sys = h("div", { class: "card" });
  const ctx = {
    settings: true,
    cfg: () => W,
    base: (p) => getPath(S.meta.defaults, p),
    set: (p, v) => { setPath(W, p, v); drawSide(); },
    reset: (p) => { setPath(W, p, clone(getPath(S.meta.defaults, p) ?? null)); drawSide(); },
  };
  const dirty = () => !eq(W, S.meta.base);

  function drawSys() {
    const d = S.doctor;
    fill(sys, h("div", { class: "row" }, h("h3", {}, "System check"), h("span", { class: "spacer" }),
      h("button", { class: "btn ghost", type: "button", onclick: () => { S.doctor = null; drawSys(); loadDoctor(); } }, icon("refresh"), "Re-check")),
      !d ? h("p", { class: "muted", style: "margin-top:8px" }, "Checking…") : d.map((c) => h("div", { class: "gate" },
        h("div", { class: "row" }, h("span", { class: "dot " + (c.ok ? "ok" : c.optional ? "warn" : "bad") }), h("b", {}, c.name), h("span", { class: "faint small" }, c.ok ? "" : c.optional ? "optional" : "required")),
        h("div", { class: "faint small mono", style: "overflow-wrap:anywhere" }, c.detail))),
      h("p", { class: "faint small", style: "margin-top:8px" }, "Checked against the saved configuration, the same as `boku doctor`."));
  }

  function drawSide() {
    const diff = leaves(W).filter(([p, v]) => !eq(v, getPath(S.meta.base, p)));
    fill(side, sys,
      h("div", { class: "card" }, h("h3", {}, "Configuration file"),
        h("dl", { class: "kv", style: "margin-top:10px" },
          h("dt", {}, "File"), h("dd", {}, S.meta.config_path + (S.meta.config_exists ? "" : " (not created)")),
          h("dt", {}, "Working directory"), h("dd", {}, S.meta.cwd),
          h("dt", {}, "Version"), h("dd", {}, S.meta.version)),
        S.meta.config_error ? h("div", { class: "errors", style: "margin-top:12px" }, h("b", {}, "The file has problems"), h("ul", {}, S.meta.config_error.split("\n").map((e) => h("li", {}, e)))) : null,
        diff.length ? [h("div", { class: "code-head" }, "Unsaved changes"), h("div", { class: "changes" }, diff.map(([p, v]) => h("div", {}, h("span", {}, h("span", { class: "faint" }, p + " "), show(v)))))] : null,
        h("button", { class: "btn primary big", type: "button", style: "margin-top:14px", disabled: !dirty(), onclick: save }, "Save to " + S.meta.config_path),
        h("p", { class: "faint small", style: "margin-top:8px" }, "These become the defaults for new runs here and for the CLI in this directory. Runs already created keep their own settings.")),
      h("div", { class: "card" }, h("div", { class: "code-head", style: "margin-top:0" }, "Preview", h("button", { type: "button", onclick: () => copy(toYAML(W)) }, "Copy")),
        h("pre", { class: "code", style: "max-height:340px;overflow:auto" }, toYAML(W))));
  }

  function save() {
    const write = async () => {
      try {
        await api("PUT", "/api/config", { config: W });
        await loadMeta();
        loadDoctor();
        toast("Saved " + S.meta.config_path, "ok");
        route();
      } catch (e) { oops(e); return false; }
    };
    if (!S.meta.config_exists) return write();
    dialog("Overwrite " + S.meta.config_path + "?", [h("p", {}, "The file is rewritten from these settings. Comments and formatting in the existing file are lost.")], [{ label: "Overwrite", fn: write }]);
  }

  fill(main, h("div", { class: "page" },
    h("div", { class: "head" }, h("div", {}, h("h1", {}, "Settings"), h("p", { class: "sub" }, "Defaults for every new run, stored in boku.yaml. Each run can still change them."))),
    h("div", { class: "compose" }, form, side)));
  const openBefore = new Set(S.open);
  S.open.add("general");
  renderForm(form, ctx);
  drawSys(); drawSide();
  document.addEventListener("boku:doctor", drawSys);
  return () => { S.open = openBefore; document.removeEventListener("boku:doctor", drawSys); };
}

// ---------- shell ----------

let navSig = "";
function drawNav() {
  const page = location.hash.split("/")[1] || "new";
  const active = S.runs.filter((r) => r.active).length;
  const sig = JSON.stringify([page, active, S.doctor]);
  if (sig === navSig) return;
  navSig = sig;
  fill($("#nav"), [["new", "plus", "New report"], ["runs", "runs", "Runs"], ["reports", "doc", "Reports"], ["settings", "gear", "Settings"]].map(([id, ic, label]) =>
    h("a", { href: "#/" + id, class: page === id ? "on" : "", "aria-current": page === id ? "page" : null, title: label }, icon(ic), h("span", { class: "label" }, label),
      id === "runs" && active ? h("span", { class: "count", title: active + " running" }, active) : null)));
  const d = S.doctor;
  const bad = d ? d.filter((c) => !c.ok && !c.optional).length : 0;
  fill($("#sys"), h("span", { class: "dot " + (!d ? "" : bad ? "bad" : "ok") }), h("span", { class: "label" }, !d ? "Checking system…" : bad ? bad + " system problem" + (bad > 1 ? "s" : "") : "System ready"));
}

let leave = null;
function route() {
  if (leave) leave();
  leave = null;
  const [, page = "new", arg, tab] = location.hash.split("/");
  const main = $("#main");
  drawNav();
  if (page === "runs" && arg) leave = pageRun(main, decodeURIComponent(arg), tab);
  else if (page === "runs") leave = pageRuns(main);
  else if (page === "reports") leave = pageReports(main);
  else if (page === "settings") leave = pageSettings(main);
  else leave = pageNew(main);
  scrollTo(0, 0);
}

async function loadMeta() {
  S.meta = await api("GET", "/api/meta");
  const blankReq = (mode) => ({ topic: "", mode, codebase: "", focus: "", overrides: {} });
  const res = await Promise.all(MODES.map((m) => api("POST", "/api/resolve", blankReq(m.id))));
  MODES.forEach((m, i) => (S.baselines[m.id] = res[i].config));
  $("#version").textContent = S.meta.version;
}
async function loadDoctor() {
  try { S.doctor = await api("GET", "/api/doctor"); } catch { S.doctor = []; }
  drawNav();
  document.dispatchEvent(new Event("boku:doctor"));
}

async function boot() {
  $("#theme").append(icon("sun"));
  $("#theme").addEventListener("click", () => {
    const dark = getComputedStyle(document.documentElement).colorScheme.includes("dark");
    document.documentElement.dataset.theme = dark ? "light" : "dark";
    try { localStorage.setItem("boku-theme", dark ? "light" : "dark"); } catch { /* not persisted */ }
  });
  try { await loadMeta(); } catch (e) {
    fill($("#main"), h("div", { class: "page" }, h("div", { class: "empty" }, h("b", {}, "Could not reach Boku"), e.message)));
    return;
  }
  const saved = store("boku-draft");
  if (saved && MODES.some((m) => m.id === saved.mode)) S.draft = { ...S.draft, ...saved, overrides: saved.overrides || {} };
  else S.draft.mode = S.meta.base.report.mode;
  await refreshRuns();
  window.addEventListener("hashchange", route);
  route();
  loadDoctor();
  setInterval(refreshRuns, 5000);
}
boot();
