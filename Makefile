.DEFAULT_GOAL := help

.PHONY: test
test: ## テストを実行。docker compose up が必要
	TEST_PG_DSN=postgres://postgres@localhost:15433/postgres go test -v ./...

.PHONY: build
build: ## ビルド
	go build -o pg-experiment .

.PHONY: help
help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
