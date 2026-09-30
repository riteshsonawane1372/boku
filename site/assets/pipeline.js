// Boku site: animated replay of a real run through the agent pipeline.
//
// Full mode replays run 2026-09-23T024958 (recorded per-task start, duration
// and cost from its manifest.json). Short mode re-times the same agents as a
// short run would schedule them (an estimate). Quick mode replays a local
// Ollama run (see QUICK below).
(function () {
  "use strict";
  var svg = document.getElementById("pipe-svg");
  if (!svg) return;
  var NS = "http://www.w3.org/2000/svg";
  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  var FULL_R = [
    { id: "r1", label: "Technical", sub: "upstream-tech-stack" },
    { id: "r2", label: "Market", sub: "adoption-standards" },
    { id: "r3", label: "Competitive", sub: "vendor-platforms" },
    { id: "r4", label: "Case study", sub: "production-case-studies" }
  ];

  // [node, start s, end s, cost USD, findings, log line]
  var FULL = {
    runtime: "Claude Code (claude -p)",
    topic: "How Kubernetes is being used for AI infrastructure",
    researchers: FULL_R,
    followups: true,
    speed: 30,
    total: 7.73, // manifest total; per-task costs are rounded to cents
    note: "Recorded timings and costs from run 2026-09-23T024958 (--depth quick): 11 agent calls, $7.73.",
    events: [
      ["planner", 0, 36, 0.12, 0, "planner: 4 workstreams"],
      ["r1", 36, 296, 1.48, 15, "technical research: 15 findings, 26 sources"],
      ["r2", 36, 194, 0.75, 13, "market research: 13 findings"],
      ["r3", 36, 173, 0.79, 12, "competitive research: 12 findings"],
      ["r4", 36, 213, 0.85, 11, "case-study research: 11 findings"],
      ["fact", 296, 360, 0.50, 0, "fact-check round 1: needs revision, 8 issues, 2 follow-ups"],
      ["f1", 360, 509, 0.75, 10, "follow-up (case study): 10 findings"],
      ["f2", 360, 531, 0.90, 9, "follow-up (primary): 9 findings"],
      ["fact", 531, 568, 0.41, 2, "fact-check round 2: pass, 2 claims rejected"],
      ["synth", 568, 635, 0.47, 0, "synthesis: thesis, insights, outline"],
      ["editor", 635, 766, 0.72, 0, "editor: draft passes the editorial gate"],
      ["render", 766, 797, 0, 0, "PDF gate passed: 32 pages"]
    ]
  };

  var SHORT = {
    runtime: "Claude Code (claude -p)",
    topic: "How Kubernetes is being used for AI infrastructure",
    researchers: FULL_R.slice(1),
    followups: false,
    speed: 22,
    note: "Estimate: the same run's per-agent costs and durations with 3 researchers and one fact-check round (≈ $4.20).",
    events: [
      ["planner", 0, 36, 0.12, 0, "planner: 3 workstreams, short outline"],
      ["r1", 36, 194, 0.75, 13, "market research: 13 findings"],
      ["r2", 36, 173, 0.79, 12, "competitive research: 12 findings"],
      ["r3", 36, 213, 0.85, 11, "case-study research: 11 findings"],
      ["fact", 213, 277, 0.50, 0, "fact-check (one round): limitations noted"],
      ["synth", 277, 344, 0.47, 0, "synthesis: 3-section outline"],
      ["editor", 344, 475, 0.72, 0, "editor: compact brief"],
      ["render", 475, 480, 0, 0, "compact PDF rendered"]
    ]
  };

  // Recorded task durations of a --quick run on llama3.1:8b (Apple silicon),
  // laid end to end (the run was resumed once after a fix, see the note).
  var QUICK = {
    runtime: "Ollama · llama3.1:8b (local)",
    topic: "What are the main approaches to GPU sharing on Kubernetes in 2026?",
    researchers: [
      { id: "r1", label: "Market", sub: "local model" },
      { id: "r2", label: "Technical", sub: "local model" },
      { id: "r3", label: "Case study", sub: "local model" }
    ],
    followups: false,
    local: true,
    speed: 35,
    note: "Recorded durations of a --quick run on llama3.1:8b: about 15 minutes, $0 API spend, a 5-page compact report. No fact-check.",
    events: [
      ["planner", 0, 33, 0, 0, "planner (local): 3 workstreams, 4 pages read"],
      ["r1", 33, 175, 0, 5, "market research: 5 findings from fetched pages"],
      ["r2", 33, 270, 0, 1, "technical research: 10 findings, 9 cited unlisted pages"],
      ["r3", 175, 422, 0, 8, "case-study research: 8 findings"],
      ["synth", 422, 490, 0, 0, "synthesis (local): 5 insights, 5 sections"],
      ["editor", 490, 665, 0, 0, "editor (local): draft; citations matched to findings"],
      ["editor", 665, 852, 0, 0, "editor (local): revision"],
      ["fmt", 852, 912, 0, 0, "formatter (local): 5 passages rewritten"],
      ["render", 912, 914, 0, 0, "compact PDF rendered: 5 pages"]
    ]
  };

  var MODES = { full: FULL, short: SHORT, quick: QUICK };

  function el(name, attrs, parent) {
    var n = document.createElementNS(NS, name);
    for (var k in attrs) n.setAttribute(k, attrs[k]);
    if (parent) parent.appendChild(n);
    return n;
  }

  var nodes, edges, packets;

  function layout(mode) {
    var m = MODES[mode];
    var L = {
      request: { x: 16, y: 180, w: 104, h: 54, label: "Request", sub: "your question" },
      planner: { x: 146, y: 180, w: 108, h: 54, label: "Planner", sub: "plan.json" },
      fact: { x: 468, y: 180, w: 116, h: 54, label: "Fact-checker", sub: "critic" },
      synth: { x: 608, y: 180, w: 112, h: 54, label: "Synthesizer", sub: "outline" },
      editor: { x: 752, y: 180, w: 128, h: 54, label: "Editor", sub: "document.json" },
      fmt: { x: 608, y: 300, w: 112, h: 46, label: "Formatter", sub: "style fixes", local: true },
      render: { x: 752, y: 300, w: 128, h: 46, label: "Renderer", sub: "HTML → PDF" },
      pdf: { x: 740, y: 396, w: 66, h: 40, label: "PDF", sub: "" },
      refs: { x: 814, y: 396, w: 76, h: 40, label: "refs.json", sub: "" }
    };
    var rs = m.researchers;
    var top = rs.length === 4 ? 36 : 70, gap = rs.length === 4 ? 82 : 95;
    rs.forEach(function (r, i) {
      L[r.id] = { x: 284, y: top + i * gap, w: 150, h: 50, label: r.label, sub: r.sub };
    });
    if (m.followups) {
      L.f1 = { x: 284, y: 372, w: 150, h: 40, label: "Follow-up", sub: "case study" };
      L.f2 = { x: 284, y: 420, w: 150, h: 40, label: "Follow-up", sub: "primary" };
    }
    return L;
  }

  function center(n, side) {
    if (side === "l") return [n.x, n.y + n.h / 2];
    if (side === "r") return [n.x + n.w, n.y + n.h / 2];
    if (side === "t") return [n.x + n.w / 2, n.y];
    return [n.x + n.w / 2, n.y + n.h];
  }

  function curve(a, b) {
    var dx = (b[0] - a[0]) / 2;
    return "M" + a[0] + "," + a[1] + " C" + (a[0] + dx) + "," + a[1] + " " + (b[0] - dx) + "," + b[1] + " " + b[0] + "," + b[1];
  }

  function build(mode) {
    var m = MODES[mode];
    while (svg.firstChild) svg.removeChild(svg.firstChild);
    var L = layout(mode);
    nodes = {}; edges = {}; packets = el("g", {}, null);

    [["Plan", 200], ["Research · parallel", 359], ["Critic", 526], ["Write", 736], ["", 0]].forEach(function (l) {
      if (!l[0]) return;
      var t = el("text", { x: l[1], y: 18, "text-anchor": "middle", "class": "lane-label" }, svg);
      t.textContent = l[0];
    });

    var eg = el("g", {}, svg);
    function edge(from, to, sideA, sideB, cls) {
      if (!L[from] || !L[to]) return;
      var d = sideA === "loop"
        ? loopPath(L[from], L[to])
        : curve(center(L[from], sideA || "r"), center(L[to], sideB || "l"));
      var p = el("path", { d: d, "class": "edge" + (cls ? " " + cls : "") }, eg);
      edges[from + ">" + to] = p;
    }
    function loopPath(a, b) {
      // fact-checker (a) down and back to a follow-up node (b)
      var s = center(a, "b"), e = center(b, "r");
      return "M" + s[0] + "," + s[1] + " C" + s[0] + "," + (e[1]) + " " + (e[0] + 40) + "," + e[1] + " " + e[0] + "," + e[1];
    }

    edge("request", "planner");
    m.researchers.forEach(function (r) {
      edge("planner", r.id);
      edge(r.id, "fact");
    });
    if (m.followups) {
      edge("fact", "f1", "loop", null, "loop");
      edge("fact", "f2", "loop", null, "loop");
      edge("f1", "fact", "r", "b");
      edge("f2", "fact", "r", "b");
    }
    edge("fact", "synth");
    edge("synth", "editor");
    edge("editor", "fmt", "b", "r", "loop");
    edge("editor", "render", "b", "t");
    edge("render", "pdf", "b", "t");
    edge("render", "refs", "b", "t");

    var ng = el("g", {}, svg);
    Object.keys(L).forEach(function (id) {
      var n = L[id];
      var agentNode = ["request", "render", "pdf", "refs"].indexOf(id) < 0;
      var isLocal = agentNode && (m.local || n.local);
      var g = el("g", { "class": "node" + (isLocal ? " local" : "") }, ng);
      el("rect", { x: n.x, y: n.y, width: n.w, height: n.h, rx: 10 }, g);
      var t = el("text", { x: n.x + 12, y: n.y + (n.sub ? n.h / 2 - 3 : n.h / 2 + 4) }, g);
      t.textContent = n.label;
      var s = el("text", { x: n.x + 12, y: n.y + n.h / 2 + 12, "class": "sub" }, g);
      s.textContent = n.sub || "";
      if (isLocal) {
        var tag = el("text", { x: n.x + n.w - 2, y: n.y - 5, "text-anchor": "end", "class": "tag", fill: "var(--green)" }, g);
        tag.textContent = "LOCAL";
      }
      nodes[id] = { g: g, sub: s, box: n, cost: 0, findings: 0 };
    });
    if (mode === "quick") nodes.fact.g.classList.add("skipped"), (nodes.fact.sub.textContent = "skipped in quick");
    if (mode !== "quick" && !hasEvent(m, "fmt")) nodes.fmt.sub.textContent = "style fixes only";
    nodes.request.g.classList.add("done");
    svg.appendChild(packets);
  }

  function hasEvent(m, id) {
    return m.events.some(function (e) { return e[0] === id; });
  }

  // Upstream nodes that feed a node, for packet animation.
  function upstream(mode, id) {
    var m = MODES[mode];
    var rs = m.researchers.map(function (r) { return r.id; });
    if (id === "planner") return ["request"];
    if (rs.indexOf(id) >= 0) return ["planner"];
    if (id === "f1" || id === "f2") return ["fact"];
    if (id === "fact") return rs;
    if (id === "synth") return ["fact"];
    if (id === "editor") return ["synth"];
    if (id === "fmt") return ["editor"];
    if (id === "render") return ["editor"];
    return [];
  }

  function sendPacket(from, to, local) {
    var path = edges[from + ">" + to];
    if (!path || reduced) return;
    var len = path.getTotalLength();
    var c = el("circle", { r: 4.5, "class": "packet" + (local ? " local" : "") }, packets);
    path.classList.add("hot");
    var t0 = performance.now(), dur = 700;
    (function step(now) {
      var k = Math.min(1, (now - t0) / dur);
      var p = path.getPointAtLength(len * k);
      c.setAttribute("cx", p.x); c.setAttribute("cy", p.y);
      if (k < 1) requestAnimationFrame(step);
      else { c.remove(); path.classList.remove("hot"); }
    })(t0);
  }

  var $ = function (id) { return document.getElementById(id); };
  var raf, currentMode = "full";

  function fmtTime(s) {
    s = Math.floor(s);
    return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
  }

  function log(sim, text) {
    var feed = $("feed");
    var d = document.createElement("div");
    var t = document.createElement("span"); t.className = "t"; t.textContent = fmtTime(sim);
    d.appendChild(t); d.appendChild(document.createTextNode(text));
    feed.appendChild(d);
    while (feed.children.length > 12) feed.removeChild(feed.firstChild);
  }

  function play(mode) {
    cancelAnimationFrame(raf);
    currentMode = mode;
    var m = MODES[mode];
    build(mode);
    $("feed").textContent = "";
    $("s-runtime").textContent = m.runtime;
    $("pipe-note").textContent = m.note;
    var cost = 0, calls = 0, findings = 0;
    var state = m.events.map(function () { return 0; }); // 0 pending, 1 running, 2 done
    var end = m.events.reduce(function (a, e) { return Math.max(a, e[2]); }, 0);
    var start = performance.now();
    log(0, "boku report \"" + m.topic + "\"" + (mode === "full" ? "" : " --" + mode));

    function frame(now) {
      var sim = reduced ? end : (now - start) / 1000 * m.speed;
      m.events.forEach(function (e, i) {
        var n = nodes[e[0]];
        if (!n) return;
        if (state[i] === 0 && sim >= e[1]) {
          state[i] = 1;
          n.g.classList.remove("done");
          n.g.classList.add("active");
          upstream(mode, e[0]).forEach(function (u) { sendPacket(u, e[0], m.local || e[0] === "fmt"); });
        }
        if (state[i] === 1 && sim >= e[2]) {
          state[i] = 2;
          n.g.classList.remove("active");
          n.g.classList.add("done");
          if (e[0] !== "render") calls++;
          cost += e[3]; findings += e[4]; n.cost += e[3]; n.findings += e[4];
          var bits = [];
          if (n.findings && e[0] !== "fact") bits.push(n.findings + " findings");
          if (m.local) bits.push("$0"); else if (n.cost) bits.push("$" + n.cost.toFixed(2));
          if (bits.length) n.sub.textContent = bits.join(" · ");
          log(e[2], e[5]);
          if (e[0] === "render") {
            ["pdf", "refs"].forEach(function (o) { sendPacket("render", o, m.local); nodes[o].g.classList.add("done"); });
          }
        }
      });
      $("s-time").textContent = fmtTime(Math.min(sim, end));
      $("s-cost").textContent = "$" + cost.toFixed(2);
      $("s-calls").textContent = calls;
      $("s-find").textContent = findings;
      if (sim >= end && m.total) $("s-cost").textContent = "$" + m.total.toFixed(2), (cost = m.total);
      if (sim < end + 2) raf = requestAnimationFrame(frame);
      else log(end, m.local ? "✓ report generated · $0.00 API spend" : "✓ report generated · cost $" + cost.toFixed(2));
    }
    raf = requestAnimationFrame(frame);
  }

  var started = false;
  document.querySelectorAll("[data-mode]").forEach(function (b) {
    b.addEventListener("click", function () {
      started = true;
      document.querySelectorAll("[data-mode]").forEach(function (o) { o.setAttribute("aria-pressed", String(o === b)); });
      play(b.getAttribute("data-mode"));
    });
  });
  $("replay").addEventListener("click", function () { started = true; play(currentMode); });

  // Start when the pipeline scrolls into view.
  var io = new IntersectionObserver(function (entries) {
    if (!started && entries[0].isIntersecting) { started = true; play("full"); }
  }, { threshold: 0.25 });
  io.observe(svg);
  build("full");
})();
