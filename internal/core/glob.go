package core

// KeyPartSeparators contains the characters used to split keys into
// searchable parts. Keep this in sync with the rkeypart triggers in the
// SQLite and Postgres schemas.
const KeyPartSeparators = ":/._-|"

type globAtomKind int

const (
	globLiteral globAtomKind = iota
	globAny
	globStar
	globClass
)

type globAtom struct {
	kind globAtomKind
	ch   byte
}

// MatchGlob reports whether key matches the glob-style pattern.
// The matcher implements Redis glob semantics independently of the
// database backend: *, ?, [...] and backslash escapes.
func MatchGlob(pattern, key string) bool {
	return matchGlob([]byte(pattern), []byte(key))
}

// GlobPrefix returns the literal prefix before the first wildcard.
func GlobPrefix(pattern string) string {
	atoms := parseGlob(pattern)
	prefix := make([]byte, 0, len(pattern))
	for _, atom := range atoms {
		if atom.kind != globLiteral {
			break
		}
		prefix = append(prefix, atom.ch)
	}
	return string(prefix)
}

// GlobSuffix returns the literal suffix after the last wildcard.
func GlobSuffix(pattern string) string {
	atoms := parseGlob(pattern)
	start := len(atoms)
	for start > 0 && atoms[start-1].kind == globLiteral {
		start--
	}
	suffix := make([]byte, 0, len(atoms)-start)
	for _, atom := range atoms[start:] {
		suffix = append(suffix, atom.ch)
	}
	return string(suffix)
}

// GlobLiteral returns the literal key represented by pattern and true
// if pattern contains no wildcards or character classes.
func GlobLiteral(pattern string) (string, bool) {
	atoms := parseGlob(pattern)
	literal := make([]byte, 0, len(pattern))
	for _, atom := range atoms {
		if atom.kind != globLiteral {
			return "", false
		}
		literal = append(literal, atom.ch)
	}
	return string(literal), true
}

// GlobTokens returns complete key parts mentioned literally in pattern.
// A part is complete only when both of its sides are delimiters or the
// pattern boundaries. This makes it safe to use the returned tokens as
// exact-match prefilters for the key part index.
func GlobTokens(pattern string) []string {
	atoms := parseGlob(pattern)
	var tokens []string
	seen := make(map[string]struct{})

	for i := 0; i < len(atoms); {
		if atoms[i].kind != globLiteral {
			i++
			continue
		}

		start := i
		for i < len(atoms) && atoms[i].kind == globLiteral {
			i++
		}
		end := i

		leftBoundary := start == 0
		rightBoundary := end == len(atoms)
		run := make([]byte, 0, end-start)
		for j := start; j < end; j++ {
			run = append(run, atoms[j].ch)
		}

		partStart := 0
		for j := 0; j <= len(run); j++ {
			if j < len(run) && !isKeyPartSeparator(run[j]) {
				continue
			}
			if j > partStart {
				left := leftBoundary || partStart > 0
				right := rightBoundary || j < len(run)
				if left && right {
					token := string(run[partStart:j])
					if _, ok := seen[token]; !ok {
						seen[token] = struct{}{}
						tokens = append(tokens, token)
					}
				}
			}
			partStart = j + 1
		}
	}

	return tokens
}

func isKeyPartSeparator(ch byte) bool {
	for i := 0; i < len(KeyPartSeparators); i++ {
		if KeyPartSeparators[i] == ch {
			return true
		}
	}
	return false
}

func parseGlob(pattern string) []globAtom {
	atoms := make([]globAtom, 0, len(pattern))
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				atoms = append(atoms, globAtom{kind: globLiteral, ch: pattern[i+1]})
				i += 2
			} else {
				atoms = append(atoms, globAtom{kind: globLiteral, ch: '\\'})
				i++
			}
		case '*':
			atoms = append(atoms, globAtom{kind: globStar})
			i++
		case '?':
			atoms = append(atoms, globAtom{kind: globAny})
			i++
		case '[':
			end, ok := globClassEnd(pattern, i)
			if !ok {
				atoms = append(atoms, globAtom{kind: globLiteral, ch: '['})
				i++
				continue
			}
			atoms = append(atoms, globAtom{kind: globClass})
			i = end
		default:
			atoms = append(atoms, globAtom{kind: globLiteral, ch: pattern[i]})
			i++
		}
	}
	return atoms
}

func globClassEnd(pattern string, start int) (int, bool) {
	for i := start + 1; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
		case ']':
			return i + 1, true
		}
	}
	return 0, false
}

func matchGlob(pattern, key []byte) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(key); i++ {
				if matchGlob(pattern, key[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(key) == 0 {
				return false
			}
			pattern = pattern[1:]
			key = key[1:]
		case '[':
			class, rest, ok := splitGlobClass(pattern)
			if !ok {
				if len(key) == 0 || key[0] != '[' {
					return false
				}
				pattern = pattern[1:]
				key = key[1:]
				continue
			}
			if len(key) == 0 || !matchGlobClass(class, key[0]) {
				return false
			}
			pattern = rest
			key = key[1:]
		case '\\':
			if len(pattern) > 1 {
				pattern = pattern[1:]
			}
			fallthrough
		default:
			if len(key) == 0 || pattern[0] != key[0] {
				return false
			}
			pattern = pattern[1:]
			key = key[1:]
		}
	}
	return len(key) == 0
}

func splitGlobClass(pattern []byte) (class []byte, rest []byte, ok bool) {
	for i := 1; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
		case ']':
			return pattern[1:i], pattern[i+1:], true
		}
	}
	return nil, nil, false
}

func matchGlobClass(class []byte, ch byte) bool {
	if len(class) == 0 {
		return false
	}

	negate := class[0] == '^' || class[0] == '!'
	if negate {
		class = class[1:]
	}

	matched := false
	for i := 0; i < len(class); {
		start := class[i]
		if start == '\\' && i+1 < len(class) {
			i++
			start = class[i]
		}
		i++

		if i+1 < len(class) && class[i] == '-' {
			i++
			end := class[i]
			if end == '\\' && i+1 < len(class) {
				i++
				end = class[i]
			}
			i++
			if start > end {
				start, end = end, start
			}
			if ch >= start && ch <= end {
				matched = true
			}
			continue
		}

		if ch == start {
			matched = true
		}
	}

	if negate {
		return !matched
	}
	return matched
}
