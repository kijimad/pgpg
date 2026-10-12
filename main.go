package main

import (
	"fmt"
	"log"
	"os"

	"github.com/violet/pg-experiment/planimage"
	"github.com/violet/pg-experiment/server"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Println("Usage: pg-experiment serve")
		os.Exit(1)
	}

	// 環境変数から設定を取得
	backendDSN := getEnv("PG_BACKEND", "postgres://postgres@localhost/postgres")
	proxyHost := getEnv("PG_PROXY_HOST", "localhost")
	proxyPort := getEnv("PG_PROXY_PORT", "15432")
	// 応答の返し方。次のどれか1つ
	//   plain:      そのまま返す
	//   echo:       クエリを「> 」付きの行にして、結果の前に返す
	//   plan-image: EXPLAIN (FORMAT JSON) の結果を画像にして PG_PLAN_IMAGE_DIR に書き、リンクを返す
	mode := getEnv("PG_MODE", "plain")

	// バックエンドに接続
	log.Printf("Connecting to backend: %s", backendDSN)
	be, err := server.New(backendDSN)
	if err != nil {
		log.Fatalf("Failed to connect to backend: %v", err)
	}

	// プロキシサーバを起動
	srv := server.NewServer(proxyHost, proxyPort, be, mode == "echo")
	switch mode {
	case "plain", "echo":
	case "plan-image":
		dir := getEnv("PG_PLAN_IMAGE_DIR", "")
		if dir == "" {
			log.Fatalf("PG_MODE=plan-image requires PG_PLAN_IMAGE_DIR")
		}
		pi := &planimage.PlanImage{Dir: dir, LinkPrefix: getEnv("PG_PLAN_IMAGE_LINK_PREFIX", "images/")}
		srv.WithRewriter(func(query string) server.Rewriter { return pi.NewRewriter(query) })
	default:
		log.Fatalf("unknown PG_MODE %q: want plain, echo or plan-image", mode)
	}
	log.Printf("Mode: %s", mode)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	log.Printf("Proxy server listening on %s:%s", proxyHost, proxyPort)

	// サーバーを実行し続ける
	select {}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
