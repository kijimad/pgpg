package server

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type PGClient struct {
	connString string
}

func New(connString string) (*PGClient, error) {
	// 接続確認
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close(ctx)

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	return &PGClient{connString: connString}, nil
}

func (b *PGClient) CreateTempDB(ctx context.Context, dbName string) error {
	conn, err := pgx.Connect(ctx, b.connString)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}

	return nil
}

func (b *PGClient) DropTempDB(ctx context.Context, dbName string) error {
	conn, err := pgx.Connect(ctx, b.connString)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	// 強制切断してから削除
	_, err = conn.Exec(ctx, fmt.Sprintf(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, dbName))
	if err != nil {
		return fmt.Errorf("terminate connections: %w", err)
	}

	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	if err != nil {
		return fmt.Errorf("drop database: %w", err)
	}

	return nil
}

func (b *PGClient) ConnString() string {
	return b.connString
}
