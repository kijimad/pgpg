package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_BasicQuery(t *testing.T) {
	_, port := startTestServer(t, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connect(ctx, t, port)
	defer conn.Close(ctx)

	var result int
	err := conn.QueryRow(ctx, "SELECT 1+1").Scan(&result)
	require.NoError(t, err)

	assert.Equal(t, 2, result)
}

func TestIntegration_CreateTable(t *testing.T) {
	_, port := startTestServer(t, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connect(ctx, t, port)
	defer conn.Close(ctx)

	// テーブル作成
	_, err := conn.Exec(ctx, "CREATE TABLE test_users (id INT, name TEXT)")
	require.NoError(t, err)

	// データ挿入
	_, err = conn.Exec(ctx, "INSERT INTO test_users VALUES (1, 'Alice'), (2, 'Bob')")
	require.NoError(t, err)

	// データ取得
	rows, err := conn.Query(ctx, "SELECT id, name FROM test_users ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int
		var name string
		err := rows.Scan(&id, &name)
		require.NoError(t, err)
		count++
	}

	assert.Equal(t, 2, count)
}

func TestIntegration_ExplainAnalyze(t *testing.T) {
	_, port := startTestServer(t, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connect(ctx, t, port)
	defer conn.Close(ctx)

	// テーブル作成
	_, err := conn.Exec(ctx, "CREATE TABLE users (id INT, name TEXT)")
	require.NoError(t, err)

	// データ挿入
	_, err = conn.Exec(ctx, "INSERT INTO users SELECT i, 'user' || i FROM generate_series(1, 1000) i")
	require.NoError(t, err)

	// EXPLAIN ANALYZE実行
	rows, err := conn.Query(ctx, "EXPLAIN ANALYZE SELECT * FROM users WHERE id = 500")
	require.NoError(t, err)
	defer rows.Close()

	hasRows := false
	for rows.Next() {
		hasRows = true
	}

	assert.True(t, hasRows, "Expected EXPLAIN ANALYZE output")
}

func TestIntegration_EchoQuery(t *testing.T) {
	_, port := startTestServer(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connect(ctx, t, port)
	defer conn.Close(ctx)

	// シンプルなクエリを実行
	// エコー機能により、セミコロン区切りの複数クエリが内部で実行される
	// pgx.Queryは最初の結果セット（エコー）を返す
	rows, err := conn.Query(ctx, "SELECT 42 AS answer")
	require.NoError(t, err)
	defer rows.Close()

	// 最初の結果セットはエコー出力
	hasRows := rows.Next()
	require.True(t, hasRows, "Expected echo row")

	var echoLine string
	err = rows.Scan(&echoLine)
	require.NoError(t, err)

	// エコー行は > で始まる
	assert.True(t, len(echoLine) > 0 && echoLine[0] == '>', "Echo line should start with >")
	assert.Contains(t, echoLine, "SELECT 42", "Echo should contain the query")
}

func TestIntegration_EchoExplainQuery(t *testing.T) {
	_, port := startTestServer(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connect(ctx, t, port)
	defer conn.Close(ctx)

	// テーブルを作成
	_, err := conn.Exec(ctx, "CREATE TABLE test_echo (id INT)")
	require.NoError(t, err)

	// EXPLAINクエリを実行
	rows, err := conn.Query(ctx, "EXPLAIN SELECT * FROM test_echo")
	require.NoError(t, err)
	defer rows.Close()

	// 最初の結果セットはエコー出力
	hasEcho := rows.Next()
	require.True(t, hasEcho, "Expected echo row")

	var echoLine string
	err = rows.Scan(&echoLine)
	require.NoError(t, err)

	// エコー行の検証
	assert.True(t, len(echoLine) > 0 && echoLine[0] == '>', "Echo line should start with >")
	assert.Contains(t, echoLine, "EXPLAIN", "Echo should contain EXPLAIN")
}

func TestIntegration_EchoOutputSnapshot(t *testing.T) {
	_, port := startTestServer(t, true)

	tests := []struct {
		name     string
		query    string
		expected string // 期待される出力の完全一致
	}{
		{
			name:  "simple select",
			query: "SELECT 'hello' AS greeting",
			expected: ` > SELECT 'hello' AS greeting

 hello

`,
		},
		{
			name:  "numeric query",
			query: "SELECT 42 AS answer",
			expected: ` > SELECT 42 AS answer

     42

`,
		},
		{
			name:  "multiline query",
			query: "SELECT 1 AS first,\n       2 AS second",
			expected: ` > SELECT 1 AS first,
 >        2 AS second

     1 |      2

`,
		},
		{
			name:  "japanese characters",
			query: "SELECT 'こんにちは' AS greeting",
			expected: ` > SELECT 'こんにちは' AS greeting

 こんにちは

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := execPsql(port, tt.query)
			require.NoError(t, err, "psql command failed: %s", result)

			// 完全一致を検証
			assert.Equal(t, tt.expected, result, "Output should match exactly")
		})
	}
}

func TestIntegration_EchoComplexSnapshot(t *testing.T) {
	_, port := startTestServer(t, true)

	// psqlで一連のクエリを実行して出力を確認
	query := `
CREATE TABLE users (name varchar(255), tel integer);
INSERT INTO users (name, tel) VALUES ('きじま', 100);
SELECT name, tel FROM users;
`

	result, err := execPsql(port, query)
	require.NoError(t, err, "psql command failed: %s", result)

	// 期待される出力（完全一致）
	// 注意: エコー行 "> " の末尾に空白が入る
	expected := ` > 
 > CREATE TABLE users (name varchar(255), tel integer);
 > INSERT INTO users (name, tel) VALUES ('きじま', 100);
 > SELECT name, tel FROM users;
 > 

CREATE TABLE
INSERT 0 1
 きじま | 100

`

	assert.Equal(t, expected, result, "Output should match exactly")
}
