package planimage

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextWidth(t *testing.T) {
	regular := textStyle{weight: 400, size: 20}
	bold := textStyle{weight: 800, size: 20}
	assert.Greater(t, bold.width("Hash Join"), regular.width("Hash Join"), "太いほど幅が広い")

	mono := textStyle{mono: true, weight: 400, size: 10}
	assert.InDelta(t, 30.0, mono.width("abcde"), 0.01, "Geist Mono は 1 文字 0.6em")

	spaced := textStyle{mono: true, weight: 400, size: 10, spacing: 2, upper: true}
	assert.InDelta(t, 30.0+10, spaced.width("abcde"), 0.01, "字間は文字ごとに足す")
}

func TestWrap(t *testing.T) {
	st := textStyle{mono: true, weight: 400, size: 10} // 1 文字 6px
	text := "Hash Cond: (orders.user_id = users.id) AND (orders.created_at > now())"
	lines := st.wrap(text, 120)
	for _, l := range lines {
		assert.LessOrEqual(t, st.width(l), 120.0)
	}
	assert.Equal(t, strings.ReplaceAll(text, " ", ""), strings.ReplaceAll(strings.Join(lines, ""), " ", ""), "折り返しで文字を落とさない")

	long := st.wrap("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 60)
	assert.Equal(t, []string{"aaaaaaaaaa", "aaaaaaaaaa", "aaaaaaaaaa"}, long, "1 語が収まらなければ途中で折る")
}

func TestWrapJapanese(t *testing.T) {
	// 埋め込んだフォントにない字形でも、文字を落とさずに折り返す
	st := textStyle{mono: true, weight: 500, size: 13}
	text := "Filter: (name = '実行計画を図にするための長い日本語の文字列')"
	lines := st.wrap(text, 160)
	assert.Greater(t, len(lines), 1)
	assert.Equal(t, strings.ReplaceAll(text, " ", ""), strings.ReplaceAll(strings.Join(lines, ""), " ", ""))
}

func TestMeasureConcurrently(t *testing.T) {
	// 接続ごとの goroutine から同時に描いても、go-text の Face と shaper を取り合わない(-race で確かめる)
	want := textStyle{weight: 800, size: 26}.width("Hash Join")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			textStyle{weight: w, size: 13}.width("Seq Scan on items")
			assert.Equal(t, want, textStyle{weight: 800, size: 26}.width("Hash Join"))
		}(200 + i*100)
	}
	wg.Wait()
}

func TestNewCard(t *testing.T) {
	plan, _, err := parsePlanJSON(nestedPlanJSON)
	require.NoError(t, err)
	outer := children(plan)[0]
	c := newCard("n2", outer, 0, false)

	assert.Equal(t, "Scan", c.tag)
	assert.Equal(t, []string{"using many_x_y_idx on many m"}, c.target)
	assert.Equal(t, []string{"Index Cond: (x = 7)", "Filter: (y < 'a&b')"}, c.conds, "条件は CONDITION に")
	assert.Equal(t, [][2]string{{"Index Searches", "1"}}, c.extras, "行数にかかわるキーは ROWS に")
	assert.Equal(t, [][2]string{{"Parallel Aware", "false"}}, c.detail, "そのほかは DETAILS に")
	assert.Empty(t, c.big, "Actual Rows も Plan Rows もなければ行数は出さない")
}

func TestHeatAndEstimate(t *testing.T) {
	plan, _, err := parsePlanJSON(weighedPlanJSON)
	require.NoError(t, err)
	root := weight(plan, false)
	share := func(p object) float64 {
		s, _ := selfShare(p, root, false)
		return s
	}
	cs := children(plan)

	join := newCard("n1", plan, share(plan), true)
	scan := newCard("n2", cs[0], share(cs[0]), true)
	hash := newCard("n3", cs[1], share(cs[1]), true)

	// 自身の重さは子の分を引く: 100 - 60 - 10 = 30
	assert.InDelta(t, 0.3, join.share, 1e-9)
	assert.Equal(t, heatHot, join.heat)
	assert.Equal(t, heatHot, scan.heat)
	assert.Equal(t, heatWarm, hash.heat)

	assert.Equal(t, "est ×40", join.est.text, "10 行の見積もりに 400 行")
	assert.Equal(t, "est ×3.0", scan.est.text, "500 行の見積もりに 1500 行")
	assert.Equal(t, "est", hash.est.text, "5 行の見積もりに 5 行")
}

// svgText は、SVG の文字の中身をつないで返す。埋め込んだフォント(<defs>)は除く
func svgText(svg string) string {
	if a, b := strings.Index(svg, "<defs>"), strings.Index(svg, "</defs>"); a >= 0 && b > a {
		svg = svg[:a] + svg[b:]
	}
	re := regexp.MustCompile(`>([^<>]*)<`)
	var b strings.Builder
	for _, m := range re.FindAllStringSubmatch(svg, -1) {
		b.WriteString(html.UnescapeString(m[1]))
	}
	return b.String()
}

// ANALYZE なしの計画。子が1つ、名前が長い、KEY の区画がある、日本語の値がある
const estimatedPlanJSON = `[{"Plan": {"Node Type": "Incremental Sort", "Total Cost": 50, "Plan Rows": 100,
  "Sort Key": ["a", "b"], "Presorted Key": ["a"],
  "Plans": [
    {"Node Type": "Bitmap Heap Scan", "Parent Relationship": "Outer", "Relation Name": "t", "Total Cost": 40, "Plan Rows": 100,
     "Recheck Cond": "(a > 1)", "Filter": "(name = '日本')"}
  ]}}]`

// PostgreSQL 18 で EXPLAIN (ANALYZE, FORMAT JSON) を既定の設定のまま取ったもの。BUFFERS が付き、Planning がオブジェクトになる
const defaultPlanJSON = `[{"Plan": {"Node Type": "Seq Scan", "Parallel Aware": false, "Async Capable": false,
  "Relation Name": "z", "Alias": "z", "Startup Cost": 0.00, "Total Cost": 17.50, "Plan Rows": 9, "Plan Width": 4,
  "Actual Startup Time": 0.004, "Actual Total Time": 0.033, "Actual Rows": 9.00, "Actual Loops": 1, "Disabled": false,
  "Filter": "(x < 10)", "Rows Removed by Filter": 991,
  "Shared Hit Blocks": 0, "Shared Read Blocks": 0, "Shared Dirtied Blocks": 0, "Shared Written Blocks": 0,
  "Local Hit Blocks": 5, "Local Read Blocks": 0, "Local Dirtied Blocks": 0, "Local Written Blocks": 0,
  "Temp Read Blocks": 0, "Temp Written Blocks": 0},
  "Planning": {"Shared Hit Blocks": 5, "Shared Read Blocks": 0, "Shared Dirtied Blocks": 0, "Shared Written Blocks": 0,
    "Local Hit Blocks": 0, "Local Read Blocks": 0, "Local Dirtied Blocks": 0, "Local Written Blocks": 0,
    "Temp Read Blocks": 0, "Temp Written Blocks": 0},
  "Planning Time": 0.021, "Triggers": [], "Execution Time": 0.037}]`

func TestSummaryFitsWidth(t *testing.T) {
	_, extras, err := parsePlanJSON(defaultPlanJSON)
	require.NoError(t, err)
	const width = 360.0
	items, h := layoutSummary(summarize(extras), width)
	require.Len(t, items, 4)
	for _, it := range items {
		for _, line := range it.lines {
			assert.LessOrEqual(t, it.x+summaryHead(it.label)+spansWidth(line), width, "%s が帯の幅を超えない", it.label)
		}
	}
	assert.Greater(t, len(items[0].lines), 1, "オブジェクトの値は折り返す")
	assert.Greater(t, h, float64(len(items[0].lines))*summaryLine)

	long := summarize(object{{"Triggers", []any{object{
		{"Trigger Name", "a_very_long_trigger_name_for_auditing_every_change"}, {"Relation", "orders"},
		{"Time", json.Number("1.5")}, {"Calls", json.Number("3")},
	}}}})
	items, _ = layoutSummary(long, 200)
	assert.Greater(t, len(items[0].lines), 1, "1行に収まらない値は折り返す")
}

func TestClassify(t *testing.T) {
	cases := map[string][2]string{
		"Seq Scan":         {"Scan", "seqscan"},
		"Index Only Scan":  {"Scan", "indexscan"},
		"Bitmap Heap Scan": {"Scan", "bitmap"},
		"CTE Scan":         {"Scan", "seqscan"},
		"Nested Loop":      {"Join", "nestedloop"},
		"Hash Join":        {"Join", "hashjoin"},
		"Merge Join":       {"Join", "mergejoin"},
		"Hash":             {"Support", "hash"},
		"Memoize":          {"Support", "materialize"},
		"Incremental Sort": {"Transform", "sort"},
		"Aggregate":        {"Transform", "aggregate"},
		"WindowAgg":        {"Transform", "aggregate"},
		"Limit":            {"Transform", "limit"},
		"Gather Merge":     {"Parallel", "gather"},
		"Append":           {"Support", "append"},
		"ModifyTable":      {"Modify", "modify"},
		"Result":           {"Support", "node"},
	}
	for nodeType, want := range cases {
		cat, icon := classify(nodeType)
		assert.Equal(t, want[0], cat.name, nodeType)
		assert.Equal(t, want[1], icon, nodeType)
		assert.NotEmpty(t, icons[icon], "アイコン %s が描ける", icon)
	}
}

func TestLongName(t *testing.T) {
	c := newCard("n1", object{{"Node Type", "Parallel Custom Scan With A Very Long Provider Name"}}, 0, false)
	assert.Less(t, c.nameSize, stName.size, "収まらなければ小さくする")
	assert.GreaterOrEqual(t, c.nameSize, 18.0)
	assert.Greater(t, len(c.name), 1, "それでも収まらなければ折り返す")
	for _, l := range c.name {
		assert.LessOrEqual(t, stName.withSize(c.nameSize).width(l), innerW-48)
	}
}

func TestRenderSVG(t *testing.T) {
	requireDot(t)

	for name, input := range map[string]string{"nested": nestedPlanJSON, "weighed": weighedPlanJSON, "estimated": estimatedPlanJSON, "default": defaultPlanJSON} {
		t.Run(name+": JSON のキーと値を1つも落とさない", func(t *testing.T) {
			plan, extras, err := parsePlanJSON(input)
			require.NoError(t, err)
			svg, err := renderSVG(plan, extras)
			require.NoError(t, err)
			text := strings.ReplaceAll(svgText(svg), " ", "")

			var check func(p object)
			check = func(p object) {
				for _, f := range p {
					if f.Key == "Plans" {
						continue
					}
					if !headerKeys[f.Key] {
						assert.Contains(t, text, strings.ReplaceAll(f.Key, " ", ""), "キー %s", f.Key)
					}
					assert.Contains(t, text, strings.ReplaceAll(formatValue(f.Value), " ", ""), "%s の値", f.Key)
				}
				for _, c := range children(p) {
					check(c)
				}
			}
			check(plan)
			for _, it := range summarize(extras) {
				if it.fields == nil {
					assert.Contains(t, text, strings.ReplaceAll(strings.ToUpper(it.label)+it.value, " ", ""))
					continue
				}
				assert.Contains(t, text, strings.ToUpper(it.label))
				for _, f := range it.fields {
					assert.Contains(t, text, strings.ReplaceAll(f[0]+f[1], " ", ""), "%s の %s", it.label, f[0])
				}
			}
		})
	}

	plan, extras, err := parsePlanJSON(weighedPlanJSON)
	require.NoError(t, err)
	svg, err := renderSVG(plan, extras)
	require.NoError(t, err)

	t.Run("フォントを埋め込む", func(t *testing.T) {
		assert.Equal(t, 2, strings.Count(svg, "data:font/woff2;base64,"))
	})
	t.Run("矢印のラベルに Loops を掛けた行数を出す", func(t *testing.T) {
		assert.Contains(t, svgText(svg), "Outer · 3,000 rows")
		assert.Contains(t, svgText(svg), "Inner · 5 rows")
	})
	t.Run("一番多い流れを青く太くする", func(t *testing.T) {
		assert.Regexp(t, `stroke="#2563eb" stroke-width="6\.0"`, svg)
	})
	t.Run("同じ段のカードは上端がそろう", func(t *testing.T) {
		re := regexp.MustCompile(`<g transform="translate\(([\d.]+) ([\d.]+)\)">`)
		ms := re.FindAllStringSubmatch(svg, -1)
		require.Len(t, ms, 3)
		assert.Equal(t, ms[1][2], ms[2][2], "Seq Scan と Hash")
	})
	t.Run("計画の外のキーは、図の上の帯にキーごとの形で出す", func(t *testing.T) {
		items := summarize(object{
			{"Planning Time", json.Number("0.123")},
			{"Triggers", []any{}},
		})
		require.Len(t, items, 2)
		assert.Equal(t, "0.123 ms", items[0].value)
		assert.Equal(t, "clock", items[0].icon)
		assert.Equal(t, "none", items[1].value, "動いたトリガーがない")
		assert.Equal(t, "bolt", items[1].icon)

		fired := summarize(object{{"Triggers", []any{object{
			{"Trigger Name", "audit"}, {"Relation", "orders"}, {"Time", json.Number("1.5")}, {"Calls", json.Number("3")},
		}, object{
			{"Trigger Name", "RI_ConstraintTrigger_c_1"}, {"Constraint Name", "orders_user_fk"}, {"Relation", "orders"}, {"Calls", json.Number("3")},
		}}}})
		assert.Equal(t, "audit on orders · 1.5 ms · calls 3 / RI_ConstraintTrigger_c_1 (orders_user_fk) on orders · calls 3", fired[0].value)
	})
	t.Run("ANALYZE なしの計画は、見積もりの行数を出し、流れを破線にする", func(t *testing.T) {
		p, e, err := parsePlanJSON(estimatedPlanJSON)
		require.NoError(t, err)
		s, err := renderSVG(p, e)
		require.NoError(t, err)
		text := svgText(s)
		assert.Contains(t, text, "Outer · 100 rows (est)")
		assert.Contains(t, text, "Plan Rows (est)")
		assert.Contains(t, s, `stroke-dasharray="5 4"`)
		assert.NotContains(t, text, "est ×", "実際の行数がなければ見積もりのずれは出さない")
		assert.Contains(t, text, "KEY")
		assert.Contains(t, text, "Sort Key: a, b")
		assert.Contains(t, text, "Filter: (name = '日本')")
		assert.Regexp(t, `<path d="M[\d.]+ [\d.]+V[\d.]+" fill="none"`, s, "子が1つなら真っすぐの線")
	})
	t.Run("ノードが1つなら重さを出さない", func(t *testing.T) {
		p, e, err := parsePlanJSON(defaultPlanJSON)
		require.NoError(t, err)
		s, err := renderSVG(p, e)
		require.NoError(t, err)
		assert.NotContains(t, svgText(s), "self")
		assert.NotContains(t, s, `stroke="#dc2626"`)
	})
	t.Run("HTML の特殊文字を逃がす", func(t *testing.T) {
		p, e, err := parsePlanJSON(nestedPlanJSON)
		require.NoError(t, err)
		s, err := renderSVG(p, e)
		require.NoError(t, err)
		assert.Contains(t, s, "Filter: (y &lt; &#39;a&amp;b&#39;)")
	})
}
