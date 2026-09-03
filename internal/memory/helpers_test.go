//go:build linux

package memory

import (
	"encoding/json"
	"time"
)

func timeNowPlusMinute() time.Time { return time.Now().Add(time.Minute) }

func jsonUnmarshal(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

func strPtr(s string) *string { return &s }
