package planimage

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// 寸法はデザインの見本(Spec Sheet)に合わせる
const (
	cardW   = 340.0
	cardPad = 16.0
	innerW  = cardW - 2*cardPad
)

var (
	stTag     = textStyle{weight: 700, size: 10.5, spacing: 1.47, upper: true}
	stSelf    = textStyle{weight: 500, size: 11}
	stSelfNum = textStyle{weight: 800, size: 14}
	stName    = textStyle{weight: 800, size: 26, spacing: -0.78}
	stTarget  = textStyle{mono: true, weight: 400, size: 13}
	stSecHead = textStyle{weight: 600, size: 9.5, spacing: 1.71, upper: true}
	stCond    = textStyle{mono: true, weight: 500, size: 13}
	stBig     = textStyle{weight: 200, size: 38, spacing: -1.52}
	stSub     = textStyle{weight: 400, size: 11}
	stSubNum  = textStyle{mono: true, weight: 700, size: 11}
	stBadge   = textStyle{weight: 700, size: 10.5}
	stKey     = textStyle{weight: 400, size: 10.5}
	stVal     = textStyle{mono: true, weight: 500, size: 10.5}
	stLabel   = textStyle{weight: 400, size: 11}
	stLabelN  = textStyle{mono: true, weight: 700, size: 11}
)

// 見出しや行数の区画にまとめて出すキー。DETAILS には出さない
var headerKeys = map[string]bool{
	"Node Type": true, "Strategy": true, "Join Type": true, "Parent Relationship": true,
	"Relation Name": true, "Alias": true, "Index Name": true,
	"Actual Rows": true, "Plan Rows": true, "Actual Loops": true,
}

func isCondKey(k string) bool {
	return (strings.HasSuffix(k, "Cond") || strings.HasSuffix(k, "Filter")) && !strings.HasPrefix(k, "Rows Removed")
}

func isKeyKey(k string) bool { return strings.HasSuffix(k, " Key") }

func isRowsKey(k string) bool {
	return strings.HasPrefix(k, "Rows Removed") || k == "Index Searches" || k == "Heap Fetches"
}

type heat int

const (
	heatNone heat = iota
	heatWarm
	heatHot
)

type card struct {
	id       string
	cat      category
	icon     string
	name     []string
	nameSize float64
	tag      string
	target   []string
	share    float64
	hasShare bool
	heat     heat

	conds  []string
	keys   []string
	big    string
	loops  string
	plan   string
	est    *badge
	extras [][2]string
	detail [][2]string

	h float64
}

type badge struct {
	text, icon, fg, bg string
}

func estimate(p object) *badge {
	a, ok1 := number(p, "Actual Rows")
	e, ok2 := number(p, "Plan Rows")
	if !ok1 || !ok2 {
		return nil
	}
	r := math.Max(a, 1) / math.Max(e, 1)
	if r < 1 {
		r = 1 / r
	}
	switch {
	case r < 2:
		return &badge{"est", "check", "#166534", "#dcfce7"}
	case r < 10:
		return &badge{fmt.Sprintf("est ×%.1f", r), "alert", "#92400e", "#fef3c7"}
	default:
		return &badge{fmt.Sprintf("est ×%.0f", r), "alert", "#991b1b", "#fee2e2"}
	}
}

func newCard(id string, p object, share float64, hasShare bool) *card {
	get := func(k string) string {
		if v, ok := p.get(k); ok {
			return formatValue(v)
		}
		return ""
	}
	nodeType := get("Node Type")
	cat, icon := classify(nodeType)
	c := &card{id: id, cat: cat, icon: icon, share: share, hasShare: hasShare}

	tag := []string{cat.name}
	for _, k := range []string{"Join Type", "Strategy"} {
		if v := get(k); v != "" {
			tag = append(tag, v)
		}
	}
	c.tag = strings.Join(tag, " · ")

	// 名前は、収まらなければ小さくし、それでも収まらなければ折り返す
	nameW := innerW - 48
	c.nameSize = stName.size
	for c.nameSize > 18 && stName.withSize(c.nameSize).width(nodeType) > nameW {
		c.nameSize--
	}
	c.name = stName.withSize(c.nameSize).wrap(nodeType, nameW)

	var target []string
	if v := get("Index Name"); v != "" {
		target = append(target, "using "+v)
	}
	if v := get("Relation Name"); v != "" {
		on := "on " + v
		if a := get("Alias"); a != "" && a != v {
			on += " " + a
		}
		target = append(target, on)
	}
	if len(target) > 0 {
		c.target = stTarget.wrap(strings.Join(target, " "), nameW-18)
	}

	if hasShare {
		switch {
		case share >= 0.3:
			c.heat = heatHot
		case share >= 0.1:
			c.heat = heatWarm
		}
	}

	if v := get("Actual Rows"); v != "" {
		c.big, c.loops, c.plan = v, get("Actual Loops"), get("Plan Rows")
	} else if v := get("Plan Rows"); v != "" {
		c.big = v
	}
	c.est = estimate(p)

	for _, f := range p {
		if f.Key == "Plans" || headerKeys[f.Key] {
			continue
		}
		v := formatValue(f.Value)
		switch {
		case isCondKey(f.Key):
			c.conds = append(c.conds, stCond.wrap(f.Key+": "+v, innerW)...)
		case isKeyKey(f.Key):
			c.keys = append(c.keys, stCond.wrap(f.Key+": "+v, innerW)...)
		case isRowsKey(f.Key):
			c.extras = append(c.extras, [2]string{f.Key, v})
		default:
			c.detail = append(c.detail, [2]string{f.Key, v})
		}
	}
	c.h = c.layout(nil)
	return c
}

func (st textStyle) withSize(size float64) textStyle {
	st.spacing = st.spacing / st.size * size
	st.size = size
	return st
}

type svgWriter struct {
	b strings.Builder
}

func (w *svgWriter) f(format string, args ...any) {
	fmt.Fprintf(&w.b, format, args...)
}

func esc(s string) string { return html.EscapeString(s) }

func fontAttrs(st textStyle) string {
	family := "Geist PI, Geist, sans-serif"
	if st.mono {
		family = "Geist Mono PI, Geist Mono, monospace"
	}
	a := fmt.Sprintf(`font-family="%s" font-size="%g" font-weight="%d"`, family, st.size, st.weight)
	if st.spacing != 0 {
		a += fmt.Sprintf(` letter-spacing="%g"`, st.spacing)
	}
	return a
}

func (w *svgWriter) text(x, y float64, st textStyle, fill, s, anchor string) {
	if st.upper {
		s = strings.ToUpper(s)
	}
	if anchor == "" {
		anchor = "start"
	}
	w.f(`<text xml:space="preserve" x="%.1f" y="%.1f" %s fill="%s" text-anchor="%s">%s</text>`, x, y, fontAttrs(st), fill, anchor, esc(s))
}

func (w *svgWriter) icon(name string, x, y, size float64, color string, stroke float64) {
	w.f(`<g transform="translate(%.1f %.1f) scale(%.4f)" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round">%s</g>`,
		x, y, size/24, color, stroke, icons[name])
}

type span struct {
	st   textStyle
	fill string
	s    string
}

func spansWidth(ss []span) float64 {
	var w float64
	for _, s := range ss {
		w += s.st.width(s.s)
	}
	return w
}

func (w *svgWriter) spans(x, y float64, ss []span, anchor string) {
	if anchor == "" {
		anchor = "start"
	}
	// tspan の端の空白が詰められないように、空白をそのまま残させる
	w.f(`<text xml:space="preserve" x="%.1f" y="%.1f" text-anchor="%s">`, x, y, anchor)
	for _, s := range ss {
		w.f(`<tspan %s fill="%s">%s</tspan>`, fontAttrs(s.st), s.fill, esc(s.s))
	}
	w.b.WriteString(`</text>`)
}

// layout は高さを返す。w が nil なら描かずに寸法だけ計る
func (c *card) layout(w *svgWriter) float64 {
	if w == nil {
		w = &svgWriter{}
	}
	const (
		line   = "#ececea"
		muted  = "#a1a1aa"
		subtle = "#71717a"
	)
	y := 14.0

	w.text(cardPad, y+9, stTag, "#52525b", c.tag, "")
	if c.hasShare {
		color, numColor, ic := "#a1a1aa", "#18181b", "gauge"
		switch c.heat {
		case heatHot:
			color, numColor, ic = "#dc2626", "#b91c1c", "flame"
		case heatWarm:
			color, numColor = "#ea8a0c", "#c2410c"
		}
		x := cardW - cardPad
		meter := 56.0
		w.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="5" rx="2.5" fill="#ececea"/>`, x-meter, y+3, meter)
		w.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="5" rx="2.5" fill="%s"/>`, x-meter, y+3, math.Max(meter*c.share, 2), color)
		x -= meter + 8
		pct := fmt.Sprintf("%.0f%%", c.share*100)
		ss := []span{{stSelf, subtle, "self "}, {stSelfNum, numColor, pct}}
		w.spans(x, y+10, ss, "end")
		x -= spansWidth(ss) + 5
		w.icon(ic, x-13, y-1.5, 13, color, 1.75)
	}
	y += 14 + 8

	w.f(`<rect x="%.1f" y="%.1f" width="36" height="36" rx="10" fill="%s" stroke="%s"/>`, cardPad, y, c.cat.tint, c.cat.ring)
	w.icon(c.icon, cardPad+8, y+8, 20, c.cat.color, 1.9)
	tx := cardPad + 48
	ny := y
	nameLine := c.nameSize * 1.08
	for i, l := range c.name {
		w.text(tx, ny+c.nameSize*0.86+float64(i)*nameLine, stName.withSize(c.nameSize), "#18181b", l, "")
	}
	ny += float64(len(c.name)) * nameLine
	for i, l := range c.target {
		if i == 0 {
			w.icon("table", tx, ny+4.5, 13, subtle, 1.75)
		}
		w.text(tx+18, ny+15+float64(i)*17, stTarget, subtle, l, "")
	}
	ny += float64(len(c.target)) * 17
	if len(c.target) > 0 {
		ny += 3
	}
	y = math.Max(y+36, ny) + 12

	run := func(title, icon string, body func(y float64) float64) {
		w.f(`<rect x="0" y="%.1f" width="%.0f" height="1" fill="%s"/>`, y, cardW, line)
		y0 := y + 11
		w.icon(icon, cardPad, y0-0.5, 11, muted, 2)
		w.text(cardPad+16, y0+8.5, stSecHead, muted, title, "")
		y = body(y0+16) + 10
	}

	condLines := func(lines []string) func(float64) float64 {
		return func(yy float64) float64 {
			for i, l := range lines {
				w.text(cardPad, yy+13+float64(i)*18, stCond, "#27272a", l, "")
			}
			return yy + float64(len(lines))*18
		}
	}
	if len(c.conds) > 0 {
		run("Condition", "filter", condLines(c.conds))
	}
	if len(c.keys) > 0 {
		run("Key", "sort", condLines(c.keys))
	}

	if c.big != "" || len(c.extras) > 0 {
		run("Rows", "rows", func(yy float64) float64 {
			end := yy
			if c.big != "" {
				w.text(cardPad-1, yy+33, stBig, "#09090b", c.big, "")
				bigW := stBig.width(c.big)
				// 右に Loops と見積もり。収まらなければ下に並べる
				var sub [][]span
				if c.loops != "" {
					unit := " loops"
					if c.loops == "1" {
						unit = " loop"
					}
					sub = append(sub, []span{{stSub, subtle, "× "}, {stSubNum, "#3f3f46", c.loops}, {stSub, subtle, unit}})
				}
				if c.plan != "" {
					sub = append(sub, []span{{stSub, subtle, "Plan Rows "}, {stSubNum, "#3f3f46", c.plan}})
				} else if c.loops == "" {
					sub = append(sub, []span{{stSub, subtle, "Plan Rows (est)"}})
				}
				badgeW := 0.0
				if c.est != nil {
					badgeW = stBadge.width(c.est.text) + 32
				}
				subW := 0.0
				for i, s := range sub {
					sw := spansWidth(s)
					if i == len(sub)-1 {
						sw += badgeW
					}
					subW = math.Max(subW, sw)
				}
				x := cardW - cardPad
				sy := yy + 14
				if bigW+16+subW > innerW {
					sy = yy + 50
					end = sy + float64(len(sub))*16
				} else {
					end = yy + 38
				}
				for i, s := range sub {
					lx := x
					if i == len(sub)-1 && c.est != nil {
						bw := badgeW - 6
						w.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="17" rx="5" fill="%s"/>`, x-bw, sy+float64(i)*16-12, bw, c.est.bg)
						w.icon(c.est.icon, x-bw+6, sy+float64(i)*16-9.5, 11, c.est.fg, 2.2)
						w.text(x-6, sy+float64(i)*16, stBadge, c.est.fg, c.est.text, "end")
						lx -= badgeW
					}
					w.spans(lx, sy+float64(i)*16, s, "end")
				}
				end = math.Max(end, sy+float64(len(sub)-1)*16+6)
			}
			for _, e := range c.extras {
				end += 16
				w.spans(cardPad, end, []span{{stSub, subtle, e[0] + ": "}, {stSubNum, "#3f3f46", e[1]}}, "")
			}
			return end + 2
		})
	}

	if len(c.detail) > 0 {
		run("Details", "details", func(yy float64) float64 {
			kw := 0.0
			for _, d := range c.detail {
				kw = math.Max(kw, stKey.width(d[0]))
			}
			kw = math.Min(kw, 150)
			vx := cardPad + kw + 14
			vw := cardW - cardPad - vx
			cy := yy
			for _, d := range c.detail {
				keys := stKey.wrap(d[0], kw)
				vals := stVal.wrap(d[1], vw)
				for i, k := range keys {
					w.text(cardPad, cy+11+float64(i)*14.5, stKey, muted, k, "")
				}
				for i, v := range vals {
					w.text(vx, cy+11+float64(i)*14.5, stVal, "#3f3f46", v, "")
				}
				cy += float64(max(len(keys), len(vals))) * 14.5
			}
			return cy
		})
	}
	return y + 4
}

func (c *card) draw(w *svgWriter, x, y float64) {
	w.f(`<g transform="translate(%.1f %.1f)">`, x, y)
	// 背景も白なので、輪郭が影だけにならないよう枠線を少し濃くする
	stroke, sw := "rgba(24,24,27,0.14)", 1.0
	switch c.heat {
	case heatHot:
		stroke, sw = "#dc2626", 2
	case heatWarm:
		stroke, sw = "#ea8a0c", 2
	}
	// 影は別の要素にして、影を描けない環境でもカードの地は残す
	w.f(`<rect width="%.0f" height="%.1f" rx="14" fill="#18181b" filter="url(#shadow)"/>`, cardW, c.h)
	w.f(`<rect width="%.0f" height="%.1f" rx="14" fill="#ffffff"/>`, cardW, c.h)
	inner := &svgWriter{}
	c.layout(inner)
	w.b.WriteString(inner.b.String())
	w.f(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="14" fill="none" stroke="%s" stroke-width="%.0f"/>`, sw/2, sw/2, cardW-sw, c.h-sw, stroke, sw)
	w.b.WriteString(`</g>`)
}
