package rkey

// Postgres queries for the key repository.
var postgres = queries{
	deleteAll: `
	truncate table rkey cascade`,

	deleteNExpired: `
	delete from rkey
	where ctid in (
		select ctid from rkey
		where etime <= $1
		limit $2
	)`,
}

func init() {
	postgres.count = sqlite.count
	postgres.delete = sqlite.delete
	// postgres.deleteAll = sqlite.deleteAll
	postgres.deleteAllExpired = sqlite.deleteAllExpired
	// postgres.deleteNExpired = sqlite.deleteNExpired
	postgres.expire = sqlite.expire
	postgres.get = sqlite.get
	postgres.len = sqlite.len
	postgres.persist = sqlite.persist
	postgres.random = sqlite.random
	postgres.rename1 = sqlite.rename1
	postgres.rename2 = sqlite.rename2
}
