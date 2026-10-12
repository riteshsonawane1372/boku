package render

import (
	"math"
	"sort"

	"github.com/riteshsonawane1372/boku/internal/report"
)

// DiagramSVG renders a diagram definition deterministically.
//
//	architecture  layered graph, top to bottom
//	process       layered graph, left to right
//	timeline      nodes in order along a horizontal axis
func DiagramSVG(d *report.Diagram, th Theme) string {
	cv := diagramCanvas(d, th)
	if cv == nil {
		return ""
	}
	return cv.SVG()
}

// diagramCanvas draws a diagram; nil when there is nothing to draw.
func diagramCanvas(d *report.Diagram, th Theme) *canvas {
	if d == nil || len(d.Nodes) == 0 {
		return nil
	}
	if d.Kind == "timeline" {
		return timeline(d, th)
	}
	return layered(d, th, d.Kind == "architecture")
}

const (
	nodeW, nodeH = 156.0, 52.0
	nodeFont     = 11.0
)

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

func layered(d *report.Diagram, th Theme, vertical bool) *canvas {
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

	gapMain, gapCross := 50.0, 22.0
	if vertical {
		gapMain = 42
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
		legendH = 26
	}
	// Keep small diagrams from being blown up to full width.
	minW := 560.0
	offX := 0.0
	if W < minW {
		offX = (minW - W) / 2
		W = minW
	}

	cv := newCanvas("diagram", W, H+legendH)
	edge := mix(th.Muted, "#ffffff", 0.15)
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
		x1, x2 = x1+offX, x2+offX
		c1, c2 := pt{x1, (y1 + y2) / 2}, pt{x2, (y1 + y2) / 2}
		if !vertical {
			c1, c2 = pt{(x1 + x2) / 2, y1}, pt{(x1 + x2) / 2, y2}
		}
		cv.curve(pt{x1, y1}, c1, c2, pt{x2, y2}, edge, 1.3).arrow = true
		if e.Label != "" {
			lx, ly := (x1+x2)/2, (y1+y2)/2
			label := truncateRunes(e.Label, 28)
			lw := textWidth(label, 9.5) + 10
			cv.rect(lx-lw/2, ly-8.5, lw, 15, 4, "#ffffff")
			cv.text(lx, ly+2.8, "middle", th.Muted, 9.5, 500, label)
		}
	}
	ids := make([]string, 0, len(pos))
	for id := range pos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := pos[id]
		col := th.series(groups[p.Group])
		x := p.x + offX
		box := cv.rect(x-nodeW/2, p.y-nodeH/2, nodeW, nodeH, 9, mix(col, "#ffffff", 0.9))
		box.stroke, box.sw = mix(col, "#ffffff", 0.45), 1.1
		cv.rect(x-nodeW/2+7, p.y-nodeH/2+11, 3.5, nodeH-22, 1.75, col)
		lines := wrap(p.Label, nodeW-30, nodeFont, 2)
		tx := x + 4
		if p.Detail != "" && len(lines) == 1 {
			cv.text(tx, p.y-2, "middle", th.Ink, nodeFont, 600, lines[0])
			cv.text(tx, p.y+12, "middle", th.Muted, 9.5, 400, truncateRunes(p.Detail, 26))
			continue
		}
		startY := p.y + 4 - float64(len(lines)-1)*6.5
		for k, line := range lines {
			cv.text(tx, startY+float64(k)*13, "middle", th.Ink, nodeFont, 600, line)
		}
	}
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
			cv.rect(x, H+8, 10, 10, 3, th.series(i))
			cv.text(x+15, H+17, "start", th.Muted, 10, 500, g)
			x += 15 + textWidth(g, 10) + 20
		}
	}
	return cv
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

func timeline(d *report.Diagram, th Theme) *canvas {
	n := len(d.Nodes)
	const W = 640.0
	const H = 190.0
	left, right := 56.0, 56.0
	axisY := H / 2
	step := 0.0
	if n > 1 {
		step = (W - left - right) / float64(n-1)
	}
	slot := math.Max(step*2-14, 96)
	cv := newCanvas("diagram", W, H)
	cv.line(left-24, axisY, W-right+24, axisY, th.Rule, 2.4)
	for i, node := range d.Nodes {
		x := left + float64(i)*step
		if n == 1 {
			x = W / 2
		}
		above := i%2 == 0
		dir := 1.0
		if above {
			dir = -1
		}
		cv.line(x, axisY+dir*8, x, axisY+dir*24, th.Rule, 1.2)
		dot := cv.circle(x, axisY, 5.5, th.Accent)
		dot.stroke, dot.sw = "#ffffff", 2
		lines := wrap(node.Label, slot, 10.5, 2)
		if above {
			y := axisY - 32 - float64(len(lines)-1)*12.5
			if node.Detail != "" {
				cv.text(x, y-15, "middle", th.Accent, 9.5, 700, node.Detail)
			}
			for k, line := range lines {
				cv.text(x, y+float64(k)*12.5, "middle", th.Ink, 10.5, 500, line)
			}
		} else {
			y := axisY + 40
			if node.Detail != "" {
				cv.text(x, y, "middle", th.Accent, 9.5, 700, node.Detail)
				y += 15
			}
			for k, line := range lines {
				cv.text(x, y+float64(k)*12.5, "middle", th.Ink, 10.5, 500, line)
			}
		}
	}
	return cv
}
