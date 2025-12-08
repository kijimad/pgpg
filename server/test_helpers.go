package server

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const (
	testUser        = "postgres"
	testHost        = "localhost"
	testBackendPort = "15433"
	testDB          = "postgres"
)

// formatDSN はPostgreSQLのDSN文字列を生成する
func formatDSN(port string) string {
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", testUser, testHost, port, testDB)
}

// startTestServer はテスト用サーバーを起動して、起動したポート番号を返す
func startTestServer(t *testing.T, echoQueries bool) (*Server, string) {
	t.Helper()
	be, err := New(formatDSN(testBackendPort))
	require.NoError(t, err)

	// ポート0で空いたポートを起動させる
	server := NewServer(testHost, "0", be, echoQueries)
	err = server.Start()
	require.NoError(t, err)

	port := fmt.Sprintf("%d", server.Port())
	return server, port
}

func connect(ctx context.Context, t *testing.T, port string) *pgx.Conn {
	config, _ := pgx.ParseConfig(formatDSN(port))
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return conn
}

// execPsql はpsqlコマンドを実行して出力を返す
func execPsql(port, query string) (string, error) {
	cmd := exec.Command("psql",
		"-h", "localhost",
		"-p", port,
		"-U", "postgres",
		"-d", "postgres",
		"-c", query,
		"-t", // タプルのみ（ヘッダーなし）
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
