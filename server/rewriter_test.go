package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tagRewriter struct{}

func (r tagRewriter) Rewrite(msg pgproto3.BackendMessage) []pgproto3.BackendMessage {
	if _, ok := msg.(*pgproto3.CommandComplete); ok {
		return []pgproto3.BackendMessage{&pgproto3.CommandComplete{CommandTag: []byte("REWRITTEN")}}
	}
	return []pgproto3.BackendMessage{msg}
}

func TestIntegration_Rewriter(t *testing.T) {
	be, err := New(formatDSN(testBackendPort))
	require.NoError(t, err)

	var queries []string
	srv := NewServer(testHost, "0", be, false).WithRewriter(func(query string) Rewriter {
		queries = append(queries, query)
		return tagRewriter{}
	})
	require.NoError(t, srv.Start())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := connect(ctx, t, fmt.Sprintf("%d", srv.Port()))
	defer conn.Close(ctx)

	tag, err := conn.Exec(ctx, "CREATE TABLE r (x int)")
	require.NoError(t, err)
	assert.Equal(t, "REWRITTEN", tag.String(), "応答は Rewriter を通って送られる")
	assert.Equal(t, []string{"CREATE TABLE r (x int)"}, queries, "Query ごとに Rewriter が作られる")
}
