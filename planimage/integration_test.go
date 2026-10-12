package planimage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/violet/pg-experiment/server"
)

const testBackendDSN = "postgres://postgres@localhost:15433/postgres?sslmode=disable"

func startProxy(ctx context.Context, t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	requireDot(t)
	be, err := server.New(testBackendDSN)
	require.NoError(t, err)

	dir := t.TempDir()
	pi := &PlanImage{Dir: dir, LinkPrefix: "images/"}
	srv := server.NewServer("localhost", "0", be, false).
		WithRewriter(func(query string) server.Rewriter { return pi.NewRewriter(query) })
	require.NoError(t, srv.Start())

	config, err := pgx.ParseConfig(fmt.Sprintf("postgres://postgres@localhost:%d/postgres?sslmode=disable", srv.Port()))
	require.NoError(t, err)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn, dir
}

// 本物のバックエンドがないと確かめられない動きだけを扱う
func TestIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, dir := startProxy(ctx, t)

	t.Run("1つのメッセージの DDL と複数の計画", func(t *testing.T) {
		query := "CREATE TABLE m (x int);\n" +
			"EXPLAIN (FORMAT JSON) SELECT * FROM m;\n" +
			"EXPLAIN (ANALYZE, FORMAT JSON) SELECT * FROM m WHERE x = 1"
		results, err := conn.PgConn().Exec(ctx, query).ReadAll()
		require.NoError(t, err)
		require.Len(t, results, 3)

		assert.Equal(t, "CREATE TABLE", results[0].CommandTag.String())
		for i, r := range results[1:] {
			require.Len(t, r.Rows, 1)
			name := planImageName(query, i)
			assert.Equal(t, "[[file:images/"+name+"]]", string(r.Rows[0][0]))
			_, err := os.Stat(filepath.Join(dir, name))
			assert.NoError(t, err)
		}
	})

	t.Run("行の途中のエラーは、そのまま返る", func(t *testing.T) {
		var n int
		err := conn.QueryRow(ctx, "SELECT 1 / (x - 5) FROM generate_series(1, 10) x").Scan(&n)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "22012", pgErr.Code) // division_by_zero
	})

	t.Run("計画でない結果をエラーにしても、後ろの文は実行される", func(t *testing.T) {
		// 割り切っている動きを固定する
		err := conn.PgConn().Exec(ctx, "SELECT 1; CREATE TABLE after_err (x int)").Close()
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "0A000", pgErr.Code)

		err = conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON) SELECT * FROM after_err").Scan(new(string))
		assert.NoError(t, err, "後ろの CREATE TABLE が実行されている")
	})
}
