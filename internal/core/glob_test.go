package core_test

import (
	"testing"

	"github.com/nalgeon/be"
	"github.com/nalgeon/redka/internal/core"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		key     string
		want    bool
	}{
		{"all", "*", "user:1", true},
		{"prefix", "user:*", "user:1", true},
		{"suffix", "*:1", "user:1", true},
		{"middle", "user:*:profile", "user:1:profile", true},
		{"middle no match", "user:*:profile", "user:1:settings", false},
		{"one char", "user:?:profile", "user:1:profile", true},
		{"one char no match", "user:?:profile", "user:12:profile", false},
		{"class", "user:[12]:profile", "user:2:profile", true},
		{"class no match", "user:[12]:profile", "user:3:profile", false},
		{"negated class", "user:[^1]:profile", "user:2:profile", true},
		{"negated class no match", "user:[^1]:profile", "user:1:profile", false},
		{"bang negated class", "user:[!1]:profile", "user:2:profile", true},
		{"bang negated class no match", "user:[!1]:profile", "user:1:profile", false},
		{"escaped star", `literal\*key`, "literal*key", true},
		{"escaped star no match", `literal\*key`, "literalXkey", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			be.Equal(t, core.MatchGlob(test.pattern, test.key), test.want)
		})
	}
}

func TestGlobPlan(t *testing.T) {
	t.Run("prefix", func(t *testing.T) {
		be.Equal(t, core.GlobPrefix("user:*:profile"), "user:")
		be.Equal(t, core.GlobPrefix("[12]:profile"), "")
	})
	t.Run("suffix", func(t *testing.T) {
		be.Equal(t, core.GlobSuffix("user:*:profile"), ":profile")
		be.Equal(t, core.GlobSuffix("user:*"), "")
	})
	t.Run("literal", func(t *testing.T) {
		literal, ok := core.GlobLiteral(`literal\*key`)
		be.Equal(t, ok, true)
		be.Equal(t, literal, "literal*key")

		_, ok = core.GlobLiteral("user:*")
		be.Equal(t, ok, false)
	})
	t.Run("tokens", func(t *testing.T) {
		be.Equal(t, core.GlobTokens("user:*:profile"), []string{"user", "profile"})
		be.Equal(t, core.GlobTokens("*profile"), []string(nil))
		be.Equal(t, core.GlobTokens("*:profile"), []string{"profile"})
		be.Equal(t, core.GlobTokens("user:*"), []string{"user"})
		be.Equal(t, core.GlobTokens("*user:*"), []string(nil))
	})
}
