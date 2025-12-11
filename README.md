org-babelで文芸的にデータベース実行するためのDBプロキシ。

- 少ない準備で気軽にデータベースを実行できる
- 文芸的プログラミングができる。入力と標準出力をテキストに残し、そのままドキュメント化できる
  - 入力クエリを出力にエコー表示できる
- 毎回クリーンな環境で、完全に再現できる
- org-mode org-babelの sql ブロックから直接使える
- じっさいのDBの機能を使える。インデックス、EXPLAIN、トランザクション...が使える

## 使い方

起動する。

```
docker compose up -d
```

org-mode側の見出しやファイルヘッダに書く。

```org-mode
:PROPERTIES:
  :header-args+: :results table
  :header-args+: :database postgres
  :header-args+: :dbhost localhost
  :header-args+: :dbport 15432
  :header-args+: :dbuser postgres
  :header-args+: :engine postgresql
:END:
```
