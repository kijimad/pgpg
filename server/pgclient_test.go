package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	dsn := formatDSN(testBackendPort)
	client, err := New(dsn)
	require.NoError(t, err)

	assert.NotEmpty(t, client.ConnString())
}

func TestCreateAndDropTempDB(t *testing.T) {
	dsn := formatDSN(testBackendPort)
	client, err := New(dsn)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 一時DBを作成
	dbName := "test_temp_db_12345"
	err = client.CreateTempDB(ctx, dbName)
	assert.NoError(t, err)

	// 一時DBを削除
	err = client.DropTempDB(ctx, dbName)
	assert.NoError(t, err)
}

func TestDropTempDBWithActiveConnections(t *testing.T) {
	dsn := formatDSN(testBackendPort)
	client, err := New(dsn)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 一時DBを作成
	dbName := "test_temp_db_active_67890"
	err = client.CreateTempDB(ctx, dbName)
	require.NoError(t, err)

	// アクティブな接続がある状態でも削除できることを確認
	err = client.DropTempDB(ctx, dbName)
	assert.NoError(t, err)
}

func TestDropNonexistentDB(t *testing.T) {
	dsn := formatDSN(testBackendPort)
	client, err := New(dsn)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 存在しないDBを削除してもエラーにならないことを確認
	err = client.DropTempDB(ctx, "nonexistent_db_99999")
	assert.NoError(t, err)
}
