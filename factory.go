package notify

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Factory constructs a Sender from a generic configuration map. Each
// provider package's factory type-asserts the keys/types it expects out of
// config and returns a descriptive error for anything missing or
// wrong-typed — Factory implementations must never panic on bad input from
// a caller (panicking is reserved for Register's own misuse-by-programmer
// checks, per the database/sql convention — see below).
//
// Well-known convention (documented per-package in each provider's doc
// comment and in ARCHITECTURE.md): HTTP-based providers expect a
// "transport" key holding the shared *transport.Wrapper; provider-specific
// Config fields are expected under their lowercase snake_case field name
// (e.g. discord's WebhookURL -> config["webhook_url"]). This module makes
// no attempt to enforce these conventions structurally.
type Factory func(config map[string]any) (Sender, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register makes a provider Factory available under name (case-insensitive;
// stored lowercased). Intended to be called from a provider package's
// init(), mirroring database/sql.Register and image.RegisterFormat.
//
// Register panics if factory is nil or if name is already registered —
// exactly like sql.Register — because a duplicate/nil registration is
// always a programmer error discoverable at package-init time (e.g. two
// packages both claiming "webhook"), never a legitimate runtime condition
// a caller should have to handle.
func Register(name string, factory Factory) {
	if factory == nil {
		panic("notify: Register called with nil Factory for " + name)
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		panic("notify: Register called with empty name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[key]; exists {
		panic("notify: Register called twice for provider " + key)
	}
	registry[key] = factory
}

// New looks up the Factory registered under name (case-insensitive) and
// invokes it with config. Returns an error — never panics — if name is not
// registered or if the factory itself returns an error (e.g. a missing
// required config key).
func New(name string, config map[string]any) (Sender, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	registryMu.RLock()
	factory, ok := registry[key]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("notify: no provider registered for type %q (registered types: %s)",
			name, strings.Join(RegisteredTypes(), ", "))
	}
	return factory(config)
}

// RegisteredTypes returns the sorted list of currently registered provider
// type names. Useful for a host application that wants to validate a
// config value or populate a UI dropdown against exactly what's compiled
// in, without hardcoding its own list.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
