// Package health checks and repairs logical consistency between the key
// metadata stored in rkey and the data stored in the type-specific tables.
package health

import (
	"database/sql"
	"fmt"

	"github.com/nalgeon/redka/internal/core"
)

// IssueKind identifies a class of logical inconsistency.
type IssueKind string

const (
	// IssueMissingData means that a key exists but has no data row.
	IssueMissingData IssueKind = "missing-data"
	// IssueOrphanData means that data exists without a matching key.
	IssueOrphanData IssueKind = "orphan-data"
	// IssueTypeMismatch means that data is stored in a table that does not
	// match the type recorded in the key.
	IssueTypeMismatch IssueKind = "type-mismatch"
	// IssueLengthMismatch means that the length recorded in the key does not
	// match the number of data rows.
	IssueLengthMismatch IssueKind = "length-mismatch"
	// IssueInvalidType means that a key has an unknown type.
	IssueInvalidType IssueKind = "invalid-type"
)

// Compatibility aliases for the more explicit issue names.
const (
	IssueMissingValue = IssueMissingData
	IssueOrphanValue  = IssueOrphanData
	IssueOrphanKey    = IssueMissingData
)

// String returns the issue name.
func (k IssueKind) String() string {
	return string(k)
}

// TypeReport contains the inconsistencies found for one key type.
type TypeReport struct {
	Type             core.TypeID
	MissingData      int
	OrphanData       int
	TypeMismatches   int
	LengthMismatches int
	Total            int
}

// CheckResult contains aggregate inconsistency counts. It intentionally
// contains no key list, so a check works without loading the whole database
// into memory.
type CheckResult struct {
	MissingData      int
	OrphanData       int
	TypeMismatches   int
	LengthMismatches int
	InvalidTypes     int
	Total            int

	// ByType contains counts grouped by key type. Orphaned data is grouped
	// by the type of its data table.
	ByType map[core.TypeID]TypeReport
	// Counts contains counts grouped by issue kind.
	Counts map[IssueKind]int
}

// Clean reports whether the database is logically consistent.
func (r CheckResult) Clean() bool {
	return r.Total == 0
}

// RepairResult contains the number of records changed by a repair.
type RepairResult struct {
	DeletedKeys    int
	DeletedData    int
	RebuiltLengths int
	Total          int
}

type dataType struct {
	typ       core.TypeID
	table     string
	hasLength bool
}

var dataTypes = []dataType{
	{core.TypeString, "rstring", false},
	{core.TypeList, "rlist", true},
	{core.TypeSet, "rset", true},
	{core.TypeHash, "rhash", true},
	{core.TypeZSet, "rzset", true},
}

// Check scans the key metadata and all type-specific tables for logical
// inconsistencies. It only reads data and never takes a write lock.
func Check(ro *sql.DB) (CheckResult, error) {
	result := CheckResult{
		ByType: map[core.TypeID]TypeReport{},
		Counts: map[IssueKind]int{},
	}
	for _, dt := range dataTypes {
		result.ByType[dt.typ] = TypeReport{Type: dt.typ}

		lengthExpr := "0"
		if dt.hasLength {
			lengthExpr = fmt.Sprintf(`
				(select count(*)
				 from rkey k
				 where k.type = $1
				   and exists (
				       select 1 from %[1]s d where d.kid = k.id
				   )
				   and coalesce(k.len, -1) <> (
				       select count(*) from %[1]s d where d.kid = k.id
				   ))`, dt.table)
		}

		query := fmt.Sprintf(`
			select
				(select count(*)
				 from rkey k
				 where k.type = $1
				   and not exists (
				       select 1 from %[1]s d where d.kid = k.id
				   )),
				(select count(distinct d.kid)
				 from %[1]s d
				 left join rkey k on k.id = d.kid
				 where k.id is null),
				(select count(distinct d.kid)
				 from %[1]s d
				 join rkey k on k.id = d.kid
				 where k.type <> $1),
				%[2]s`,
			dt.table, lengthExpr)

		var missing, orphan, mismatch, length int
		err := ro.QueryRow(query, int(dt.typ)).Scan(
			&missing, &orphan, &mismatch, &length,
		)
		if err != nil {
			return CheckResult{}, err
		}

		result.add(IssueMissingData, dt.typ, missing)
		result.add(IssueOrphanData, dt.typ, orphan)
		result.add(IssueTypeMismatch, dt.typ, mismatch)
		result.add(IssueLengthMismatch, dt.typ, length)
	}

	var invalid int
	err := ro.QueryRow(`
		select count(*) from rkey
		where type not in (1, 2, 3, 4, 5)`).Scan(&invalid)
	if err != nil {
		return CheckResult{}, err
	}
	result.add(IssueInvalidType, core.TypeAny, invalid)

	return result, nil
}

func (r *CheckResult) add(kind IssueKind, typ core.TypeID, n int) {
	if n == 0 {
		return
	}

	r.Counts[kind] += n
	r.Total += n

	tr := r.ByType[typ]
	switch kind {
	case IssueMissingData:
		r.MissingData += n
		tr.MissingData += n
	case IssueOrphanData:
		r.OrphanData += n
		tr.OrphanData += n
	case IssueTypeMismatch:
		r.TypeMismatches += n
		tr.TypeMismatches += n
	case IssueLengthMismatch:
		r.LengthMismatches += n
		tr.LengthMismatches += n
	case IssueInvalidType:
		r.InvalidTypes += n
	}
	tr.Total += n
	r.ByType[typ] = tr
}

// Repair removes orphaned and misplaced data, deletes keys without data,
// and rebuilds length counters. It is idempotent: running it again after a
// successful repair makes no changes.
func Repair(rw *sql.DB) (RepairResult, error) {
	tx, err := rw.Begin()
	if err != nil {
		return RepairResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result RepairResult
	exec := func(query string, args ...any) (int, error) {
		res, err := tx.Exec(query, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		return int(n), err
	}

	// Remove rows that have no parent or whose parent has another type.
	for _, dt := range dataTypes {
		query := fmt.Sprintf(`
			delete from %[1]s
			where not exists (
				select 1 from rkey k
				where k.id = %[1]s.kid and k.type = $1
			)`, dt.table)
		n, err := exec(query, int(dt.typ))
		if err != nil {
			return RepairResult{}, err
		}
		result.DeletedData += n
	}

	// Remove keys with an unknown type. Their data rows were removed above.
	n, err := exec(`
		delete from rkey
		where type not in (1, 2, 3, 4, 5)`)
	if err != nil {
		return RepairResult{}, err
	}
	result.DeletedKeys += n

	// A key without data cannot be represented by the Redis data model.
	for _, dt := range dataTypes {
		query := fmt.Sprintf(`
			delete from rkey
			where type = $1
			  and not exists (
			      select 1 from %s d where d.kid = rkey.id
			  )`, dt.table)
		n, err := exec(query, int(dt.typ))
		if err != nil {
			return RepairResult{}, err
		}
		result.DeletedKeys += n
	}

	// Rebuild collection lengths from their actual data rows.
	for _, dt := range dataTypes {
		if !dt.hasLength {
			continue
		}
		query := fmt.Sprintf(`
			update rkey
			set len = (
				select count(*) from %s d where d.kid = rkey.id
			)
			where type = $1
			  and coalesce(len, -1) <> (
			      select count(*) from %s d where d.kid = rkey.id
			  )`, dt.table, dt.table)
		n, err := exec(query, int(dt.typ))
		if err != nil {
			return RepairResult{}, err
		}
		result.RebuiltLengths += n
	}

	if err := tx.Commit(); err != nil {
		return RepairResult{}, err
	}

	result.Total = result.DeletedKeys + result.DeletedData + result.RebuiltLengths
	return result, nil
}
