//go:build linux

package memory

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/pathx"
	_ "modernc.org/sqlite"
)

func timeNowPlusMinute() time.Time { return time.Now().Add(time.Minute) }

func jsonUnmarshal(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }

func strPtr(s string) *string { return &s }

func sqlOpen(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }

func pathxEnsure(l pathx.Layout, dir string) error { return pathx.EnsureDir(l.Base, dir) }
