FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o pg-experiment .

FROM gcr.io/distroless/static-debian12

WORKDIR /

COPY --from=builder /app/pg-experiment .

EXPOSE 15432

CMD ["./pg-experiment", "serve"]
