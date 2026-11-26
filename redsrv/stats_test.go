package redsrv

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/nalgeon/redka"
	"github.com/nalgeon/redka/internal/testx"
	"github.com/tidwall/redcon"
)

func TestHandlerCommandStats(t *testing.T) {
	db := testx.OpenDB(t)
	handler := createHandlers(db)

	pingConn := new(fakeConn)
	handler.ServeRESP(pingConn, redcon.Command{
		Args: [][]byte{[]byte("PING")},
	})

	infoConn := new(fakeConn)
	handler.ServeRESP(infoConn, redcon.Command{
		Args: [][]byte{[]byte("INFO")},
	})

	stats := db.Stats()
	be.Equal(t, stats.TotalCommands, uint64(2))
	be.True(t, strings.Contains(infoConn.out(), "total_commands_processed:2"))
}

func TestInfoAfterSelectUsesSingleDatabase(t *testing.T) {
	db := testx.OpenDB(t)
	handler := createHandlers(db)
	conn := new(fakeConn)

	commands := [][][]byte{
		{[]byte("SELECT"), []byte("5")},
		{[]byte("SET"), []byte("answer"), []byte("42")},
		{[]byte("INFO")},
	}
	for _, args := range commands {
		handler.ServeRESP(conn, redcon.Command{Args: args})
	}

	be.Equal(t, db.Stats().TotalCommands, uint64(3))
	be.True(t, strings.Contains(conn.out(), "db0:keys=1"))
	be.True(t, !strings.Contains(conn.out(), "db5"))
}

func TestServerConnectionStats(t *testing.T) {
	db := testx.OpenDB(t)
	srv := New("tcp", "127.0.0.1:0", db)
	t.Cleanup(func() { _ = srv.Stop() })

	ready := make(chan error, 1)
	go func() {
		_ = srv.Start(ready)
	}()
	be.Err(t, <-ready, nil)

	conn, err := net.Dial("tcp", srv.srv.Addr().String())
	be.Err(t, err, nil)
	defer conn.Close()

	_, err = conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	be.Err(t, err, nil)

	reader := bufio.NewReader(conn)
	header, err := reader.ReadString('\n')
	be.Err(t, err, nil)
	body, err := reader.ReadString('\n')
	be.Err(t, err, nil)
	be.Equal(t, header, "$4\r\n")
	be.Equal(t, body, "PONG\r\n")

	waitForStats(t, db, func(stats redka.Stats) bool {
		return stats.ConnectedClients == 1 &&
			stats.TotalConnections == 1 &&
			stats.TotalCommands == 1
	})

	be.Err(t, conn.Close(), nil)
	waitForStats(t, db, func(stats redka.Stats) bool {
		return stats.ConnectedClients == 0 && stats.TotalConnections == 1
	})

	be.Err(t, srv.Stop(), nil)
}

func waitForStats(tb testing.TB, db *redka.DB, match func(redka.Stats) bool) {
	tb.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if match(db.Stats()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for stats: %+v", db.Stats())
}
