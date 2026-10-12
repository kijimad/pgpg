FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o pg-experiment .

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    graphviz \
 && rm -rf /var/lib/apt/lists/*

# root 以外で動かすと fontconfig のキャッシュを書けないため
ENV XDG_CACHE_HOME=/tmp

WORKDIR /

COPY --from=builder /app/pg-experiment .

CMD ["./pg-experiment", "serve"]
