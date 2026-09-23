package render

import (
	"fmt"
	"math"
	"sort"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// DiagramSVG renders a diagram definition deterministically.
//
//	architecture  layered graph, top to bottom
//	process       layered graph, left to right
//	timeline      nodes in order along a horizontal axis
func DiagramSVG(d *report.Diagram) string {
	if d == nil || len(d.Nodes) == 0 {
		return ""
	}
	if d.Kind == "timeline" {
		return timeline(d)
	}
	return layered(d, d.Kind == "architecture")
}

const (
	nodeW, nodeH = 150.0, 48.0
	nodeFont     = 10.0
)

var groupFills = []string{"#e7eef6", "#f2f4f7", "#eef3ea", "#fbf4e7", "#f3eef6", "#e9f3f3"}

// layers assigns each node a layer by longest path from a source. Cycles are
// broken by ignoring edges that point back to a node already on the stack.
func layers(d *report.Diagram) map[string]int {
	out := map[string][]string{}
	indeg := map[string]int{}
	for _, e := range d.Edges {
		out[e.From] = append(out[e.From], e.To)
		indeg[e.To]++
	}
	layer := map[string]int{}
	state := map[string]int{} // 0 unvisited, 1 on stack, 2 done
	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if state[id] == 1 {
			return
		}
		if depth <= layer[id] && state[id] == 2 {
			return
		}
		if depth > layer[id] {
			layer[id] = depth
		}
		state[id] = 1
		for _, to := range out[id] {
			visit(to, layer[id]+1)
		}
		state[id] = 2
	}
	for _, n := range d.Nodes {
		if indeg[n.ID] == 0 {
			visit(n.ID, 0)
		}
	}
	for _, n := range d.Nodes { // nodes only reachable through cycles
		if _, ok := layer[n.ID]; !ok {
			visit(n.ID, 0)
		}
	}
	return layer
}

type placed struct {
	report.Node
	x, y float64 // centre
}

func layered(d *report.Diagram, vertical bool) string {
	layer := layers(d)
	byLayer := map[int][]report.Node{}
	maxLayer := 0
	for _, n := range d.Nodes {
		l := layer[n.ID]
		byLayer[l] = append(byLayer[l], n)
		maxLayer = max(maxLayer, l)
	}
	widest := 0
	for _, ns := range byLayer {
		widest = max(widest, len(ns))
	}

	gapMain, gapCross := 46.0, 22.0
	if vertical {
		gapMain = 40
	}
	var W, H float64
	pos := map[string]placed{}
	for l := 0; l <= maxLayer; l++ {
		ns := byLayer[l]
		for i, n := range ns {
			var x, y float64
			if vertical {
				rowW := float64(len(ns))*nodeW + float64(len(ns)-1)*gapCross
				fullW := float64(widest)*nodeW + float64(widest-1)*gapCross
				x = (fullW-rowW)/2 + float64(i)*(nodeW+gapCross) + nodeW/2 + 10
				y = float64(l)*(nodeH+gapMain) + nodeH/2 + 10
			} else {
				colH := float64(len(ns))*nodeH + float64(len(ns)-1)*gapCross
				fullH := float64(widest)*nodeH + float64(widest-1)*gapCross
				x = float64(l)*(nodeW+gapMain) + nodeW/2 + 10
				y = (fullH-colH)/2 + float64(i)*(nodeH+gapCross) + nodeH/2 + 10
			}
			pos[n.ID] = placed{Node: n, x: x, y: y}
			W = math.Max(W, x+nodeW/2+10)
			H = math.Max(H, y+nodeH/2+10)
		}
	}

	groups := groupIndex(d.Nodes)
	legendH := 0.0
	if len(groups) > 1 {
		legendH = 22
	}
	// Keep small diagrams from being blown up to full width.
	minW := 560.0
	offX := 0.0
	if W < minW {
		offX = (minW - W) / 2
		W = minW
	}

	var s svgBuf
	s.f(`<svg class="diagram" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H+legendH)
	s.f(`<defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="%s"/></marker></defs>`, colMuted)
	s.f(`<g transform="translate(%.1f,0)">`, offX)
	for _, e := range d.Edges {
		a, b := pos[e.From], pos[e.To]
		var x1, y1, x2, y2 float64
		if vertical {
			x1, y1, x2, y2 = a.x, a.y+nodeH/2, b.x, b.y-nodeH/2
			if b.y <= a.y { // back edge
				y1, y2 = a.y-nodeH/2, b.y+nodeH/2
			}
		} else {
			x1, y1, x2, y2 = a.x+nodeW/2, a.y, b.x-nodeW/2, b.y
			if b.x <= a.x {
				x1, x2 = a.x-nodeW/2, b.x+nodeW/2
			}
		}
		var path string
		if vertical {
			my := (y1 + y2) / 2
			path = fmt.Sprintf("M%.1f,%.1f C%.1f,%.1f %.1f,%.1f %.1f,%.1f", x1, y1, x1, my, x2, my, x2, y2)
		} else {
			mx := (x1 + x2) / 2
			path = fmt.Sprintf("M%.1f,%.1f C%.1f,%.1f %.1f,%.1f %.1f,%.1f", x1, y1, mx, y1, mx, y2, x2, y2)
		}
		s.f(`<path d="%s" fill="none" stroke="%s" stroke-width="1.1" marker-end="url(#arrow)"/>`, path, colMuted)
		if e.Label != "" {
			lx, ly := (x1+x2)/2, (y1+y2)/2
			lw := textWidth(e.Label, 8.5) + 8
			s.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="13" fill="#ffffff"/>`, lx-lw/2, ly-8, lw)
			s.text(lx, ly+2.5, "middle", colMuted, 8.5, 400, truncateRunes(e.Label, 28))
		}
	}
	ids := make([]string, 0, len(pos))
	for id := range pos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := pos[id]
		fill := groupFills[groups[p.Group]%len(groupFills)]
		s.f(`<rect x="%.1f" y="%.1f" width="%.0f" height="%.0f" rx="3" fill="%s" stroke="%s" stroke-width="0.9"/>`,
			p.x-nodeW/2, p.y-nodeH/2, nodeW, nodeH, fill, "#0d3b66")
		lines := wrap(p.Label, nodeW-14, nodeFont, 2)
		if p.Detail != "" && len(lines) == 1 {
			s.text(p.x, p.y-2, "middle", colInk, nodeFont, 600, lines[0])
			s.text(p.x, p.y+11, "middle", colMuted, 8.5, 400, truncateRunes(p.Detail, 26))
			continue
		}
		startY := p.y + 3.5 - float64(len(lines)-1)*6
		for k, line := range lines {
			s.text(p.x, startY+float64(k)*12, "middle", colInk, nodeFont, 600, line)
		}
	}
	s.f(`</g>`)
	if len(groups) > 1 {
		x := 10.0
		names := make([]string, len(groups))
		for g, i := range groups {
			names[i] = g
		}
		for i, g := range names {
			if g == "" {
				g = "Other"
			}
			s.f(`<rect x="%.1f" y="%.1f" width="10" height="10" fill="%s" stroke="#0d3b66" stroke-width="0.6"/>`, x, H+6, groupFills[i%len(groupFills)])
			s.text(x+14, H+14.5, "start", colMuted, 9, 400, g)
			x += 14 + textWidth(g, 9) + 18
		}
	}
	s.f(`</svg>`)
	return s.String()
}

// groupIndex numbers groups in order of first appearance.
func groupIndex(nodes []report.Node) map[string]int {
	idx := map[string]int{}
	for _, n := range nodes {
		if _, ok := idx[n.Group]; !ok {
			idx[n.Group] = len(idx)
		}
	}
	return idx
}

func timeline(d *report.Diagram) string {
	n := len(d.Nodes)
	const W = 640.0
	const H = 170.0
	left, right := 50.0, 50.0
	axisY := H / 2
	step := 0.0
	if n > 1 {
		step = (W - left - right) / float64(n-1)
	}
	slot := math.Max(step*2-12, 90)
	var s svgBuf
	s.f(`<svg class="diagram" viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" role="img">`, W, H)
	s.f(`<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="1.4"/>`, left-20, W-right+20, axisY, axisY, colInk)
	for i, node := range d.Nodes {
		x := left + float64(i)*step
		if n == 1 {
			x = W / 2
		}
		above := i%2 == 0
		s.f(`<circle cx="%.1f" cy="%.1f" r="4.5" fill="#0d3b66"/>`, x, axisY)
		dir := -1.0
		if !above {
			dir = 1
		}
		s.f(`<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="0.8"/>`, x, x, axisY+dir*6, axisY+dir*22, colAxis)
		lines := wrap(node.Label, slot, 9.5, 2)
		if above {
			y := axisY - 28 - float64(len(lines)-1)*11
			if node.Detail != "" {
				s.text(x, y-13, "middle", "#0d3b66", 8.5, 700, node.Detail)
			}
			for k, line := range lines {
				s.text(x, y+float64(k)*11, "middle", colInk, 9.5, 500, line)
			}
		} else {
			y := axisY + 36
			if node.Detail != "" {
				s.text(x, y, "middle", "#0d3b66", 8.5, 700, node.Detail)
				y += 13
			}
			for k, line := range lines {
				s.text(x, y+float64(k)*11, "middle", colInk, 9.5, 500, line)
			}
		}
	}
	s.f(`</svg>`)
	return s.String()
}
