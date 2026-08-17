package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeSender is a minimal notify.Sender used only by factory_test.go to
// exercise the registry without depending on any providers/* package (this
// package must not import its own consumers).
type fakeSender struct{ name string }

func (f *fakeSender) Send(context.Context, Message) error { return nil }

var _ Sender = (*fakeSender)(nil)

func TestRegister_PanicsOnNilFactory(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Register(nil factory) to panic, it did not")
		}
	}()
	Register("factory-test-nil", nil)
}

func TestRegister_PanicsOnEmptyName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Register(\"\") to panic, it did not")
		}
	}()
	Register("   ", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	Register("factory-test-dup", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected second Register call for the same name to panic, it did not")
		}
	}()
	Register("factory-test-dup", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })
}

func TestRegister_CaseInsensitiveLookup(t *testing.T) {
	Register("Factory-Test-Case", func(config map[string]any) (Sender, error) {
		return &fakeSender{name: "case"}, nil
	})

	sender, err := New("FACTORY-TEST-CASE", nil)
	if err != nil {
		t.Fatalf("New with different case returned error: %v", err)
	}
	if sender == nil {
		t.Fatal("expected a non-nil Sender")
	}
}

func TestNew_UnregisteredNameReturnsErrorNotPanic(t *testing.T) {
	sender, err := New("factory-test-does-not-exist", map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("expected an error for an unregistered provider name, got nil")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender on error, got %#v", sender)
	}
	if !strings.Contains(err.Error(), "factory-test-does-not-exist") {
		t.Errorf("expected error to mention the requested name, got: %v", err)
	}
}

func TestNew_UnregisteredNameErrorListsRegisteredTypes(t *testing.T) {
	Register("factory-test-listed", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })

	_, err := New("factory-test-not-listed", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "factory-test-listed") {
		t.Errorf("expected error to list currently-registered types, got: %v", err)
	}
}

func TestNew_CallsFactoryAndReturnsSender(t *testing.T) {
	var receivedConfig map[string]any
	Register("factory-test-calls", func(config map[string]any) (Sender, error) {
		receivedConfig = config
		return &fakeSender{name: "calls"}, nil
	})

	cfg := map[string]any{"webhook_url": "https://example.com"}
	sender, err := New("factory-test-calls", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fs, ok := sender.(*fakeSender)
	if !ok || fs.name != "calls" {
		t.Fatalf("expected the registered factory's Sender to be returned, got %#v", sender)
	}
	if receivedConfig["webhook_url"] != "https://example.com" {
		t.Errorf("expected config to be passed through to the factory unchanged, got %#v", receivedConfig)
	}
}

func TestNew_FactoryErrorIsPropagated(t *testing.T) {
	Register("factory-test-errors", func(map[string]any) (Sender, error) {
		return nil, fmt.Errorf("factory-test: missing required key")
	})

	sender, err := New("factory-test-errors", nil)
	if err == nil {
		t.Fatal("expected the factory's error to propagate")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender when the factory errors, got %#v", sender)
	}
}

func TestRegisteredTypes_SortedAndReflectsRegistrations(t *testing.T) {
	Register("factory-test-zzz", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })
	Register("factory-test-aaa", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })

	types := RegisteredTypes()

	var zIdx, aIdx = -1, -1
	for i, name := range types {
		switch name {
		case "factory-test-zzz":
			zIdx = i
		case "factory-test-aaa":
			aIdx = i
		}
	}
	if aIdx == -1 || zIdx == -1 {
		t.Fatalf("expected both registered names present in %v", types)
	}
	if aIdx > zIdx {
		t.Errorf("expected RegisteredTypes to be sorted ascending, got %v", types)
	}

	for i := 1; i < len(types); i++ {
		if types[i-1] > types[i] {
			t.Fatalf("RegisteredTypes not fully sorted: %v", types)
		}
	}
}

func TestRegistry_ConcurrentReadsAreSafe(t *testing.T) {
	Register("factory-test-concurrent", func(map[string]any) (Sender, error) { return &fakeSender{}, nil })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = New("factory-test-concurrent", nil)
		}()
		go func() {
			defer wg.Done()
			_ = RegisteredTypes()
		}()
	}
	wg.Wait()
}
