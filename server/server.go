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
	newRewriter RewriterFactory
	listener    net.Listener
}

func NewServer(host, port string, backend *PGClient, echoQueries bool) *Server {
	return &Server{
		host:        host,
		port:        port,
		backend:     backend,
		echoQueries: echoQueries,
	}
}

func (s *Server) WithRewriter(f RewriterFactory) *Server {
	s.newRewriter = f
	return s
}

// サーバを開始する。ブロックしない
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	s.listener = listener

	// シグナルハンドリング
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		listener.Close()
	}()

	// 接続を受け付ける
	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Accept error: %v", err)
				return
			}

			go s.handleConnection(conn)
		}
	}()

	return nil
}

func (s *Server) handleConnection(conn net.Conn) {
	ctx := context.Background()

	handler := NewConnectionHandler(conn, s.backend.ConnString(), s.echoQueries, s.newRewriter)
	handler.Handle(ctx,
		func(dbName string) error {
			return s.backend.CreateTempDB(ctx, dbName)
		},
		func(dbName string) error {
			return s.backend.DropTempDB(ctx, dbName)
		},
	)
}

// Port returns the actual port number the server is listening on
func (s *Server) Port() int {
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}
