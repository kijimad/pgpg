package planimage

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nestedPlanJSON = `[{"Plan": {"Node Type": "Nested Loop", "Join Type": "Inner", "Actual Rows": 10.0, "Actual Loops": 1,
  "Plans": [
    {"Node Type": "Index Only Scan", "Parent Relationship": "Outer", "Parallel Aware": false, "Relation Name": "many", "Alias": "m",
     "Index Name": "many_x_y_idx", "Index Cond": "(x = 7)", "Filter": "(y < 'a&b')", "Actual Loops": 1, "Index Searches": 1},
    {"Node Type": "Index Only Scan", "Parent Relationship": "Inner", "Relation Name": "few", "Index Name": "few_x_y_idx",
     "Index Cond": "(y = m.y)", "Actual Loops": 10, "Index Searches": 119}
  ]},
  "Planning Time": 0.123}]`

func TestParsePlanJSON(t *testing.T) {
	t.Run("実行計画の JSON を読める", func(t *testing.T) {
		plan, extras, err := parsePlanJSON(nestedPlanJSON)
		require.NoError(t, err)
		nodeType, _ := plan.get("Node Type")
		assert.Equal(t, "Nested Loop", nodeType)
		require.Len(t, extras, 1)
		assert.Equal(t, "Planning Time", extras[0].Key)
	})

	cases := map[string]string{
		"JSON でない(テキスト形式の EXPLAIN)": "Seq Scan on t  (cost=0.00..1.00 rows=1 width=4)",
		"配列でない":             `{"Plan": {"Node Type": "Seq Scan"}}`,
		"空の配列":              `[]`,
		"Plan がない":          `[{"foo": 1}]`,
		"Node Type がない":     `[{"Plan": {"Relation Name": "t"}}]`,
		"後ろに余計なものがある":       `[{"Plan": {"Node Type": "Seq Scan"}}] []`,
		"途中で切れている":          `[{"Plan": {"Node Type": "Seq Scan"`,
		"要素がオブジェクトでない":      `[1]`,
		"Plan がオブジェクトでない":   `[{"Plan": 1}]`,
		"Node Type が文字列でない": `[{"Plan": {"Node Type": 1}}]`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parsePlanJSON(input)
			assert.ErrorIs(t, err, errNotJSONPlan)
		})
	}
}

const weighedPlanJSON = `[{"Plan": {"Node Type": "Hash Join", "Total Cost": 100, "Plan Rows": 10, "Actual Rows": 400, "Actual Loops": 1,
  "Plans": [
    {"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Total Cost": 60, "Plan Rows": 500, "Actual Rows": 1500, "Actual Loops": 2,
     "Parallel Aware": true},
    {"Node Type": "Hash", "Parent Relationship": "Inner", "Total Cost": 10, "Plan Rows": 5, "Actual Rows": 5, "Actual Loops": 1}
  ]}}]`

func TestCommas(t *testing.T) {
	assert.Equal(t, "0", commas(0))
	assert.Equal(t, "999", commas(999))
	assert.Equal(t, "300,000", commas(300000))
	assert.Equal(t, "1,234,567", commas(1234567))
}

func TestFormatValue(t *testing.T) {
	assert.Equal(t, "total DESC, u.name", formatValue([]any{"total DESC", "u.name"}))
	assert.Equal(t, "{Worker Number: 0, Actual Rows: 5}",
		formatValue(object{{"Worker Number", json.Number("0")}, {"Actual Rows", json.Number("5")}}))
	assert.Equal(t, "true", formatValue(true))
	assert.Equal(t, "null", formatValue(nil))
	assert.Equal(t, "[]", formatValue([]any{}))
}

func TestPlanImageName(t *testing.T) {
	a := planImageName("SELECT 1", 0)
	assert.Equal(t, a, planImageName("SELECT 1", 0), "同じクエリなら同じ名前")
	assert.NotEqual(t, a, planImageName("SELECT 2", 0), "違うクエリなら違う名前")
	assert.NotEqual(t, a, planImageName("SELECT 1", 1), "同じクエリの2つ目の計画は別の名前")
	assert.Regexp(t, `^plan-[0-9a-f]{12}\.svg$`, a)
}

func TestPlanImageRender(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("Graphviz の dot がないので飛ばす")
	}
	dir := t.TempDir()
	pi := &PlanImage{Dir: dir, LinkPrefix: "images/"}

	link, err := pi.Render(nestedPlanJSON, "SELECT 1", 0)
	require.NoError(t, err)

	name := planImageName("SELECT 1", 0)
	assert.Equal(t, "[[file:images/"+name+"]]", link)
	info, err := os.Stat(filepath.Join(dir, name))
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestPlanImageRenderError(t *testing.T) {
	t.Run("計画でなければ画像を書かない", func(t *testing.T) {
		dir := t.TempDir()
		pi := &PlanImage{Dir: dir, LinkPrefix: "images/"}
		_, err := pi.Render(`[]`, "SELECT 1", 0)
		assert.ErrorIs(t, err, errNotJSONPlan)
		entries, _ := os.ReadDir(dir)
		assert.Empty(t, entries)
	})
	t.Run("ディレクトリを作れなければエラー", func(t *testing.T) {
		// ファイルの下にはディレクトリを作れない
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o644))
		pi := &PlanImage{Dir: filepath.Join(file, "images"), LinkPrefix: "images/"}
		_, err := pi.Render(nestedPlanJSON, "SELECT 1", 0)
		assert.ErrorContains(t, err, "画像のディレクトリを作れません")
	})
}
