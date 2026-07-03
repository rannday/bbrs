package bitburner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const RequestTimeout = 30 * time.Second

type Client struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	nextID uint64
}

func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn:   conn,
		nextID: 1,
	}
}

func (client *Client) Close(status websocket.StatusCode, reason string) error {
	return client.conn.Close(status, reason)
}

func (client *Client) Ping(ctx context.Context) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	pingCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	return client.conn.Ping(pingCtx)
}

func (client *Client) GetFileNames(ctx context.Context, server string) ([]string, error) {
	var names []string
	if err := client.requestResult(ctx, "getFileNames", map[string]string{"server": server}, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func (client *Client) PushFile(ctx context.Context, server, filename, content string) error {
	result, err := client.requestRaw(ctx, "pushFile", map[string]string{
		"filename": filename,
		"content":  content,
		"server":   server,
	})
	if err != nil {
		return err
	}
	return RequireOK("pushFile", result)
}

func (client *Client) DeleteFile(ctx context.Context, server, filename string) error {
	result, err := client.requestRaw(ctx, "deleteFile", map[string]string{
		"filename": filename,
		"server":   server,
	})
	if err != nil {
		return err
	}
	return RequireOK("deleteFile", result)
}

func (client *Client) requestResult(ctx context.Context, method string, params interface{}, target interface{}) error {
	result, err := client.requestRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("%s response missing result", method)
	}
	if err := json.Unmarshal(*result, target); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

func (client *Client) requestRaw(ctx context.Context, method string, params interface{}) (*json.RawMessage, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	requestID := client.nextID
	client.nextID++
	if client.nextID == 0 {
		client.nextID = 1
	}

	req := NewRequest(requestID, method, params)
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", method, err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	if err := client.conn.Write(requestCtx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("send %s request: %w", method, err)
	}

	messageType, responseData, err := client.conn.Read(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("%s timed out or failed waiting for response: %w", method, err)
	}
	if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
		return nil, fmt.Errorf("%s returned unsupported websocket message type %v", method, messageType)
	}

	var response Response
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", method, err)
	}
	result, err := ValidateResponse(method, requestID, response)
	if err != nil {
		return nil, err
	}
	return result, nil
}
