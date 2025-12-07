package server

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func getTestDSN() string {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		dsn = "postgres://postgres@localhost/postgres?sslmode=disable"
	}
	return dsn
}

func connect(ctx context.Context, t *testing.T, port string) *pgx.Conn {
	dsn := fmt.Sprintf("postgres://postgres@localhost:%s/postgres?sslmode=disable", port)
	config, _ := pgx.ParseConfig(dsn)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return conn
}
