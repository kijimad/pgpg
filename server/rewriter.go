package server

import "github.com/jackc/pgx/v5/pgproto3"

// Rewriter は、1つの Query の応答を、クライアントに送る前に差し替える。
// msg は次の Receive までしか有効でないので、溜めるならコピーする
type Rewriter interface {
	// Rewrite は、msg の代わりに送るメッセージを返す。空なら何も送らない
	Rewrite(msg pgproto3.BackendMessage) []pgproto3.BackendMessage
}

type RewriterFactory func(query string) Rewriter
