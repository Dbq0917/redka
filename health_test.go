package redka_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/nalgeon/be"
	"github.com/nalgeon/redka"

	_ "github.com/mattn/go-sqlite3"
)

func TestConsistencyCheckAndRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redka.db")

	db, err := redka.Open(path, nil)
	be.Err(t, err, nil)

	be.Err(t, db.Str().Set("str", "value"), nil)
	_, err = db.List().PushBack("list", "a")
	be.Err(t, err, nil)
	_, err = db.List().PushBack("list", "b")
	be.Err(t, err, nil)
	_, err = db.Set().Add("set", "a", "b", "c")
	be.Err(t, err, nil)
	_, err = db.Hash().SetMany("hash", map[string]any{"a": "1", "b": "2"})
	be.Err(t, err, nil)
	_, err = db.ZSet().AddMany("zset", map[any]float64{"a": 1, "b": 2})
	be.Err(t, err, nil)
	be.Err(t, db.Close(), nil)

	// Manually create inconsistencies that foreign keys and page-level
	// integrity checks cannot detect.
	raw, err := sql.Open("sqlite3", path)
	be.Err(t, err, nil)
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("pragma foreign_keys = off")
	be.Err(t, err, nil)

	var strID int
	err = raw.QueryRow("select id from rkey where key = 'str'").Scan(&strID)
	be.Err(t, err, nil)

	_, err = raw.Exec(`
		insert into rkey (key, type, version, mtime, len)
		values
			('empty-string', 1, 1, 1, null),
			('empty-hash', 4, 1, 1, 7),
			('bad-type', 99, 1, 1, 0)`)
	be.Err(t, err, nil)
	_, err = raw.Exec(
		"insert into rset (kid, elem) values (?, ?)",
		999999, []byte("orphan"),
	)
	be.Err(t, err, nil)
	_, err = raw.Exec(
		"insert into rhash (kid, field, value) values (?, 'ghost', ?)",
		strID, []byte("boo"),
	)
	be.Err(t, err, nil)
	_, err = raw.Exec("update rkey set len = 99 where key = 'hash'")
	be.Err(t, err, nil)
	be.Err(t, raw.Close(), nil)

	// A read-only handle must be enough to run the check.
	ro, err := redka.OpenRead(path, nil)
	be.Err(t, err, nil)
	report, err := ro.Check()
	be.Err(t, err, nil)
	be.Err(t, ro.Close(), nil)

	be.Equal(t, report.MissingData, 2)
	be.Equal(t, report.OrphanData, 1)
	be.Equal(t, report.TypeMismatches, 1)
	be.Equal(t, report.LengthMismatches, 1)
	be.Equal(t, report.InvalidTypes, 1)
	be.Equal(t, report.Total, 6)
	be.Equal(t, report.Clean(), false)
	be.Equal(t, report.ByType[redka.TypeString].MissingData, 1)
	be.Equal(t, report.ByType[redka.TypeHash].MissingData, 1)
	be.Equal(t, report.ByType[redka.TypeHash].TypeMismatches, 1)
	be.Equal(t, report.ByType[redka.TypeHash].LengthMismatches, 1)
	be.Equal(t, report.ByType[redka.TypeSet].OrphanData, 1)

	db, err = redka.Open(path, nil)
	be.Err(t, err, nil)
	defer func() { _ = db.Close() }()

	repaired, err := db.Repair()
	be.Err(t, err, nil)
	be.Equal(t, repaired.DeletedKeys, 3)
	be.Equal(t, repaired.DeletedData, 2)
	be.Equal(t, repaired.RebuiltLengths, 1)
	be.Equal(t, repaired.Total, 6)

	report, err = db.Check()
	be.Err(t, err, nil)
	be.Equal(t, report.Clean(), true)

	// Repair is idempotent: the second run must not change anything.
	repaired, err = db.Repair()
	be.Err(t, err, nil)
	be.Equal(t, repaired.Total, 0)

	report, err = db.Check()
	be.Err(t, err, nil)
	be.Equal(t, report.Clean(), true)

	// Healthy keys remain usable and their lengths are rebuilt.
	hashLen, err := db.Hash().Len("hash")
	be.Err(t, err, nil)
	be.Equal(t, hashLen, 2)
	_, err = db.Hash().Get("hash", "ghost")
	be.Err(t, err, redka.ErrNotFound)
	str, err := db.Str().Get("str")
	be.Err(t, err, nil)
	be.Equal(t, str.String(), "value")

	for _, key := range []string{"empty-string", "empty-hash", "bad-type"} {
		exists, err := db.Key().Exists(key)
		be.Err(t, err, nil)
		be.Equal(t, exists, false)
	}
}
