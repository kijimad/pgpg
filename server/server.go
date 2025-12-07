package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	host        string
	port        string
	backend     *PGClient
	echoQueries bool
}

func NewServer(host, port string, backend *PGClient, echoQueries bool) *Server {
	return &Server{
		host:        host,
		port:        port,
		backend:     backend,
		echoQueries: echoQueries,
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	defer listener.Close()

	log.Printf("Proxy server listening on %s", addr)
	log.Printf("Backend: %s", s.backend.ConnString())

	// シグナルハンドリング
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		listener.Close()
	}()

	// 接続を受け付ける
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			return err
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	ctx := context.Background()

	handler := NewConnectionHandler(conn, s.backend.ConnString(), s.echoQueries)
	handler.Handle(ctx,
		func(dbName string) error {
			return s.backend.CreateTempDB(ctx, dbName)
		},
		func(dbName string) error {
			return s.backend.DropTempDB(ctx, dbName)
		},
	)
}
