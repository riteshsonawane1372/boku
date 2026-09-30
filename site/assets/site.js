// Boku site: theme toggle, copy buttons, terminal replay, cost bars.
(function () {
  "use strict";
  var root = document.documentElement;
  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Theme: follows the system unless the visitor picks one.
  try {
    var saved = localStorage.getItem("boku-theme");
    if (saved) root.setAttribute("data-theme", saved);
  } catch (e) {}
  document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var current = root.getAttribute("data-theme") ||
        (window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
      var next = current === "light" ? "dark" : "light";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("boku-theme", next); } catch (e) {}
    });
  });

  document.querySelectorAll("[data-copy]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var el = document.querySelector(btn.getAttribute("data-copy"));
      if (!el || !navigator.clipboard) return;
      navigator.clipboard.writeText(el.textContent.trim()).then(function () {
        var t = btn.textContent;
        btn.textContent = "Copied";
        setTimeout(function () { btn.textContent = t; }, 1400);
      });
    });
  });

  // Terminal: the log of the real run shown in the README, typed out.
  var term = document.getElementById("term");
  if (term) {
    var lines = [
      ["step", "[1/7] Planning research"],
      ["info", "INFO  planner completed  workstreams=4"],
      ["dim", "INFO    workstream  id=upstream-tech-stack  role=technical"],
      ["dim", "INFO    workstream  id=adoption-standards  role=market"],
      ["dim", "INFO    workstream  id=vendor-platforms  role=competitive"],
      ["dim", "INFO    workstream  id=production-case-studies  role=case-study"],
      ["step", "[2/7] Researching"],
      ["info", "INFO  spawning 4 research agents  max_parallel=4"],
      ["dim", "INFO  vendor-platforms research completed  took=2m17s"],
      ["step", "[3/7] Cross-checking evidence"],
      ["warn", "INFO  fact checker round 1: needs_revision  issues=8"],
      ["warn", "INFO  fact checker requested 2 follow-up research task(s)"],
      ["info", "INFO  fact checker round 2: pass  verdicts=19"],
      ["info", "INFO  research gate passed · fact-check gate passed"],
      ["step", "[4/7] Synthesizing findings"],
      ["step", "[5/7] Writing and editing report"],
      ["step", "[6/7] Running quality gates"],
      ["info", "INFO  editorial gate passed  warnings=0"],
      ["step", "[7/7] Rendering report"],
      ["info", "INFO  PDF rendered  pages=32  size=\"1060 KB\""],
      ["", ""],
      ["ok", "✓ Report generated"],
      ["dim", "  reports/kubernetes-platform-ai-infrastructure.pdf"],
      ["dim", "  reports/kubernetes-platform-ai-infrastructure.references.json"],
      ["cost", "Cost: $7.73"]
    ];
    var html = function (l) {
      var span = document.createElement("span");
      if (l[0]) span.className = "c-" + l[0];
      span.textContent = l[1];
      return span;
    };
    var cursor = document.createElement("span");
    cursor.className = "cursor";
    var i = 0;
    var tick = function () {
      if (i >= lines.length) {
        term.appendChild(cursor);
        setTimeout(function () { term.textContent = ""; i = 0; tick(); }, 6000);
        return;
      }
      if (cursor.parentNode) cursor.remove();
      term.appendChild(html(lines[i]));
      term.appendChild(document.createTextNode("\n"));
      term.appendChild(cursor);
      var kind = lines[i][0];
      i++;
      setTimeout(tick, kind === "step" ? 700 : 260);
    };
    if (reduced) {
      lines.forEach(function (l) { term.appendChild(html(l)); term.appendChild(document.createTextNode("\n")); });
    } else {
      tick();
    }
  }

  // Cost bars from recorded runs (manifest.json of each run).
  var ROLE_COLORS = {
    planner: "#b69cff", research: "#7cc4ff", followup: "#4f9cf0",
    "fact-checker": "#f5b454", synthesizer: "#6fdc9b", editorial: "#ff8a80"
  };
  var ROLE_NAMES = {
    planner: "Planner", research: "Researchers", followup: "Follow-up research",
    "fact-checker": "Fact-checker", synthesizer: "Synthesis", editorial: "Editor"
  };
  var RUNS = [
    { title: "Kubernetes for AI infrastructure", depth: "quick", calls: 11, findings: 72, sources: 77, mins: 13,
      roles: { planner: 0.12, research: 3.87, followup: 1.65, "fact-checker": 0.91, synthesizer: 0.47, editorial: 0.72 }, total: 7.73 },
    { title: "GPU monitoring tool roadmap", depth: "standard", calls: 13, findings: 120, sources: 147, mins: 24,
      roles: { planner: 0.19, research: 5.91, followup: 3.13, "fact-checker": 1.49, synthesizer: 0.86, editorial: 1.39 }, total: 12.96 },
    { title: "Self-hosting vs managed AI", depth: "deep", calls: 10, findings: 197, sources: 217, mins: 21,
      roles: { planner: 0.19, research: 20.07, "fact-checker": 1.29, synthesizer: 1.04, editorial: 1.51 }, total: 24.09 },
    { title: "Launching an agentic AI start-up", depth: "deep", calls: 10, findings: 178, sources: 207, mins: null,
      roles: { planner: 0.26, research: 25.55, "fact-checker": 2.19, synthesizer: 1.09, editorial: 1.64 }, total: 30.72 }
  ];
  var runsEl = document.getElementById("runs");
  if (runsEl) {
    var maxTotal = Math.max.apply(null, RUNS.map(function (r) { return r.total; }));
    RUNS.forEach(function (r) {
      var row = document.createElement("div");
      row.className = "run-row";
      var top = document.createElement("div");
      top.className = "top";
      var b = document.createElement("b"); b.textContent = r.title;
      var usd = document.createElement("span"); usd.className = "usd"; usd.textContent = "$" + r.total.toFixed(2);
      top.appendChild(b); top.appendChild(usd);
      var meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = "--depth " + r.depth + " · " + r.calls + " agent calls · " + r.findings + " findings · " + r.sources + " sources" + (r.mins ? " · ~" + r.mins + " min" : " · resumed run");
      var bar = document.createElement("div");
      bar.className = "bar";
      bar.style.width = (r.total / maxTotal * 100).toFixed(1) + "%";
      Object.keys(ROLE_COLORS).forEach(function (k) {
        if (!r.roles[k]) return;
        var s = document.createElement("span");
        s.style.flex = r.roles[k];
        s.style.background = ROLE_COLORS[k];
        s.title = ROLE_NAMES[k] + ": $" + r.roles[k].toFixed(2);
        bar.appendChild(s);
      });
      row.appendChild(top); row.appendChild(meta); row.appendChild(bar);
      runsEl.appendChild(row);
    });
    var legend = document.getElementById("legend");
    Object.keys(ROLE_COLORS).forEach(function (k) {
      var item = document.createElement("span");
      var sw = document.createElement("i"); sw.style.background = ROLE_COLORS[k];
      item.appendChild(sw); item.appendChild(document.createTextNode(ROLE_NAMES[k]));
      legend.appendChild(item);
    });
  }
})();
