package planimage

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// <img> で読み込まれた SVG は外のファイルを読めないので、フォントは埋め込むしかない
func embeddedFont(family string) string {
	data, err := fontFS.ReadFile("fonts/" + family + "[wght].woff2")
	if err != nil {
		panic(fmt.Sprintf("埋め込んだフォントがありません: %v", err))
	}
	return base64.StdEncoding.EncodeToString(data)
}

const (
	margin  = 32.0
	rankGap = 84.0 // 親と子の間。矢印の曲がり角とラベルを置く
	nodeGap = 40.0
	bendAt  = 26.0 // 親の下から曲がり角までの長さ
)

type edge struct {
	parent, child *card
	rel           string
	rows          float64
	actual        bool
	slot, slots   int
}

type point struct{ x, y float64 }

func renderSVG(plan, extras object) (string, error) {
	_, useTime := number(plan, "Actual Total Time")
	root := weight(plan, useTime)
	_, hasCost := number(plan, "Total Cost")
	// ノードが1つなら比べる相手がないので、重さは出さない
	weighed := (useTime || hasCost) && len(children(plan)) > 0

	var cards []*card
	var edges []edge
	depth := map[*card]int{}
	var walk func(p object, d int) *card
	walk = func(p object, d int) *card {
		share, ok := selfShare(p, root, useTime)
		c := newCard(fmt.Sprintf("n%d", len(cards)+1), p, share, ok && weighed)
		cards = append(cards, c)
		depth[c] = d
		cs := children(p)
		for i, child := range cs {
			cc := walk(child, d+1)
			rows, actual := totalRows(child)
			rel, _ := child.get("Parent Relationship")
			edges = append(edges, edge{c, cc, formatValue(rel), rows, actual, i, len(cs)})
		}
		return c
	}
	walk(plan, 0)

	pos, gw, gh, err := layoutDot(cards, edges)
	if err != nil {
		return "", err
	}

	width := math.Max(gw, 360) + 2*margin
	summary, headerH := layoutSummary(summarize(extras), width-2*margin)
	height := gh + 2*margin + headerH

	w := &svgWriter{}
	w.f(`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`, width, height, width, height)
	w.f(`<defs><style>@font-face{font-family:"Geist PI";font-weight:100 900;src:url(data:font/woff2;base64,%s) format("woff2")}@font-face{font-family:"Geist Mono PI";font-weight:100 900;src:url(data:font/woff2;base64,%s) format("woff2")}</style>`,
		embeddedFont("Geist"), embeddedFont("GeistMono"))
	// feDropShadow は SVG 2 の機能で、描けない環境があるので、SVG 1.1 の部品で影を作る
	w.b.WriteString(`<filter id="shadow" x="-15%" y="-10%" width="130%" height="140%"><feGaussianBlur in="SourceAlpha" stdDeviation="12"/><feOffset dy="10" result="far"/><feComponentTransfer in="far" result="farA"><feFuncA type="linear" slope="0.08"/></feComponentTransfer><feGaussianBlur in="SourceAlpha" stdDeviation="1"/><feOffset dy="1" result="near"/><feComponentTransfer in="near" result="nearA"><feFuncA type="linear" slope="0.06"/></feComponentTransfer><feMerge><feMergeNode in="farA"/><feMergeNode in="nearA"/></feMerge></filter></defs>`)
	w.f(`<rect width="%.0f" height="%.0f" fill="#ffffff"/>`, width, height)

	for _, it := range summary {
		w.icon(it.icon, margin+it.x, margin+it.y, 12, "#a1a1aa", 2)
		w.text(margin+it.x+18, margin+it.y+10, stSecHead, "#a1a1aa", it.label, "")
		for i, line := range it.lines {
			w.spans(margin+it.x+summaryHead(it.label), margin+it.y+10.5+float64(i)*summaryLine, line, "")
		}
	}

	// dot は同じ段のカードを中心でそろえるので、段ごとに一番上のカードに上端をそろえる
	rankTop := map[int]float64{}
	for _, c := range cards {
		top := pos[c.id].y - c.h/2
		if t, ok := rankTop[depth[c]]; !ok || top < t {
			rankTop[depth[c]] = top
		}
	}
	off := point{margin + (width-2*margin-gw)/2, margin + headerH}
	topLeft := func(c *card) point {
		return point{off.x + pos[c.id].x - cardW/2, off.y + rankTop[depth[c]]}
	}

	maxRows := 0.0
	for _, e := range edges {
		maxRows = math.Max(maxRows, e.rows)
	}
	for _, e := range edges {
		drawEdge(w, e, topLeft(e.parent), topLeft(e.child), maxRows)
	}
	for _, c := range cards {
		p := topLeft(c)
		c.draw(w, p.x, p.y)
	}
	w.b.WriteString(`</svg>`)
	return w.b.String(), nil
}

// 配置だけを dot に任せる。カードは大きさを固定した空の箱として渡す
func layoutDot(cards []*card, edges []edge) (map[string]point, float64, float64, error) {
	var in strings.Builder
	fmt.Fprintf(&in, "digraph plan {\n  graph [ordering=out, ranksep=%.3f, nodesep=%.3f];\n  node [shape=box, fixedsize=true, label=\"\"];\n", rankGap/72, nodeGap/72)
	for _, c := range cards {
		fmt.Fprintf(&in, "  %s [width=%.3f, height=%.3f];\n", c.id, cardW/72, c.h/72)
	}
	for _, e := range edges {
		fmt.Fprintf(&in, "  %s -> %s;\n", e.parent.id, e.child.id)
	}
	in.WriteString("}\n")

	cmd := exec.Command("dot", "-Tplain")
	cmd.Stdin = strings.NewReader(in.String())
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, 0, 0, fmt.Errorf("Graphviz で配置できません: %v: %s", err, stderr.String())
	}

	pos := map[string]point{}
	var gw, gh float64
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) >= 4 && f[0] == "graph":
			gw, _ = strconv.ParseFloat(f[2], 64)
			gh, _ = strconv.ParseFloat(f[3], 64)
		case len(f) >= 4 && f[0] == "node":
			x, _ := strconv.ParseFloat(f[2], 64)
			y, _ := strconv.ParseFloat(f[3], 64)
			pos[f[1]] = point{x * 72, y * 72}
		}
	}
	if len(pos) != len(cards) {
		return nil, 0, 0, fmt.Errorf("Graphviz の出力にカードの位置が %d 個しかありません(%d 個のはず)", len(pos), len(cards))
	}
	// -Tplain は下が 0 なので、上が 0 に直す
	for id, p := range pos {
		pos[id] = point{p.x, gh*72 - p.y}
	}
	return pos, gw * 72, gh * 72, nil
}

func drawEdge(w *svgWriter, e edge, parent, child point, maxRows float64) {
	sw := 1.5
	if maxRows > 0 {
		sw += 4.5 * math.Log10(e.rows+1) / math.Log10(maxRows+1)
	}
	color := "#c4c4bf"
	if maxRows > 0 && e.rows >= maxRows/4 {
		color = "#2563eb"
	}
	dash := ""
	if !e.actual {
		// 実際の行数がなく見積もりで描いた流れは、破線にする
		dash = ` stroke-dasharray="5 4"`
	}

	ax := parent.x + cardW/2 + (float64(e.slot)-float64(e.slots-1)/2)*24
	pb := parent.y + e.parent.h
	cx := child.x + cardW/2
	ct := child.y
	mid := pb + bendAt
	ah := math.Max(9, sw*1.8)
	aw := math.Max(6, sw*1.3)

	var d string
	if math.Abs(ax-cx) < 1 {
		d = fmt.Sprintf("M%.1f %.1fV%.1f", cx, ct, pb+ah)
	} else {
		r := math.Min(10, math.Abs(ax-cx)/2)
		dir := 1.0
		if ax < cx {
			dir = -1
		}
		d = fmt.Sprintf("M%.1f %.1fV%.1fQ%.1f %.1f %.1f %.1fH%.1fQ%.1f %.1f %.1f %.1fV%.1f",
			cx, ct, mid+r, cx, mid, cx+dir*r, mid, ax-dir*r, ax, mid, ax, mid-r, pb+ah)
	}
	w.f(`<path d="%s" fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round" stroke-linejoin="round"%s/>`, d, color, sw, dash)
	w.f(`<path d="M%.1f %.1fL%.1f %.1fL%.1f %.1fZ" fill="%s"/>`, ax, pb+1, ax-aw, pb+1+ah, ax+aw, pb+1+ah, color)

	unit := " rows"
	if !e.actual {
		unit = " rows (est)"
	}
	ss := []span{{stLabel, "#71717a", e.rel + " · "}, {stLabelN, "#18181b", commas(e.rows)}, {stLabel, "#71717a", unit}}
	lw := spansWidth(ss) + 30
	lx := cx + sw/2 + 8
	ly := (mid+ct)/2 - 10
	w.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="20" rx="6" fill="#ffffff" stroke="rgba(24,24,27,0.10)"/>`, lx, ly, lw)
	w.icon("up", lx+7, ly+4.5, 11, color, 2.2)
	w.spans(lx+22, ly+14, ss, "")
}

type summaryItem struct {
	icon, label, value, fill string
	style                    textStyle
	fields                   [][2]string // 値がオブジェクトのとき
	x, y                     float64
	lines                    [][]span
}

// 値は書き方を変えるだけで落とさない
func summarize(extras object) []summaryItem {
	var items []summaryItem
	for _, f := range extras {
		it := summaryItem{icon: "details", label: f.Key, value: formatValue(f.Value), fill: "#18181b", style: stLabelN}
		if o, ok := f.Value.(object); ok {
			for _, g := range o {
				it.fields = append(it.fields, [2]string{g.Key, formatValue(g.Value)})
			}
		}
		switch {
		case f.Key == "Triggers":
			it.icon = "bolt"
			triggers, _ := f.Value.([]any)
			if len(triggers) == 0 {
				// 空の配列は、動いたトリガーがないという意味
				it.value, it.fill, it.style = "none", "#a1a1aa", stLabel
				break
			}
			var parts []string
			for _, t := range triggers {
				o, ok := t.(object)
				if !ok {
					parts = append(parts, formatValue(t))
					continue
				}
				parts = append(parts, formatTrigger(o))
			}
			it.value = strings.Join(parts, " / ")
		case strings.HasSuffix(f.Key, " Time"):
			// EXPLAIN の時間はミリ秒
			it.icon = "clock"
			it.value += " ms"
		}
		items = append(items, it)
	}
	return items
}

// 決まったキー以外も後ろに残す
func formatTrigger(o object) string {
	get := func(k string) string {
		if v, ok := o.get(k); ok {
			return formatValue(v)
		}
		return ""
	}
	var parts []string
	head := get("Trigger Name")
	if c := get("Constraint Name"); c != "" {
		head += " (" + c + ")"
	}
	if r := get("Relation"); r != "" {
		head += " on " + r
	}
	parts = append(parts, head)
	if t := get("Time"); t != "" {
		parts = append(parts, t+" ms")
	}
	if c := get("Calls"); c != "" {
		parts = append(parts, "calls "+c)
	}
	for _, f := range o {
		switch f.Key {
		case "Trigger Name", "Constraint Name", "Relation", "Time", "Calls":
		default:
			parts = append(parts, f.Key+": "+formatValue(f.Value))
		}
	}
	return strings.Join(parts, " · ")
}

const summaryLine = 20.0

// 短い値は横に並べ、収まらない値とオブジェクトは1項目で数行を使う
func layoutSummary(items []summaryItem, width float64) ([]summaryItem, float64) {
	if len(items) == 0 {
		return nil, 0
	}
	x, y := 0.0, 0.0
	for i := range items {
		it := &items[i]
		head := summaryHead(it.label)
		if it.fields == nil {
			line := []span{{it.style, it.fill, it.value}}
			w := head + spansWidth(line)
			if x > 0 && x+w > width {
				x, y = 0, y+summaryLine
			}
			if x+w <= width {
				it.x, it.y, it.lines = x, y, [][]span{line}
				x += w + 24
				continue
			}
			for _, l := range it.style.wrap(it.value, width-head) {
				it.lines = append(it.lines, []span{{it.style, it.fill, l}})
			}
		} else {
			it.lines = packFields(it.fields, width-head)
		}
		if x > 0 {
			x, y = 0, y+summaryLine
		}
		it.x, it.y = 0, y
		y += float64(len(it.lines)) * summaryLine
	}
	if x > 0 {
		y += summaryLine
	}
	return items, y + 14
}

func summaryHead(label string) float64 {
	return 18 + stSecHead.width(label) + 8
}

// packFields は、オブジェクトのキーと値の組を「キー 値 · キー 値」と並べ、幅で折り返す
func packFields(fields [][2]string, width float64) [][]span {
	sep := span{stLabel, "#d4d4d8", "  ·  "}
	var lines [][]span
	var cur []span
	for _, f := range fields {
		field := []span{{stLabel, "#71717a", f[0] + " "}, {stLabelN, "#18181b", f[1]}}
		if len(cur) > 0 && spansWidth(cur)+spansWidth([]span{sep})+spansWidth(field) > width {
			lines = append(lines, cur)
			cur = nil
		}
		if len(cur) > 0 {
			cur = append(cur, sep)
		}
		cur = append(cur, field...)
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}
