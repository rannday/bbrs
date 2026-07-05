package syncer

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexibleTimestamp unmarshals Bitburner metadata timestamps from strings or numbers.
// Docs describe strings; game API often sends Unix epoch milliseconds as numbers.
type FlexibleTimestamp int64

func (ts *FlexibleTimestamp) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*ts = 0
		return nil
	}

	var number int64
	if err := json.Unmarshal(data, &number); err == nil {
		*ts = FlexibleTimestamp(normalizeEpoch(number))
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("timestamp: %w", err)
	}
	if text == "" {
		*ts = 0
		return nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("timestamp %q: %w", text, err)
	}
	*ts = FlexibleTimestamp(normalizeEpoch(parsed))
	return nil
}

// Milliseconds returns the timestamp as Unix epoch milliseconds.
func (ts FlexibleTimestamp) Milliseconds() int64 {
	return int64(ts)
}

// normalizeEpoch converts second- or millisecond-scale epoch values to milliseconds.
func normalizeEpoch(value int64) int64 {
	switch {
	case value == 0:
		return 0
	case value < 1_000_000_000_000: // before year ~2001 in ms
		return value * 1000
	default:
		return value
	}
}
