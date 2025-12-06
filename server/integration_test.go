package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_BasicQuery(t *testing.T) {
	be, err := New("postgres://postgres@localhost:15433/postgres?sslmode=disable")
	require.NoError(t, err)

	server := NewServer("localhost", "15440", be)
	go server.Start()
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connectSimple(ctx, t, "15440")
	defer conn.Close(ctx)

	var result int
	err = conn.QueryRow(ctx, "SELECT 1+1").Scan(&result)
	require.NoError(t, err)

	assert.Equal(t, 2, result)
}

func TestIntegration_CreateTable(t *testing.T) {
	be, err := New("postgres://postgres@localhost:15433/postgres?sslmode=disable")
	require.NoError(t, err)

	server := NewServer("localhost", "15441", be)
	go server.Start()
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connectSimple(ctx, t, "15441")
	defer conn.Close(ctx)

	// テーブル作成
	_, err = conn.Exec(ctx, "CREATE TABLE test_users (id INT, name TEXT)")
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
	be, err := New("postgres://postgres@localhost:15433/postgres?sslmode=disable")
	require.NoError(t, err)

	server := NewServer("localhost", "15442", be)
	go server.Start()
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := connectSimple(ctx, t, "15442")
	defer conn.Close(ctx)

	// テーブル作成
	_, err = conn.Exec(ctx, "CREATE TABLE users (id INT, name TEXT)")
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
