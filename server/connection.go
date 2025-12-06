package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

type SimpleConnectionHandler struct {
	clientConn net.Conn
	tempDB     string
	backendDSN string
}

func NewSimpleConnectionHandler(clientConn net.Conn, backendDSN string) *SimpleConnectionHandler {
	return &SimpleConnectionHandler{
		clientConn: clientConn,
		backendDSN: backendDSN,
	}
}

func (h *SimpleConnectionHandler) Handle(ctx context.Context, createDB, dropDB func(string) error) {
	defer h.clientConn.Close()

	// 一時DB名を生成
	h.tempDB = generateTempDBName()
	log.Printf("Creating temp database: %s", h.tempDB)

	// 一時DBを作成
	if err := createDB(h.tempDB); err != nil {
		log.Printf("Failed to create temp database: %v", err)
		h.sendError("Failed to create temporary database")
		return
	}

	// 終了時に一時DBを削除
	defer func() {
		log.Printf("Dropping temp database: %s", h.tempDB)
		cleanupCtx := context.Background()
		if err := dropDB(h.tempDB); err != nil {
			log.Printf("Failed to drop temp database: %v", err)
		}
		_ = cleanupCtx
	}()

	// PostgreSQLバックエンドに接続（一時DBを使用）
	tempDSN := replaceDatabaseInDSN(h.backendDSN, h.tempDB)
	config, err := pgx.ParseConfig(tempDSN)
	if err != nil {
		log.Printf("Failed to parse DSN: %v", err)
		h.sendError(fmt.Sprintf("Failed to parse DSN: %v", err))
		return
	}
	config.RuntimeParams["standard_conforming_strings"] = "on"

	backendConn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		log.Printf("Failed to connect to backend: %v", err)
		h.sendError(fmt.Sprintf("Failed to connect to backend: %v", err))
		return
	}
	defer backendConn.Close(ctx)

	// クライアントのリクエストを処理
	if err := h.handleClient(ctx, backendConn); err != nil {
		log.Printf("Client handling error: %v", err)
	}
}

func (h *SimpleConnectionHandler) handleClient(ctx context.Context, backend *pgx.Conn) error {
	clientBackend := pgproto3.NewBackend(h.clientConn, h.clientConn)

	// スタートアップメッセージを受信
	startupMsg, err := clientBackend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("receive startup: %w", err)
	}

	log.Printf("Client connected, startup: %T", startupMsg)

	// SSLRequestの場合は拒否
	if _, ok := startupMsg.(*pgproto3.SSLRequest); ok {
		// SSLを拒否 ('N' を送信)
		_, err := h.clientConn.Write([]byte{'N'})
		if err != nil {
			return fmt.Errorf("failed to send SSL rejection: %w", err)
		}

		// 再度スタートアップメッセージを受信
		startupMsg, err = clientBackend.ReceiveStartupMessage()
		if err != nil {
			return fmt.Errorf("receive startup after SSL: %w", err)
		}
		log.Printf("Received startup after SSL rejection: %T", startupMsg)
	}

	// StartupMessageの確認
	if _, ok := startupMsg.(*pgproto3.StartupMessage); !ok {
		return fmt.Errorf("unexpected startup message type: %T", startupMsg)
	}

	// 認証成功を送信
	buf := []byte{
		'R',        // Authentication
		0, 0, 0, 8, // length
		0, 0, 0, 0, // AuthenticationOk
	}
	_, err = h.clientConn.Write(buf)
	if err != nil {
		return fmt.Errorf("send auth ok: %w", err)
	}

	// ParameterStatus を送信
	params := map[string]string{
		"server_version":             "16.0",
		"server_encoding":            "UTF8",
		"client_encoding":            "UTF8",
		"application_name":           "",
		"is_superuser":               "on",
		"session_authorization":      "postgres",
		"DateStyle":                  "ISO, MDY",
		"IntervalStyle":              "postgres",
		"TimeZone":                   "UTC",
		"integer_datetimes":          "on",
		"standard_conforming_strings": "on",
	}

	for key, value := range params {
		paramMsg := append([]byte(key), 0)
		paramMsg = append(paramMsg, []byte(value)...)
		paramMsg = append(paramMsg, 0)

		paramBuf := make([]byte, 5+len(paramMsg))
		paramBuf[0] = 'S' // ParameterStatus
		paramLen := 4 + len(paramMsg)
		paramBuf[1] = byte(paramLen >> 24)
		paramBuf[2] = byte(paramLen >> 16)
		paramBuf[3] = byte(paramLen >> 8)
		paramBuf[4] = byte(paramLen)
		copy(paramBuf[5:], paramMsg)

		_, err = h.clientConn.Write(paramBuf)
		if err != nil {
			return fmt.Errorf("send parameter status: %w", err)
		}
	}

	// ReadyForQuery を送信
	readyBuf := []byte{
		'Z',        // ReadyForQuery
		0, 0, 0, 5, // length
		'I', // status (Idle)
	}
	_, err = h.clientConn.Write(readyBuf)
	if err != nil {
		return fmt.Errorf("send ready: %w", err)
	}

	// メッセージループ
	for {
		msg, err := clientBackend.Receive()
		if err != nil {
			return fmt.Errorf("receive from client: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.Query:
			// クエリを実行
			if err := h.executeQuery(ctx, backend, m.String); err != nil {
				return err
			}

		case *pgproto3.Terminate:
			log.Printf("Client terminated connection")
			return nil

		default:
			log.Printf("Unhandled message type: %T", m)
		}
	}
}

func (h *SimpleConnectionHandler) executeQuery(ctx context.Context, backend *pgx.Conn, query string) error {
	rows, err := backend.Query(ctx, query)
	if err != nil {
		return h.sendQueryError(err)
	}
	defer rows.Close()

	// RowDescription を送信
	fieldDescs := rows.FieldDescriptions()
	rowDescBuf := make([]byte, 0, 1024)
	rowDescBuf = append(rowDescBuf, 'T') // RowDescription
	rowDescBuf = append(rowDescBuf, 0, 0, 0, 0) // length placeholder

	// フィールド数
	rowDescBuf = append(rowDescBuf, byte(len(fieldDescs)>>8), byte(len(fieldDescs)))

	for _, fd := range fieldDescs {
		// フィールド名
		rowDescBuf = append(rowDescBuf, []byte(fd.Name)...)
		rowDescBuf = append(rowDescBuf, 0) // null terminator

		// テーブルOID
		tableOID := fd.TableOID
		rowDescBuf = append(rowDescBuf, byte(tableOID>>24), byte(tableOID>>16), byte(tableOID>>8), byte(tableOID))

		// Column番号
		colNum := fd.TableAttributeNumber
		rowDescBuf = append(rowDescBuf, byte(colNum>>8), byte(colNum))

		// データ型OID
		dataTypeOID := fd.DataTypeOID
		rowDescBuf = append(rowDescBuf, byte(dataTypeOID>>24), byte(dataTypeOID>>16), byte(dataTypeOID>>8), byte(dataTypeOID))

		// データ型サイズ
		dataSize := fd.DataTypeSize
		rowDescBuf = append(rowDescBuf, byte(dataSize>>8), byte(dataSize))

		// Type modifier
		typeMod := fd.TypeModifier
		rowDescBuf = append(rowDescBuf, byte(typeMod>>24), byte(typeMod>>16), byte(typeMod>>8), byte(typeMod))

		// Format code (0 = text)
		rowDescBuf = append(rowDescBuf, 0, 0)
	}

	// length を設定
	length := len(rowDescBuf) - 1
	rowDescBuf[1] = byte(length >> 24)
	rowDescBuf[2] = byte(length >> 16)
	rowDescBuf[3] = byte(length >> 8)
	rowDescBuf[4] = byte(length)

	if _, err := h.clientConn.Write(rowDescBuf); err != nil {
		return fmt.Errorf("send row description: %w", err)
	}

	// DataRow を送信
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return fmt.Errorf("get row values: %w", err)
		}

		dataRowBuf := make([]byte, 0, 1024)
		dataRowBuf = append(dataRowBuf, 'D') // DataRow
		dataRowBuf = append(dataRowBuf, 0, 0, 0, 0) // length placeholder

		// カラム数
		dataRowBuf = append(dataRowBuf, byte(len(values)>>8), byte(len(values)))

		for _, val := range values {
			if val == nil {
				// NULL
				dataRowBuf = append(dataRowBuf, 0xff, 0xff, 0xff, 0xff)
			} else {
				str := fmt.Sprintf("%v", val)
				strLen := len(str)
				dataRowBuf = append(dataRowBuf, byte(strLen>>24), byte(strLen>>16), byte(strLen>>8), byte(strLen))
				dataRowBuf = append(dataRowBuf, []byte(str)...)
			}
		}

		// length を設定
		length := len(dataRowBuf) - 1
		dataRowBuf[1] = byte(length >> 24)
		dataRowBuf[2] = byte(length >> 16)
		dataRowBuf[3] = byte(length >> 8)
		dataRowBuf[4] = byte(length)

		if _, err := h.clientConn.Write(dataRowBuf); err != nil {
			return fmt.Errorf("send data row: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return h.sendQueryError(err)
	}

	// CommandComplete を送信
	tag := rows.CommandTag().String()
	cmdCompleteBuf := make([]byte, 0, 128)
	cmdCompleteBuf = append(cmdCompleteBuf, 'C') // CommandComplete
	cmdCompleteBuf = append(cmdCompleteBuf, 0, 0, 0, 0) // length placeholder
	cmdCompleteBuf = append(cmdCompleteBuf, []byte(tag)...)
	cmdCompleteBuf = append(cmdCompleteBuf, 0) // null terminator

	// length を設定
	cmdLength := len(cmdCompleteBuf) - 1
	cmdCompleteBuf[1] = byte(cmdLength >> 24)
	cmdCompleteBuf[2] = byte(cmdLength >> 16)
	cmdCompleteBuf[3] = byte(cmdLength >> 8)
	cmdCompleteBuf[4] = byte(cmdLength)

	if _, err := h.clientConn.Write(cmdCompleteBuf); err != nil {
		return fmt.Errorf("send command complete: %w", err)
	}

	// ReadyForQuery を送信
	readyBuf := []byte{
		'Z',        // ReadyForQuery
		0, 0, 0, 5, // length
		'I', // status (Idle)
	}
	if _, err := h.clientConn.Write(readyBuf); err != nil {
		return fmt.Errorf("send ready: %w", err)
	}

	return nil
}

func (h *SimpleConnectionHandler) sendQueryError(err error) error {
	errMsg := err.Error()
	errBuf := make([]byte, 0, 256)
	errBuf = append(errBuf, 'E') // ErrorResponse
	errBuf = append(errBuf, 0, 0, 0, 0) // length placeholder

	// Severity
	errBuf = append(errBuf, 'S')
	errBuf = append(errBuf, []byte("ERROR")...)
	errBuf = append(errBuf, 0)

	// Code
	errBuf = append(errBuf, 'C')
	errBuf = append(errBuf, []byte("42000")...)
	errBuf = append(errBuf, 0)

	// Message
	errBuf = append(errBuf, 'M')
	errBuf = append(errBuf, []byte(errMsg)...)
	errBuf = append(errBuf, 0)

	// Terminator
	errBuf = append(errBuf, 0)

	// length を設定
	length := len(errBuf) - 1
	errBuf[1] = byte(length >> 24)
	errBuf[2] = byte(length >> 16)
	errBuf[3] = byte(length >> 8)
	errBuf[4] = byte(length)

	h.clientConn.Write(errBuf)

	// ReadyForQuery を送信
	readyBuf := []byte{
		'Z',        // ReadyForQuery
		0, 0, 0, 5, // length
		'I', // status (Idle)
	}
	h.clientConn.Write(readyBuf)

	return nil
}

func (h *SimpleConnectionHandler) sendError(message string) {
	backend := pgproto3.NewBackend(h.clientConn, h.clientConn)
	errResp := &pgproto3.ErrorResponse{
		Severity: "ERROR",
		Code:     "XX000",
		Message:  message,
	}
	backend.Send(errResp)
}

func generateTempDBName() string {
	timestamp := time.Now().Format("20060102_150405")
	random := rand.Intn(10000)
	return fmt.Sprintf("temp_experiment_%s_%04d", timestamp, random)
}

func replaceDatabaseInDSN(dsn, newDB string) string {
	// postgres://user:pass@host:port/database の database 部分を置換
	parts := strings.Split(dsn, "/")
	if len(parts) >= 4 {
		// クエリパラメータを保持
		dbParts := strings.Split(parts[len(parts)-1], "?")
		parts[len(parts)-1] = newDB
		if len(dbParts) > 1 {
			parts[len(parts)-1] = newDB + "?" + dbParts[1]
		}
		return strings.Join(parts, "/")
	}
	return dsn
}
