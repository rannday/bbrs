package bitburner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

type fakeConn struct {
	writeErr error
	readErr  error
	readData []byte
}

func (conn *fakeConn) Write(context.Context, websocket.MessageType, []byte) error {
	return conn.writeErr
}

func (conn *fakeConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	return websocket.MessageText, conn.readData, conn.readErr
}

func (conn *fakeConn) Close(websocket.StatusCode, string) error {
	return nil
}

func TestWriteFailureMarksClientDisconnected(t *testing.T) {
	client := newClient(&fakeConn{writeErr: errors.New("use of closed network connection")})
	disconnects := 0
	client.SetDisconnectHandler(func(error) {
		disconnects++
	})

	_, err := client.GetFileNames(context.Background(), "home")
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

	_, err := client.GetFileNames(context.Background(), "home")
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
