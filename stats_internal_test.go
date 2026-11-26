package redka

import (
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nalgeon/be"
)

func TestBackgroundCleanupRecordsExpiredKeys(t *testing.T) {
	oldInterval := bgInterval
	bgInterval = time.Millisecond
	t.Cleanup(func() { bgInterval = oldInterval })

	db, err := Open("file:/stats-bg.db?vfs=memdb", &Options{DriverName: "sqlite3"})
	be.Err(t, err, nil)
	t.Cleanup(func() { _ = db.Close() })

	err = db.Str().Set("gone", "value")
	be.Err(t, err, nil)
	err = db.Key().ExpireAt("gone", time.Now().Add(-time.Minute))
	be.Err(t, err, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if db.Stats().ExpiredKeys == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	stats := db.Stats()
	be.Equal(t, stats.ExpiredKeys, uint64(1))

	keys, err := db.Key().Len()
	be.Err(t, err, nil)
	be.Equal(t, keys, 0)
}
