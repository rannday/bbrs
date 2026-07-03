package bitburner

import (
	"encoding/json"
	"strings"
	"testing"
)

func raw(value string) *json.RawMessage {
	message := json.RawMessage(value)
	return &message
}

func TestValidateResponseCatchesMismatchedIDs(t *testing.T) {
	_, err := ValidateResponse("pushFile", 1, Response{JSONRPC: "2.0", ID: 2, Result: raw(`"OK"`)})
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateResponseCatchesInvalidJSONRPC(t *testing.T) {
	_, err := ValidateResponse("pushFile", 1, Response{JSONRPC: "1.0", ID: 1, Result: raw(`"OK"`)})
	if err == nil || !strings.Contains(err.Error(), "invalid jsonrpc") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateResponseCatchesBothResultAndError(t *testing.T) {
	_, err := ValidateResponse("pushFile", 1, Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  raw(`"OK"`),
		Error:   &RPCError{Code: -32000, Message: "bad"},
	})
	if err == nil || !strings.Contains(err.Error(), "both result and error") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateResponseCatchesNeitherResultNorError(t *testing.T) {
	_, err := ValidateResponse("pushFile", 1, Response{JSONRPC: "2.0", ID: 1})
	if err == nil || !strings.Contains(err.Error(), "neither result nor error") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequireOKForPushFileAndDeleteFile(t *testing.T) {
	for _, method := range []string{"pushFile", "deleteFile"} {
		if err := RequireOK(method, raw(`"OK"`)); err != nil {
			t.Fatalf("%s OK err = %v", method, err)
		}
		err := RequireOK(method, raw(`"NO"`))
		if err == nil || !strings.Contains(err.Error(), "unexpected result") {
			t.Fatalf("%s err = %v", method, err)
		}
	}
}
