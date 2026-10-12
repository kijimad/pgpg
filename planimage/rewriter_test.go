package planimage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rowDesc(name string) *pgproto3.RowDescription {
	return &pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte(name)}}}
}

func dataRow(v string) *pgproto3.DataRow {
	return &pgproto3.DataRow{Values: [][]byte{[]byte(v)}}
}

func complete(tag string) *pgproto3.CommandComplete {
	return &pgproto3.CommandComplete{CommandTag: []byte(tag)}
}

func feed(r *Rewriter, msgs ...pgproto3.BackendMessage) []pgproto3.BackendMessage {
	var out []pgproto3.BackendMessage
	for _, m := range msgs {
		out = append(out, r.Rewrite(m)...)
	}
	return out
}

func requireDot(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("Graphviz の dot がないので飛ばす")
	}
}

func TestRewriter(t *testing.T) {
	t.Run("行を返さない結果はそのまま通す", func(t *testing.T) {
		r := (&PlanImage{Dir: t.TempDir()}).NewRewriter("CREATE TABLE t (x int)")
		msg := complete("CREATE TABLE")
		assert.Equal(t, []pgproto3.BackendMessage{msg}, feed(r, msg))
	})

	t.Run("計画の JSON は画像のリンクの1行になる", func(t *testing.T) {
		requireDot(t)
		dir := t.TempDir()
		r := (&PlanImage{Dir: dir, LinkPrefix: "images/"}).NewRewriter("q")
		out := feed(r, rowDesc("QUERY PLAN"), dataRow(nestedPlanJSON), complete("EXPLAIN"))

		require.Len(t, out, 3)
		name := planImageName("q", 0)
		assert.Equal(t, "[[file:images/"+name+"]]", string(out[1].(*pgproto3.DataRow).Values[0]))
		assert.Equal(t, "SELECT 1", string(out[2].(*pgproto3.CommandComplete).CommandTag))
		_, err := os.Stat(filepath.Join(dir, name))
		assert.NoError(t, err)
	})

	t.Run("同じ Query の2つ目の計画は別の名前になる", func(t *testing.T) {
		requireDot(t)
		r := (&PlanImage{Dir: t.TempDir(), LinkPrefix: "images/"}).NewRewriter("q")
		plan := []pgproto3.BackendMessage{rowDesc("QUERY PLAN"), dataRow(nestedPlanJSON), complete("EXPLAIN")}
		first := feed(r, plan...)
		second := feed(r, plan...)
		assert.Equal(t, "[[file:images/"+planImageName("q", 1)+"]]", string(second[1].(*pgproto3.DataRow).Values[0]))
		assert.NotEqual(t, first[1], second[1])
	})

	notPlans := map[string][]pgproto3.BackendMessage{
		"列名が QUERY PLAN でない": {rowDesc("?column?"), dataRow("1"), complete("SELECT 1")},
		"0行":                 {rowDesc("x"), complete("SELECT 0")},
		"複数行のテキスト形式の EXPLAIN": {rowDesc("QUERY PLAN"), dataRow("Seq Scan on t"), dataRow("  Filter: (x = 1)"), complete("EXPLAIN")},
		"1行のテキスト形式の EXPLAIN":  {rowDesc("QUERY PLAN"), dataRow("Seq Scan on t"), complete("EXPLAIN")},
	}
	for name, msgs := range notPlans {
		t.Run("計画でない結果は 0A000 のエラー: "+name, func(t *testing.T) {
			r := (&PlanImage{Dir: t.TempDir()}).NewRewriter("q")
			out := feed(r, msgs...)
			require.Len(t, out, 1)
			e := out[0].(*pgproto3.ErrorResponse)
			assert.Equal(t, "0A000", e.Code)
			assert.Contains(t, e.Message, "実行計画の JSON ではありません")
		})
	}

	t.Run("行の途中のエラーは、ためた行を捨ててそのまま流す", func(t *testing.T) {
		requireDot(t)
		r := (&PlanImage{Dir: t.TempDir()}).NewRewriter("q")
		errMsg := &pgproto3.ErrorResponse{Code: "22012"}
		out := feed(r, rowDesc("?column?"), dataRow("1"), errMsg)
		assert.Equal(t, []pgproto3.BackendMessage{errMsg}, out)

		out = feed(r, rowDesc("QUERY PLAN"), dataRow(nestedPlanJSON), complete("EXPLAIN"))
		assert.Len(t, out, 3)
	})

	t.Run("ためた行は元のバッファを書き換えられても変わらない", func(t *testing.T) {
		requireDot(t)
		r := (&PlanImage{Dir: t.TempDir()}).NewRewriter("q")
		buf := []byte(nestedPlanJSON)
		r.Rewrite(rowDesc("QUERY PLAN"))
		r.Rewrite(&pgproto3.DataRow{Values: [][]byte{buf}})
		// 次の Receive でバッファが使い回されたのを真似る
		for i := range buf {
			buf[i] = ' '
		}
		out := r.Rewrite(complete("EXPLAIN"))
		require.Len(t, out, 3)
		_, isRow := out[1].(*pgproto3.DataRow)
		assert.True(t, isRow, "コピーした JSON から画像を描けている")
	})
}
