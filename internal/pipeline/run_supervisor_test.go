package pipeline

import (
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
	queued := queueAdmittedMessage(active)
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
	key := runKey{chatID: 7, threadID: 11}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.chatID, threadID: key.threadID, text: "primeiro"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}
	_, admission, _ = rs.admit(pipelineInput{chatID: key.chatID, threadID: key.threadID, text: "segundo"})
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
	key := runKey{chatID: 1, threadID: 2}
	run, admission, _ := rs.admit(pipelineInput{chatID: key.chatID, threadID: key.threadID, text: "first"})
	if admission != admitStart || run == nil {
		t.Fatalf("first admit = (%v, %v), want start", run, admission)
	}
	_, admission, _ = rs.admit(pipelineInput{chatID: key.chatID, threadID: key.threadID, text: "second"})
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
