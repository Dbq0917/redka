package server

import (
	"strings"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/nalgeon/redka"
	"github.com/nalgeon/redka/internal/testx"
	"github.com/nalgeon/redka/redsrv/internal/redis"
)

func TestInfoParse(t *testing.T) {
	tests := []struct {
		cmd string
		err error
	}{
		{cmd: "info", err: nil},
		{cmd: "info all", err: nil},
		{cmd: "info clients stats", err: nil},
	}

	for _, test := range tests {
		t.Run(test.cmd, func(t *testing.T) {
			_, err := redis.Parse(ParseInfo, test.cmd)
			be.Equal(t, err, test.err)
		})
	}
}

func TestInfoExec(t *testing.T) {
	db := testx.OpenDB(t)
	red := redis.RedkaDB(db)

	_ = red.Str().Set("name", "alice")
	_ = red.Str().Set("age", 25)

	db.RecordConnection()
	db.RecordConnection()
	db.RecordDisconnection()
	for range 3 {
		db.RecordCommand()
	}

	cmd := redis.MustParse(ParseInfo, "info")
	conn := redis.NewFakeConn()
	res, err := cmd.Run(conn, red)
	be.Err(t, err, nil)

	want := "# Clients\r\n" +
		"connected_clients:1\r\n" +
		"# Stats\r\n" +
		"total_connections_received:2\r\n" +
		"total_commands_processed:3\r\n" +
		"expired_keys:0\r\n" +
		"# Keyspace\r\n" +
		"db0:keys=2\r\n"
	be.Equal(t, res, any(want))
	be.Equal(t, conn.Out(), want)
}

func TestInfoExpiredKeysUseDBSizeScope(t *testing.T) {
	db := testx.OpenDB(t)
	red := redis.RedkaDB(db)

	_ = red.Str().Set("alive", "yes")
	_ = red.Str().Set("gone", "yes")
	err := db.Key().ExpireAt("gone", time.Now().Add(-time.Minute))
	be.Err(t, err, nil)

	sizeCmd := redis.MustParse(ParseDBSize, "dbsize")
	sizeRes, err := sizeCmd.Run(redis.NewFakeConn(), red)
	be.Err(t, err, nil)
	be.Equal(t, sizeRes, any(2))

	infoCmd := redis.MustParse(ParseInfo, "info")
	infoConn := redis.NewFakeConn()
	_, err = infoCmd.Run(infoConn, red)
	be.Err(t, err, nil)
	be.True(t, strings.Contains(infoConn.Out(), "expired_keys:0"))
	be.True(t, strings.Contains(infoConn.Out(), "db0:keys=2"))

	deleted, err := db.Key().DeleteExpired(0)
	be.Err(t, err, nil)
	be.Equal(t, deleted, 1)
	be.Equal(t, db.Stats().ExpiredKeys, uint64(1))

	deleted, err = db.DeleteExpired(0)
	be.Err(t, err, nil)
	be.Equal(t, deleted, 0)
	be.Equal(t, db.Stats().ExpiredKeys, uint64(1))

	sizeRes, err = sizeCmd.Run(redis.NewFakeConn(), red)
	be.Err(t, err, nil)
	be.Equal(t, sizeRes, any(1))

	infoConn = redis.NewFakeConn()
	_, err = infoCmd.Run(infoConn, red)
	be.Err(t, err, nil)
	be.True(t, strings.Contains(infoConn.Out(), "expired_keys:1"))
	be.True(t, strings.Contains(infoConn.Out(), "db0:keys=1"))
}

func TestInfoUsesTransactionStats(t *testing.T) {
	db := testx.OpenDB(t)
	db.RecordCommand()
	err := db.Update(func(tx *redka.Tx) error {
		conn := redis.NewFakeConn()
		cmd := redis.MustParse(ParseInfo, "info")
		_, err := cmd.Run(conn, redis.RedkaTx(tx))
		be.Err(t, err, nil)
		be.True(t, strings.Contains(conn.Out(), "total_commands_processed:1"))
		return nil
	})
	be.Err(t, err, nil)
}
