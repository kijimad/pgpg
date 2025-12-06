package server

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyServer(t *testing.T) {
	// バックエンドを準備
	backendDSN := getTestDSN()
	be, err := New(backendDSN)
	require.NoError(t, err)

	// プロキシサーバを起動
	server := NewServer("localhost", "15433", be)
	go func() {
		if err := server.Start(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// サーバが起動するまで待機
	time.Sleep(500 * time.Millisecond)

	// プロキシ経由で接続
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, _ := pgx.ParseConfig("postgres://postgres@localhost:15433/postgres?sslmode=disable")
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// クエリを実行
	var result int
	err = conn.QueryRow(ctx, "SELECT 1+1").Scan(&result)
	require.NoError(t, err)

	assert.Equal(t, 2, result)
}

func TestProxyServerWithTable(t *testing.T) {
	// バックエンドを準備
	backendDSN := getTestDSN()
	be, err := New(backendDSN)
	require.NoError(t, err)

	// プロキシサーバを起動
	server := NewServer("localhost", "15434", be)
	go func() {
		if err := server.Start(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// サーバが起動するまで待機
	time.Sleep(500 * time.Millisecond)

	// プロキシ経由で接続
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, _ := pgx.ParseConfig("postgres://postgres@localhost:15434/postgres?sslmode=disable")
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// テーブルを作成
	_, err = conn.Exec(ctx, "CREATE TABLE test_users (id INT, name TEXT)")
	require.NoError(t, err)

	// データを挿入
	_, err = conn.Exec(ctx, "INSERT INTO test_users VALUES (1, 'Alice'), (2, 'Bob')")
	require.NoError(t, err)

	// データを取得
	rows, err := conn.Query(ctx, "SELECT id, name FROM test_users ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var users []struct {
		ID   int
		Name string
	}

	for rows.Next() {
		var u struct {
			ID   int
			Name string
		}
		err := rows.Scan(&u.ID, &u.Name)
		require.NoError(t, err)
		users = append(users, u)
	}

	assert.Len(t, users, 2)
	assert.Equal(t, "Alice", users[0].Name)
	assert.Equal(t, "Bob", users[1].Name)
}

func TestMultipleConnections(t *testing.T) {
	// バックエンドを準備
	backendDSN := getTestDSN()
	be, err := New(backendDSN)
	require.NoError(t, err)

	// プロキシサーバを起動
	server := NewServer("localhost", "15435", be)
	go func() {
		if err := server.Start(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// サーバが起動するまで待機
	time.Sleep(500 * time.Millisecond)

	// 複数の接続を同時に作成
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan bool, 3)
	errs := make(chan error, 3)

	for i := 0; i < 3; i++ {
		go func(id int) {
			config, _ := pgx.ParseConfig("postgres://postgres@localhost:15435/postgres?sslmode=disable")
			config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			conn, err := pgx.ConnectConfig(ctx, config)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close(ctx)

			// 各接続で独立したテーブルを作成
			_, err = conn.Exec(ctx, "CREATE TABLE test_table (id INT)")
			if err != nil {
				errs <- err
				return
			}

			_, err = conn.Exec(ctx, "INSERT INTO test_table VALUES (1)")
			if err != nil {
				errs <- err
				return
			}

			var count int
			err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
			if err != nil {
				errs <- err
				return
			}

			if count != 1 {
				errs <- err
				return
			}

			done <- true
		}(i)
	}

	// すべての接続が完了するまで待機
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			// OK
		case err := <-errs:
			t.Fatalf("Connection %d failed: %v", i, err)
		case <-time.After(20 * time.Second):
			t.Fatalf("Timeout waiting for connection %d", i)
		}
	}
}
