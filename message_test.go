package notify

import (
	"testing"
	"time"
)

func TestMessageNormalizedDefaultsZeroTimestamp(t *testing.T) {
	msg := Message{Title: "hi"}
	got := msg.Normalized()
	if got.Timestamp.IsZero() {
		t.Fatal("expected Normalized() to default a zero Timestamp to time.Now()")
	}
	if !msg.Timestamp.IsZero() {
		t.Fatal("expected Normalized() to not mutate the original Message")
	}
}

func TestMessageNormalizedPreservesExistingTimestamp(t *testing.T) {
	want := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := Message{Title: "hi", Timestamp: want}
	got := msg.Normalized()
	if !got.Timestamp.Equal(want) {
		t.Fatalf("expected timestamp to be preserved, got %v want %v", got.Timestamp, want)
	}
}
