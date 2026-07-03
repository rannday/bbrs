package bitburner

import (
	"encoding/json"
	"fmt"
)

const JSONRPCVersion = "2.0"

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      uint64      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      uint64           `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type RPCError struct {
	Code    int64           `json:"code,omitempty"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewRequest(id uint64, method string, params interface{}) Request {
	return Request{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

func ValidateResponse(method string, requestID uint64, response Response) (*json.RawMessage, error) {
	if response.ID != requestID {
		return nil, fmt.Errorf("%s response id %d did not match request id %d", method, response.ID, requestID)
	}
	if response.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("invalid jsonrpc version %q", response.JSONRPC)
	}

	hasResult := response.Result != nil
	hasError := response.Error != nil
	switch {
	case hasResult && hasError:
		return nil, fmt.Errorf("%s response has both result and error", method)
	case !hasResult && !hasError:
		return nil, fmt.Errorf("%s response has neither result nor error", method)
	case hasError:
		return nil, response.Error
	default:
		return response.Result, nil
	}
}

func RequireOK(method string, result *json.RawMessage) error {
	if result == nil {
		return fmt.Errorf("%s response missing result", method)
	}
	var value string
	if err := json.Unmarshal(*result, &value); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	if value != "OK" {
		return fmt.Errorf("%s returned unexpected result %q", method, value)
	}
	return nil
}

func (err *RPCError) Error() string {
	if err == nil {
		return "remote JSON-RPC error"
	}
	if err.Code == 0 {
		return fmt.Sprintf("remote JSON-RPC error: %s", err.Message)
	}
	return fmt.Sprintf("remote JSON-RPC error %d: %s", err.Code, err.Message)
}
