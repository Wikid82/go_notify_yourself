package all

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
)

// wantProviderCount is the number of built-in provider packages this
// module ships. Bump this constant (and the corresponding blank import in
// all.go) whenever a new provider package is added — this test is the
// safety net for the one step in adding a provider that has no compiler
// enforcement (a provider that registers itself but isn't added to
// providers/all silently isn't part of the "one import gets everything"
// bundle).
const wantProviderCount = 8

func TestAll_RegistersEveryBuiltInProvider(t *testing.T) {
	types := notify.RegisteredTypes()
	if len(types) != wantProviderCount {
		t.Fatalf("expected %d registered provider types via providers/all, got %d: %v",
			wantProviderCount, len(types), types)
	}

	want := []string{"discord", "email", "gotify", "ntfy", "pushover", "slack", "telegram", "webhook"}
	registered := make(map[string]bool, len(types))
	for _, name := range types {
		registered[name] = true
	}
	for _, name := range want {
		if !registered[name] {
			t.Errorf("expected %q to be registered after importing providers/all, got %v", name, types)
		}
	}
}
