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
    {"Node Type": "Index Only Scan", "Parent Relationship": "Outer", "Relation Name": "many", "Index Name": "many_x_y_idx",
     "Index Cond": "(x = 7)", "Actual Loops": 1, "Index Searches": 1},
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

func TestPlanToDot(t *testing.T) {
	plan, extras, err := parsePlanJSON(nestedPlanJSON)
	require.NoError(t, err)

	dot := planToDot(plan, extras)

	t.Run("親から2つの子へ矢印が出る", func(t *testing.T) {
		assert.Contains(t, dot, "n1 -> n2;")
		assert.Contains(t, dot, "n1 -> n3;")
	})
	t.Run("決め打ちの一覧になかったキーも出る", func(t *testing.T) {
		assert.Contains(t, dot, `Join Type: Inner\l`)
		assert.Contains(t, dot, `Parent Relationship: Inner\l`)
	})
	t.Run("数値は JSON に書かれたまま出る", func(t *testing.T) {
		assert.Contains(t, dot, `Actual Rows: 10.0\l`)
	})
	t.Run("キーは JSON の順番で出る", func(t *testing.T) {
		assert.Contains(t, dot, `Index Only Scan\lParent Relationship: Outer\lRelation Name: many\lIndex Name: many_x_y_idx\l`)
	})
	t.Run("計画の外の最上位のキーも出る", func(t *testing.T) {
		assert.Contains(t, dot, `summary [shape=note, label="Planning Time: 0.123\l"];`)
	})
}

func TestFormatValue(t *testing.T) {
	assert.Equal(t, "total DESC, u.name", formatValue([]any{"total DESC", "u.name"}))
	assert.Equal(t, "{Worker Number: 0, Actual Rows: 5}",
		formatValue(object{{"Worker Number", json.Number("0")}, {"Actual Rows", json.Number("5")}}))
	assert.Equal(t, "true", formatValue(true))
	assert.Equal(t, "null", formatValue(nil))
}

func TestDotLabel(t *testing.T) {
	assert.Equal(t, `Filter: (name = \"a\\b\")\l`, dotLabel([]string{`Filter: (name = "a\b")`}))
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
