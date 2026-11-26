package redsrv

import (
	"fmt"
	"strings"

	"github.com/nalgeon/redka/redsrv/internal/redis"
	"github.com/tidwall/redcon"
)

// normName returns the normalized command name.
func normName(cmd redcon.Command) string {
	return strings.ToLower(string(cmd.Args[0]))
}

// getState returns the connection state.
func getState(conn redcon.Conn) *connState {
	state := conn.Context()
	if state == nil {
		state = new(connState)
		conn.SetContext(state)
	}
	return state.(*connState)
}

// connState represents the connection state.
type connState struct {
	inMulti bool
	cmds    []redis.Cmd
	watches map[string]watchState
}

// watchState is the last known state of a watched key.
type watchState struct {
	exists  bool
	id      int
	version int
	mtime   int64
}

// push adds a command to the state.
func (s *connState) push(cmd redis.Cmd) {
	s.cmds = append(s.cmds, cmd)
}

// pop removes the last command from the state and returns it.
func (s *connState) pop() redis.Cmd {
	if len(s.cmds) == 0 {
		return nil
	}
	var last redis.Cmd
	s.cmds, last = s.cmds[:len(s.cmds)-1], s.cmds[len(s.cmds)-1]
	return last
}

// clear removes all commands from the state.
func (s *connState) clear() {
	s.cmds = []redis.Cmd{}
}

// watch records the baseline state of a key if it is not watched yet.
func (s *connState) watch(key string, state watchState) {
	if s.watches == nil {
		s.watches = make(map[string]watchState)
	}
	if _, ok := s.watches[key]; ok {
		return
	}
	s.watches[key] = state
}

// unwatch removes all watched keys.
func (s *connState) unwatch() {
	s.watches = nil
}

// String returns the string representation of the state.
func (s *connState) String() string {
	cmds := make([]string, len(s.cmds))
	for i, cmd := range s.cmds {
		cmds[i] = cmd.Name()
	}
	return fmt.Sprintf("[inMulti=%v,commands=%v]", s.inMulti, cmds)
}
