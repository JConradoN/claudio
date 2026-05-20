package pipeline

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassifyConcurrentMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want concurrentMessageKind
	}{
		{name: "cancel exact", text: "para", want: concurrentCancel},
		{name: "cancel phrase", text: "não precisa mais", want: concurrentCancel},
		{name: "supersede correction", text: "na verdade faça diferente", want: concurrentSupersede},
		{name: "supersede topic", text: "estou no tópico errado", want: concurrentSupersede},
		{name: "status", text: "conseguiu testar?", want: concurrentStatus},
		{name: "enqueue", text: "depois veja a previsão do tempo", want: concurrentEnqueue},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyConcurrentMessage(tc.text); got != tc.want {
				t.Fatalf("classifyConcurrentMessage(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestRunSupervisorSerializesByChatThread(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	input := pipelineInput{chatID: 1, threadID: 2, text: "first"}
	run, admission, _ := rs.admit(input)
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start with run", run, admission)
	}

	second := pipelineInput{chatID: 1, threadID: 2, text: "second"}
	next, admission, active := rs.admit(second)
	if admission != admitQueued || next != nil || active == nil || active.id != run.id {
		t.Fatalf("second admit = (%v, %v, %v), want queued against active", next, admission, active)
	}

	nextRun, nextInput := rs.finish(run)
	if nextRun == nil || nextInput == nil || nextInput.text != "second" {
		t.Fatalf("finish next = (%v, %v), want queued second", nextRun, nextInput)
	}
}

func TestQueueMessagesIncludeActiveContext(t *testing.T) {
	t.Parallel()

	active := &activeRun{prompt: "gerando relatório", startedAt: time.Now().Add(-2 * time.Minute)}
	queued := queueAdmittedMessage(active, 1)
	if !strings.Contains(queued, "gerando relatório") || !strings.Contains(queued, "próxima") {
		t.Fatalf("queued message should include active context and position, got %q", queued)
	}
	status := queueStatusMessage(active, 1)
	if !strings.Contains(status, "gerando relatório") || !strings.Contains(status, "rodando") || !strings.Contains(status, "Fila: 1") {
		t.Fatalf("status message should include active context and queue size, got %q", status)
	}
}

func TestRunSupervisorQueueInfo(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 7, ThreadID: 11, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "primeiro"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}
	_, admission, _ = rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "segundo"})
	if admission != admitQueued {
		t.Fatalf("second admission = %v, want queued", admission)
	}
	if got := rs.queueSize(key); got != 1 {
		t.Fatalf("queueSize() = %d, want 1", got)
	}
	if got := rs.activeDescription(key); !strings.Contains(got, "primeiro") {
		t.Fatalf("activeDescription() = %q, want active prompt", got)
	}
}

func TestRunSupervisorCancelStopsActiveAndDropsQueue(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 1, ThreadID: 2, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}
	_, admission, _ = rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "second"})
	if admission != admitQueued {
		t.Fatalf("second admission = %v, want queued", admission)
	}
	if !rs.cancel(key) {
		t.Fatal("cancel should report active run")
	}
	if run.ctx.Err() == nil {
		t.Fatal("active run should be canceled")
	}
	if got := rs.queueSize(key); got != 0 {
		t.Fatalf("queueSize() after cancel = %d, want 0", got)
	}
}

func TestRunSupervisorStatusNoActive(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 999, ThreadID: 0, UserID: 0}

	// No active run for this key — status should not panic and return empty.
	desc, size := rs.status(key)
	if desc != "" {
		t.Fatalf("expected empty description, got %q", desc)
	}
	if size != 0 {
		t.Fatalf("expected queue size 0, got %d", size)
	}
}

func TestRunSupervisorSupersedeCancelsAndReplacesQueue(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	run, admission, _ := rs.admit(pipelineInput{chatID: 1, threadID: 0, text: "long task"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}

	_, admission, _ = rs.admit(pipelineInput{chatID: 1, threadID: 0, text: "na verdade faça só teste"})
	if admission != admitSupersede {
		t.Fatalf("supersede admission = %v, want %v", admission, admitSupersede)
	}
	if run.ctx.Err() == nil {
		t.Fatal("active run should be canceled by supersede")
	}

	nextRun, nextInput := rs.finish(run)
	if nextRun == nil || nextInput == nil || nextInput.text != "na verdade faça só teste" {
		t.Fatalf("finish next = (%v, %v), want supersede input", nextRun, nextInput)
	}
}

func TestRunSupervisorQueueUpToThree(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 5, ThreadID: 1, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}

	// Queue 3 follow-ups
	var admits []admissionKind
	for i := 0; i < 3; i++ {
		text := fmt.Sprintf("followup-%d", i+1)
		_, adm, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: text})
		admits = append(admits, adm)
	}
	for i, adm := range admits {
		if adm != admitQueued {
			t.Fatalf("followup %d admission = %v, want admitQueued", i+1, adm)
		}
	}

	if got := rs.queueSize(key); got != 3 {
		t.Fatalf("queueSize() = %d, want 3", got)
	}

	// Verify FIFO order by finishing and checking each dequeued text
	expected := []string{"followup-1", "followup-2", "followup-3"}
	for i, want := range expected {
		nextRun, nextInput := rs.finish(run)
		if nextRun == nil || nextInput == nil {
			t.Fatalf("finish iteration %d returned nil, want %q", i, want)
		}
		if nextInput.text != want {
			t.Fatalf("finish iteration %d text = %q, want %q", i, nextInput.text, want)
		}
		run = nextRun // advance to the newly started run
	}

	// All items dequeued — finish should return nil
	finalRun, finalInput := rs.finish(run)
	if finalRun != nil || finalInput != nil {
		t.Fatalf("finish after draining queue = (%v, %v), want (nil, nil)", finalRun, finalInput)
	}

	if got := rs.queueSize(key); got != 0 {
		t.Fatalf("queueSize() after drain = %d, want 0", got)
	}
}

func TestRunSupervisorQueueFull(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 5, ThreadID: 2, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}

	// Fill queue to capacity
	for i := 0; i < maxQueueDepth; i++ {
		text := fmt.Sprintf("fill-%d", i+1)
		_, adm, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: text})
		if adm != admitQueued {
			t.Fatalf("fill %d admission = %v, want admitQueued", i+1, adm)
		}
	}

	// 4th should be rejected
	_, adm, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "too-many"})
	if adm != admitQueueFull {
		t.Fatalf("4th admission = %v, want admitQueueFull", adm)
	}

	if got := rs.queueSize(key); got != maxQueueDepth {
		t.Fatalf("queueSize() after full rejection = %d, want %d", got, maxQueueDepth)
	}
}

func TestRunSupervisorSupersedeClearsQueue(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 5, ThreadID: 3, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}

	// Queue two follow-ups
	_, adm, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "queued-1"})
	if adm != admitQueued {
		t.Fatalf("queued-1 admission = %v, want admitQueued", adm)
	}
	_, adm, _ = rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "queued-2"})
	if adm != admitQueued {
		t.Fatalf("queued-2 admission = %v, want admitQueued", adm)
	}

	// Supersede should clear queue and replace with just the superseding message
	_, adm, _ = rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "na verdade troque tudo"})
	if adm != admitSupersede {
		t.Fatalf("supersede admission = %v, want admitSupersede", adm)
	}

	if got := rs.queueSize(key); got != 1 {
		t.Fatalf("queueSize() after supersede = %d, want 1", got)
	}

	// Finish should give us the superseding message, not queued-1
	nextRun, nextInput := rs.finish(run)
	if nextRun == nil || nextInput == nil || nextInput.text != "na verdade troque tudo" {
		t.Fatalf("finish after supersede = (%v, %q), want supersede message", nextRun, nextInput.text)
	}
}

func TestRunSupervisorCancelClearsQueue(t *testing.T) {
	t.Parallel()

	rs := newRunSupervisor()
	key := runKey{ChatID: 5, ThreadID: 4, UserID: 0}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}

	// Queue a few messages
	for i := 0; i < 3; i++ {
		text := fmt.Sprintf("queued-%d", i+1)
		_, adm, _ := rs.admit(pipelineInput{chatID: key.ChatID, threadID: key.ThreadID, text: text})
		if adm != admitQueued {
			t.Fatalf("queued-%d admission = %v, want admitQueued", i+1, adm)
		}
	}

	if got := rs.queueSize(key); got != 3 {
		t.Fatalf("queueSize() before cancel = %d, want 3", got)
	}

	if !rs.cancel(key) {
		t.Fatal("cancel should report active run")
	}

	if got := rs.queueSize(key); got != 0 {
		t.Fatalf("queueSize() after cancel = %d, want 0", got)
	}

	// Further finish should not start a queued item
	nextRun, nextInput := rs.finish(run)
	if nextRun != nil || nextInput != nil {
		t.Fatalf("finish after cancel = (%v, %v), want (nil, nil)", nextRun, nextInput)
	}
}
