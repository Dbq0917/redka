package redka

import "sync/atomic"

// Stats is a point-in-time snapshot of server statistics.
type Stats struct {
	// ConnectedClients is the current number of client connections.
	ConnectedClients uint64
	// TotalConnections is the cumulative number of accepted connections.
	TotalConnections uint64
	// TotalCommands is the cumulative number of parsed commands.
	TotalCommands uint64
	// ExpiredKeys is the cumulative number of expired keys deleted.
	ExpiredKeys uint64
}

// statsCollector stores counters shared by the database and its transactions.
type statsCollector struct {
	connectedClients atomic.Int64
	totalConnections atomic.Uint64
	totalCommands    atomic.Uint64
	expiredKeys      atomic.Uint64
}

func (s *statsCollector) clientConnected() {
	if s == nil {
		return
	}
	s.totalConnections.Add(1)
	s.connectedClients.Add(1)
}

func (s *statsCollector) clientDisconnected() {
	if s == nil {
		return
	}
	s.connectedClients.Add(-1)
}

func (s *statsCollector) commandProcessed() {
	if s == nil {
		return
	}
	s.totalCommands.Add(1)
}

func (s *statsCollector) expiredKeysDeleted(count int) {
	if s == nil || count <= 0 {
		return
	}
	s.expiredKeys.Add(uint64(count))
}

func (s *statsCollector) snapshot() Stats {
	if s == nil {
		return Stats{}
	}
	connected := s.connectedClients.Load()
	if connected < 0 {
		connected = 0
	}
	return Stats{
		ConnectedClients: uint64(connected),
		TotalConnections: s.totalConnections.Load(),
		TotalCommands:    s.totalCommands.Load(),
		ExpiredKeys:      s.expiredKeys.Load(),
	}
}

// Stats returns a point-in-time snapshot of the database statistics.
func (db *DB) Stats() Stats {
	return db.stats.snapshot()
}

// Stats returns the database statistics shared by this transaction.
func (tx *Tx) Stats() Stats {
	return tx.stats.snapshot()
}

// RecordConnection records a newly accepted client connection.
func (db *DB) RecordConnection() {
	db.stats.clientConnected()
}

// RecordDisconnection records a closed client connection.
func (db *DB) RecordDisconnection() {
	db.stats.clientDisconnected()
}

// RecordCommand records a successfully parsed command.
func (db *DB) RecordCommand() {
	db.stats.commandProcessed()
}

// DeleteExpired deletes keys with expired TTL, but no more than n keys.
// If n = 0, deletes all expired keys. The key repository records deletions
// in the shared statistics collector.
func (db *DB) DeleteExpired(n int) (int, error) {
	return db.keyDB.DeleteExpired(n)
}
