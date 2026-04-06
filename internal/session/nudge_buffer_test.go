package session

import "testing"

func TestNudgeBuffer_AddAndCount(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, "hello", "hi there")
	b.AddTurn(1, "how are you", "good")
	b.AddTurn(2, "other chat", "response")

	if got := b.TurnCount(1); got != 2 {
		t.Errorf("TurnCount(1) = %d, want 2", got)
	}
	if got := b.TurnCount(2); got != 1 {
		t.Errorf("TurnCount(2) = %d, want 1", got)
	}
	if got := b.TurnCount(999); got != 0 {
		t.Errorf("TurnCount(999) = %d, want 0", got)
	}
}

func TestNudgeBuffer_GetAndReset(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, "msg1", "resp1")
	b.AddTurn(1, "msg2", "resp2")

	msgs := b.GetAndReset(1)
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
	if got := b.TurnCount(1); got != 0 {
		t.Errorf("TurnCount after reset = %d, want 0", got)
	}
	if msgs := b.GetAndReset(1); msgs != nil {
		t.Errorf("GetAndReset after reset = %v, want nil", msgs)
	}
}

func TestNudgeBuffer_IsolatedChats(t *testing.T) {
	b := NewNudgeBuffer()

	b.AddTurn(1, "chat1", "resp1")
	b.AddTurn(2, "chat2", "resp2")

	// Reset chat 1, chat 2 should be unaffected
	b.GetAndReset(1)

	if got := b.TurnCount(2); got != 1 {
		t.Errorf("TurnCount(2) after reset(1) = %d, want 1", got)
	}
}
