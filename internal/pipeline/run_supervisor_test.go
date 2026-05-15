package pipeline

import "testing"

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
