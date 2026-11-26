package rkey

import (
	"strings"
	"unicode/utf8"

	"github.com/nalgeon/redka/internal/core"
)

const maxKeyFilterTokens = 8

// keyFilter describes SQL conditions that can safely narrow the keys
// considered for a glob pattern before exact matching in Go.
type keyFilter struct {
	exact    string
	hasExact bool
	prefix   string
	suffix   string
	tokens   []string
}

func newKeyFilter(pattern string) keyFilter {
	if exact, ok := core.GlobLiteral(pattern); ok {
		return keyFilter{exact: exact, hasExact: true}
	}

	tokens := core.GlobTokens(pattern)
	if len(tokens) > maxKeyFilterTokens {
		tokens = tokens[:maxKeyFilterTokens]
	}
	return keyFilter{
		prefix: core.GlobPrefix(pattern),
		suffix: lastKeyPart(core.GlobSuffix(pattern)),
		tokens: tokens,
	}
}

// append adds filter conditions to query and returns the new query and args.
// The query must use ? placeholders. The caller is responsible for calling
// Dialect.Enumerate before executing the query.
func (f keyFilter) append(query string, args []any) (string, []any) {
	if f.hasExact {
		return query + " and key = ?", append(args, f.exact)
	}

	if f.prefix != "" {
		query += " and key >= ?"
		args = append(args, f.prefix)
		if upper, ok := prefixUpperBound(f.prefix); ok {
			query += " and key < ?"
			args = append(args, upper)
		}
	}

	if f.suffix != "" {
		reversed := reverse(f.suffix)
		query += " and id in (select kid from rkeypart where revpart >= ?"
		args = append(args, reversed)
		if upper, ok := prefixUpperBound(reversed); ok {
			query += " and revpart < ?"
			args = append(args, upper)
		}
		query += ")"
	}

	for _, token := range f.tokens {
		query += " and id in (select kid from rkeypart where part = ?)"
		args = append(args, token)
	}

	return query, args
}

func lastKeyPart(value string) string {
	if idx := strings.LastIndexAny(value, core.KeyPartSeparators); idx >= 0 {
		return value[idx+1:]
	}
	return value
}

func reverse(value string) string {
	runes := []rune(value)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// prefixUpperBound returns the smallest string greater than all strings
// with the given prefix. It returns false if no finite bound exists.
func prefixUpperBound(prefix string) (string, bool) {
	upper := []rune(prefix)
	for i := len(upper) - 1; i >= 0; i-- {
		if upper[i] < utf8.MaxRune {
			upper[i]++
			return string(upper[:i+1]), true
		}
	}
	return "", false
}
