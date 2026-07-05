package bitburner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

type fakeConn struct {
	writeErr error
	readErr  error
	readData []byte
	writes   int
}

func (conn *fakeConn) Write(context.Context, websocket.MessageType, []byte) error {
	conn.writes++
	return conn.writeErr
}

func (conn *fakeConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	return websocket.MessageText, conn.readData, conn.readErr
}

func (conn *fakeConn) Close(websocket.StatusCode, string) error {
	return nil
}

func responseJSON(id uint64, result any) []byte {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}

func TestWriteFailureMarksClientDisconnected(t *testing.T) {
	client := newClient(&fakeConn{writeErr: errors.New("use of closed network connection")})
	disconnects := 0
	client.SetDisconnectHandler(func(error) {
		disconnects++
	})

	_, err := client.GetAllFileMetadata(context.Background(), "home")
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("err = %v", err)
	}
	if client.Connected() {
		t.Fatal("client still connected")
	}
	client.MarkDisconnected(errors.New("again"))
	if disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1", disconnects)
	}
}

func TestReadFailureMarksClientDisconnected(t *testing.T) {
	client := newClient(&fakeConn{readErr: errors.New("connection reset by peer")})
	disconnects := 0
	client.SetDisconnectHandler(func(error) {
		disconnects++
	})

	_, err := client.GetAllFileMetadata(context.Background(), "home")
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("err = %v", err)
	}
	if client.Connected() {
		t.Fatal("client still connected")
	}
	if disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1", disconnects)
	}
}

func TestGetAllFileMetadataUsesRemoteAPI(t *testing.T) {
	conn := &fakeConn{
		readData: responseJSON(1, []map[string]any{{
			"filename": "main.js",
			"atime":    int64(1577836800000),
			"btime":    int64(1577836800000),
			"mtime":    int64(1577923200000),
		}}),
	}
	client := newClient(conn)

	metadata, err := client.GetAllFileMetadata(context.Background(), "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 1 || metadata[0].Filename != "main.js" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if conn.writes != 1 {
		t.Fatalf("writes = %d", conn.writes)
	}
}
