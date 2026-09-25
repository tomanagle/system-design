// Package shortcodetest provides a mock code generator for tests.
package shortcodetest

import (
	"sync"
	"testing"
)

// Generator returns configured codes and records how many times it is called.
// Use NewGenerator to create one. Its methods are safe for concurrent use.
type Generator struct {
	mu    sync.Mutex
	codes []string
	calls int
}

// NewGenerator returns codes in order, repeating the last code once exhausted.
// Supply one code to force repeated collisions, or several to simulate recovery.
func NewGenerator(first string, rest ...string) *Generator {
	return &Generator{codes: append([]string{first}, rest...)}
}

// Generate can be passed directly to dbstore.NewShortUrlStore.
func (g *Generator) Generate() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	code := g.codes[min(g.calls, len(g.codes)-1)]
	g.calls++
	return code
}

func (g *Generator) Calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func (g *Generator) AssertCalls(t testing.TB, want int) {
	t.Helper()
	if got := g.Calls(); got != want {
		t.Errorf("generator called %d times; want %d", got, want)
	}
}
