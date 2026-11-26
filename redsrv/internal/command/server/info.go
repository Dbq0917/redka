package server

import (
	"fmt"
	"strings"

	"github.com/nalgeon/redka/redsrv/internal/redis"
)

// Info returns connection, command, expiration, and keyspace statistics.
// The keyspace count uses Key.Len, so it matches DBSIZE and includes
// expired rows that have not yet been deleted by the background cleanup.
// INFO [section ...]
// https://redis.io/commands/info
type Info struct {
	redis.BaseCmd
}

func ParseInfo(b redis.BaseCmd) (Info, error) {
	return Info{BaseCmd: b}, nil
}

func (c Info) Run(w redis.Writer, red redis.Redka) (any, error) {
	stats := red.Stats()
	keys, err := red.Key().Len()
	if err != nil {
		w.WriteError(c.Error(err))
		return nil, err
	}

	var info strings.Builder
	info.WriteString("# Clients\r\n")
	fmt.Fprintf(&info, "connected_clients:%d\r\n", stats.ConnectedClients)

	info.WriteString("# Stats\r\n")
	fmt.Fprintf(&info, "total_connections_received:%d\r\n", stats.TotalConnections)
	fmt.Fprintf(&info, "total_commands_processed:%d\r\n", stats.TotalCommands)
	fmt.Fprintf(&info, "expired_keys:%d\r\n", stats.ExpiredKeys)

	info.WriteString("# Keyspace\r\n")
	fmt.Fprintf(&info, "db0:keys=%d\r\n", keys)

	result := info.String()
	w.WriteBulkString(result)
	return result, nil
}
