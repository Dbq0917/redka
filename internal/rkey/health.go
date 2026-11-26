package rkey

import "github.com/nalgeon/redka/internal/health"

// Check scans the database for logical inconsistencies between keys and
// their type-specific data.
func (d *DB) Check() (health.CheckResult, error) {
	return health.Check(d.ro)
}

// CheckConsistency is an alias for [DB.Check].
func (d *DB) CheckConsistency() (health.CheckResult, error) {
	return d.Check()
}

// HealthCheck is an alias for [DB.Check].
func (d *DB) HealthCheck() (health.CheckResult, error) {
	return d.Check()
}

// Verify is an alias for [DB.Check].
func (d *DB) Verify() (health.CheckResult, error) {
	return d.Check()
}

// Repair removes orphaned and misplaced data, deletes keys without data,
// and rebuilds length counters.
func (d *DB) Repair() (health.RepairResult, error) {
	return health.Repair(d.rw)
}

// RepairConsistency is an alias for [DB.Repair].
func (d *DB) RepairConsistency() (health.RepairResult, error) {
	return d.Repair()
}

// HealthRepair is an alias for [DB.Repair].
func (d *DB) HealthRepair() (health.RepairResult, error) {
	return d.Repair()
}
