package redsrv

import (
	"errors"
	"log/slog"
	"time"

	"github.com/nalgeon/redka"
	"github.com/nalgeon/redka/redsrv/internal/command"
	"github.com/nalgeon/redka/redsrv/internal/redis"
	"github.com/tidwall/redcon"
)

var errWatchConflict = errors.New("watched key was modified")

type unwatchCommand struct {
	redis.BaseCmd
}

func (unwatchCommand) Name() string { return "unwatch" }

func (unwatchCommand) String() string { return "UNWATCH" }

func (unwatchCommand) Error(err error) string { return err.Error() }

func (unwatchCommand) Run(w redis.Writer, _ redis.Redka) (any, error) {
	w.WriteString("OK")
	return true, nil
}

// createHandlers returns the server command handlers.
func createHandlers(db *redka.DB) redcon.HandlerFunc {
	return logging(parse(multi(db, handle(db))), db.Log())
}

// logging logs the command processing time.
func logging(next redcon.HandlerFunc, log *slog.Logger) redcon.HandlerFunc {
	return func(conn redcon.Conn, cmd redcon.Command) {
		start := time.Now()
		next(conn, cmd)
		log.Debug("process command", "client", conn.RemoteAddr(),
			"name", string(cmd.Args[0]), "time", time.Since(start))
	}
}

// parse parses the command arguments.
func parse(next redcon.HandlerFunc) redcon.HandlerFunc {
	return func(conn redcon.Conn, cmd redcon.Command) {
		pcmd, err := command.Parse(cmd.Args)
		if err != nil {
			conn.WriteError(pcmd.Error(err))
			return
		}
		state := getState(conn)
		state.push(pcmd)
		next(conn, cmd)
	}
}

// multi handles the MULTI, EXEC, DISCARD, WATCH, and UNWATCH commands
// and delegates the rest to the next handler either in multi or single mode.
func multi(db *redka.DB, next redcon.HandlerFunc) redcon.HandlerFunc {
	return func(conn redcon.Conn, cmd redcon.Command) {
		name := normName(cmd)
		state := getState(conn)
		if state.inMulti {
			switch name {
			case "multi":
				state.pop()
				conn.WriteError(redis.ErrNestedMulti.Error())
			case "exec":
				state.pop()
				next(conn, cmd)
				state.inMulti = false
			case "discard":
				state.clear()
				state.unwatch()
				conn.WriteString("OK")
				state.inMulti = false
			case "watch":
				state.pop()
				conn.WriteError(redis.ErrWatchInsideMulti.Error())
			case "unwatch":
				state.pop()
				if len(cmd.Args) != 1 {
					conn.WriteError(redis.ErrUnwatchArgNum.Error())
				} else {
					state.push(unwatchCommand{})
					conn.WriteString("QUEUED")
				}
			default:
				conn.WriteString("QUEUED")
			}
		} else {
			switch name {
			case "watch":
				state.pop()
				handleWatch(db, conn, state, cmd)
			case "unwatch":
				state.pop()
				if len(cmd.Args) != 1 {
					conn.WriteError(redis.ErrUnwatchArgNum.Error())
				} else {
					state.unwatch()
					conn.WriteString("OK")
				}
			case "multi":
				state.inMulti = true
				state.pop()
				conn.WriteString("OK")
			case "exec":
				state.pop()
				conn.WriteError(redis.ErrNotInMulti.Error())
			case "discard":
				state.pop()
				conn.WriteError(redis.ErrNotInMulti.Error())
			default:
				next(conn, cmd)
			}
		}
	}
}

// handle processes the command in either multi or single mode.
func handle(db *redka.DB) redcon.HandlerFunc {
	return func(conn redcon.Conn, cmd redcon.Command) {
		state := getState(conn)
		if state.inMulti {
			handleMulti(conn, state, db)
		} else {
			handleSingle(conn, state, db)
		}
		state.clear()
	}
}

// handleMulti processes a batch of commands in a transaction.
func handleMulti(conn redcon.Conn, state *connState, db *redka.DB) {
	defer state.unwatch()

	conflicted := false
	err := db.Update(func(tx *redka.Tx) error {
		if len(state.watches) > 0 {
			ok, err := watchesMatch(tx, state.watches)
			if err != nil {
				conn.WriteError(redis.ErrWatchState.Error())
				return err
			}
			if !ok {
				conflicted = true
				return errWatchConflict
			}
		}

		conn.WriteArray(len(state.cmds))
		for _, pcmd := range state.cmds {
			_, err := pcmd.Run(conn, redis.RedkaTx(tx))
			if err != nil {
				db.Log().Warn("run multi command", "client", conn.RemoteAddr(),
					"name", pcmd.Name(), "err", err)
				return err
			}
		}
		return nil
	})

	switch {
	case conflicted || errors.Is(err, errWatchConflict):
		conn.WriteArray(-1)
	case err != nil:
		db.Log().Warn("run multi", "client", conn.RemoteAddr(), "err", err)
	}
}

// handleWatch records the current state of the requested keys.
func handleWatch(db *redka.DB, conn redcon.Conn, state *connState, cmd redcon.Command) {
	if len(cmd.Args) < 2 {
		conn.WriteError(redis.ErrWatchArgNum.Error())
		return
	}

	baselines := make(map[string]watchState, len(cmd.Args)-1)
	for _, arg := range cmd.Args[1:] {
		key := string(arg)
		if _, ok := baselines[key]; ok {
			continue
		}
		if _, watched := state.watches[key]; watched {
			continue
		}

		keyInfo, err := db.Key().Get(key)
		if errors.Is(err, redka.ErrNotFound) {
			baselines[key] = watchState{}
			continue
		}
		if err != nil {
			db.Log().Warn("watch key", "client", conn.RemoteAddr(), "key", key, "err", err)
			conn.WriteError(redis.ErrWatchState.Error())
			return
		}
		baselines[key] = newWatchState(keyInfo)
	}
	for key, baseline := range baselines {
		state.watch(key, baseline)
	}

	conn.WriteString("OK")
}

// watchesMatch compares watched key baselines with their current state.
// Every lookup is a unique-key point query, so it never scans the keyspace.
func watchesMatch(tx *redka.Tx, watches map[string]watchState) (bool, error) {
	for key, baseline := range watches {
		keyInfo, err := tx.Key().Get(key)
		if errors.Is(err, redka.ErrNotFound) {
			if baseline.exists {
				return false, nil
			}
			continue
		}
		if err != nil {
			return false, err
		}

		if newWatchState(keyInfo) != baseline {
			return false, nil
		}
	}
	return true, nil
}

func newWatchState(key redka.Key) watchState {
	return watchState{
		exists:  true,
		id:      key.ID,
		version: key.Version,
		mtime:   key.MTime,
	}
}

// handleSingle processes a single command.
func handleSingle(conn redcon.Conn, state *connState, db *redka.DB) {
	pcmd := state.pop()
	_, err := pcmd.Run(conn, redis.RedkaDB(db))
	if err != nil {
		db.Log().Warn("run single command", "client", conn.RemoteAddr(),
			"name", pcmd.Name(), "err", err)
		return
	}
}
