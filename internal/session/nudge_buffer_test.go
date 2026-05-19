package session

import "testing"

func TestNudgeBuffer_AddAndCount(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, "hello", "hi there")
	b.AddTurn(1, 0, "how are you", "good")
	b.AddTurn(2, 0, "other chat", "response")

	if got := b.TurnCount(1, 0); got != 2 {
		t.Errorf("TurnCount(1) = %d, want 2", got)
	}
	if got := b.TurnCount(2, 0); got != 1 {
		t.Errorf("TurnCount(2) = %d, want 1", got)
	}
	if got := b.TurnCount(999, 0); got != 0 {
		t.Errorf("TurnCount(999) = %d, want 0", got)
	}
}

func TestNudgeBuffer_GetAndReset(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, "msg1", "resp1")
	b.AddTurn(1, 0, "msg2", "resp2")

	msgs := b.GetAndReset(1, 0)
	if len(msgs) != 4 { // 2 turns × 2 messages each
		t.Fatalf("GetAndReset returned %d messages, want 4", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "msg1" {
		t.Errorf("first message = %+v, want user/msg1", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "resp1" {
		t.Errorf("second message = %+v, want assistant/resp1", msgs[1])
	}

	// Buffer should be empty after reset
	if got := b.TurnCount(1, 0); got != 0 {
		t.Errorf("TurnCount after reset = %d, want 0", got)
	}
	if msgs := b.GetAndReset(1, 0); msgs != nil {
		t.Errorf("GetAndReset after reset = %v, want nil", msgs)
	}
}

func TestNudgeBuffer_IsolatedChats(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 0, "chat1", "resp1")
	b.AddTurn(2, 0, "chat2", "resp2")

	// Reset chat 1, chat 2 should be unaffected
	b.GetAndReset(1, 0)

	if got := b.TurnCount(2, 0); got != 1 {
		t.Errorf("TurnCount(2) after reset(1) = %d, want 1", got)
	}
}

func TestNudgeBuffer_IsolatedThreads(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, 10, "topic10", "resp10")
	b.AddTurn(1, 20, "topic20", "resp20")
	b.GetAndReset(1, 10)

	if got := b.TurnCount(1, 20); got != 1 {
		t.Errorf("TurnCount(1,20) after reset(1,10) = %d, want 1", got)
	}
	if got := b.TurnCount(1, 10); got != 0 {
		t.Errorf("TurnCount(1,10) after reset = %d, want 0", got)
	}
}

func TestNudgeBuffer_SnapshotPreservesBuffer(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, "hello", "hi")
	b.AddTurn(1, 0, "how are you", "good")

	snap, ver := b.Snapshot(1, 0)
	if len(snap) != 4 {
		t.Fatalf("Snapshot returned %d messages, want 4", len(snap))
	}
	if ver == 0 {
		t.Fatal("expected non-zero version")
	}

	// Buffer should still be intact
	if got := b.TurnCount(1, 0); got != 2 {
		t.Errorf("TurnCount after Snapshot = %d, want 2", got)
	}
	if got := len(b.GetAndReset(1, 0)); got != 4 {
		t.Errorf("buffer after Snapshot = %d messages, want 4", got)
	}
}

func TestNudgeBuffer_CommitPartial(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, "q1", "a1")
	b.AddTurn(1, 0, "q2", "a2")
	b.AddTurn(1, 0, "q3", "a3")

	msgs, ver := b.Snapshot(1, 0)
	// Commit 2 messages (1 turn) — should leave 2 turns (4 messages)
	b.Commit(1, 0, ver, 2)

	if got := b.TurnCount(1, 0); got != 2 {
		t.Errorf("TurnCount after partial commit = %d, want 2", got)
	}
	msgs2, _ := b.Snapshot(1, 0)
	if len(msgs2) != 4 {
		t.Fatalf("messages after partial commit = %d, want 4", len(msgs2))
	}
	if msgs2[0].Content != "q2" || msgs2[2].Content != "q3" {
		t.Fatalf("remaining messages out of order: %+v", msgs2)
	}
	_ = msgs
}

func TestNudgeBuffer_CommitAll(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, "q1", "a1")
	b.AddTurn(1, 0, "q2", "a2")

	_, ver := b.Snapshot(1, 0)
	b.Commit(1, 0, ver, 10) // more than available

	if got := b.TurnCount(1, 0); got != 0 {
		t.Errorf("TurnCount after full commit = %d, want 0", got)
	}
	if msgs, _ := b.Snapshot(1, 0); msgs != nil {
		t.Errorf("expected nil after full commit, got %d messages", len(msgs))
	}
}

func TestNudgeBuffer_CommitNoop(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, "q1", "a1")

	_, ver := b.Snapshot(1, 0)
	b.Commit(1, 0, ver, 0) // count=0 should be no-op

	if got := b.TurnCount(1, 0); got != 1 {
		t.Errorf("TurnCount after noop commit = %d, want 1", got)
	}
}

func TestNudgeBuffer_SnapshotCommitPreservesOtherThreads(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 10, "t10", "r10")
	b.AddTurn(1, 20, "t20", "r20")

	snap, ver := b.Snapshot(1, 10)
	b.Commit(1, 10, ver, 2)

	// Thread 20 should be unaffected
	if got := b.TurnCount(1, 20); got != 1 {
		t.Errorf("TurnCount(1,20) = %d, want 1", got)
	}
	_ = snap
}

func TestNudgeBuffer_StaleCommitSkipped(t *testing.T) {
	b := NewNudgeBuffer()
	b.AddTurn(1, 0, "q1", "a1")

	_, ver := b.Snapshot(1, 0)

	// Buffer modified before commit — version changed
	b.AddTurn(1, 0, "q2", "a2")

	// Attempt commit with stale version — should be silently skipped
	b.Commit(1, 0, ver, 2)

	// Buffer should still have both turns
	if got := b.TurnCount(1, 0); got != 2 {
		t.Errorf("TurnCount after stale commit = %d, want 2", got)
	}
}

func TestNudgeBuffer_CapDropsOldest(t *testing.T) {
	b := NewNudgeBuffer()
	// Fill to just under cap, then add one more pair to exceed
	for i := 0; i < 19; i++ {
		b.AddTurn(1, 0, "pre", "pre-resp")
	}
	if got := b.TurnCount(1, 0); got != 19 {
		t.Fatalf("expected 19 turns, got %d", got)
	}

	// Snapshot at cap boundary
	snapAtCap, verAtCap := b.Snapshot(1, 0)
	if len(snapAtCap) != 38 {
		t.Fatalf("expected 38 messages before final add, got %d", len(snapAtCap))
	}

	// Now add one more — should trigger cap. 20 turns = 40 messages, at cap.
	b.AddTurn(1, 0, "final", "final-answer")
	if got := b.TurnCount(1, 0); got != 20 {
		t.Fatalf("expected 20 turns (at cap), got %d", got)
	}

	// Add beyond cap — oldest should be dropped
	b.AddTurn(1, 0, "extra", "extra-answer")
	if got := b.TurnCount(1, 0); got != 20 {
		t.Fatalf("expected 20 turns (capped), got %d", got)
	}

	msgs, ver := b.Snapshot(1, 0)
	if len(msgs) != 40 {
		t.Fatalf("expected 40 messages (capped), got %d", len(msgs))
	}
	// Last assistant should be extra-answer
	if msgs[39].Content != "extra-answer" {
		t.Fatalf("expected last message 'extra-answer', got %q", msgs[39].Content)
	}

	// Stale commit from prior snapshot should be skipped
	b.Commit(1, 0, verAtCap, len(snapAtCap))
	if got := b.TurnCount(1, 0); got != 20 {
		t.Errorf("stale commit should be skipped, got %d turns", got)
	}

	// Fresh commit from current snapshot should work
	b.Commit(1, 0, ver, len(msgs))
	if got := b.TurnCount(1, 0); got != 0 {
		t.Errorf("fresh commit should clear buffer, got %d turns", got)
	}
}
