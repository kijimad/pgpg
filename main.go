package main

import (
	"fmt"
	"log"
	"os"

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
	echoQueries := getEnv("PG_ECHO_QUERIES", "") == "on"

	// バックエンドに接続
	log.Printf("Connecting to backend: %s", backendDSN)
	be, err := server.New(backendDSN)
	if err != nil {
		log.Fatalf("Failed to connect to backend: %v", err)
	}

	// プロキシサーバを起動
	srv := server.NewServer(proxyHost, proxyPort, be, echoQueries)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
