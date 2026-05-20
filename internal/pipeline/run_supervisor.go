package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/session"
)

type pipelineInput struct {
	chatID    int64
	threadID  int
	messageID int
	userID    int64
	text      string
	images    []bridge.ImageAttachment
}

type runKey = session.SessionKey

type activeRun struct {
	id        uint64
	key       runKey
	prompt    string
	startedAt time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
}

type admissionKind int

const (
	admitStart admissionKind = iota
	admitCancelOnly
	admitSupersede
	admitStatus
	admitQueued
	admitQueueFull
)

const maxQueueDepth = 3

type runSupervisor struct {
	mu     sync.Mutex
	nextID uint64
	active map[runKey]*activeRun
	queued map[runKey][]pipelineInput
}

func newRunSupervisor() *runSupervisor {
	return &runSupervisor{
		active: make(map[runKey]*activeRun, 16),
		queued: make(map[runKey][]pipelineInput, 16),
	}
}

func (rs *runSupervisor) admit(input pipelineInput) (*activeRun, admissionKind, *activeRun) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	key := runKey{ChatID: input.chatID, ThreadID: input.threadID, UserID: input.userID}
	current := rs.active[key]
	if current == nil {
		return rs.startLocked(key, input), admitStart, nil
	}

	switch classifyConcurrentMessage(input.text) {
	case concurrentCancel:
		current.cancel()
		delete(rs.queued, key)
		return nil, admitCancelOnly, current
	case concurrentStatus:
		return nil, admitStatus, current
	case concurrentSupersede:
		current.cancel()
		rs.queued[key] = []pipelineInput{input}
		return nil, admitSupersede, current
	default:
		queue := rs.queued[key]
		if len(queue) >= maxQueueDepth {
			return nil, admitQueueFull, current
		}
		rs.queued[key] = append(queue, input)
		return nil, admitQueued, current
	}
}

func (rs *runSupervisor) cancel(key runKey) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	current := rs.active[key]
	if current == nil {
		delete(rs.queued, key)
		return false
	}
	current.cancel()
	delete(rs.queued, key)
	return true
}

// cancelAllForUser cancels all active runs for a given user across all chats/threads.
// Returns true if at least one run was cancelled.
func (rs *runSupervisor) cancelAllForUser(userID int64) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	cancelled := false
	for key, run := range rs.active {
		if key.UserID == userID {
			run.cancel()
			delete(rs.active, key)
			delete(rs.queued, key)
			cancelled = true
		}
	}
	// Also clean up stale queued entries for this user
	for key := range rs.queued {
		if key.UserID == userID {
			if _, active := rs.active[key]; !active {
				delete(rs.queued, key)
			}
		}
	}
	return cancelled
}

func (rs *runSupervisor) activeDescription(key runKey) string {
	description, _ := rs.status(key)
	return description
}

func (rs *runSupervisor) queueSize(key runKey) int {
	_, size := rs.status(key)
	return size
}

func (rs *runSupervisor) status(key runKey) (string, int) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	queueSize := len(rs.queued[key])
	run := rs.active[key]
	if run == nil {
		return "", queueSize
	}
	return run.description(), queueSize
}

func (rs *runSupervisor) finish(run *activeRun) (*activeRun, *pipelineInput) {
	if run == nil {
		return nil, nil
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()
	defer close(run.done)

	current := rs.active[run.key]
	if current == nil || current.id != run.id {
		return nil, nil
	}
	delete(rs.active, run.key)

	queue, ok := rs.queued[run.key]
	if !ok || len(queue) == 0 {
		delete(rs.queued, run.key)
		return nil, nil
	}

	nextInput := queue[0]
	if len(queue) == 1 {
		delete(rs.queued, run.key)
	} else {
		rs.queued[run.key] = queue[1:]
	}

	next := rs.startLocked(run.key, nextInput)
	return next, &nextInput
}

func (rs *runSupervisor) startLocked(key runKey, input pipelineInput) *activeRun {
	rs.nextID++
	ctx, cancel := context.WithCancel(context.Background())
	run := &activeRun{
		id:        rs.nextID,
		key:       key,
		prompt:    input.text,
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	rs.active[key] = run
	return run
}

func (r *activeRun) description() string {
	if r == nil {
		return ""
	}
	prompt := strings.TrimSpace(r.prompt)
	if len(prompt) > 60 {
		prompt = prompt[:60] + "..."
	}
	age := time.Since(r.startedAt).Round(time.Second)
	if prompt == "" {
		return fmt.Sprintf("rodando há %s", age)
	}
	return fmt.Sprintf("%q rodando há %s", prompt, age)
}

type concurrentMessageKind int

const (
	concurrentEnqueue concurrentMessageKind = iota
	concurrentCancel
	concurrentSupersede
	concurrentStatus
)

func classifyConcurrentMessage(text string) concurrentMessageKind {
	n := normalizeConcurrentText(text)
	if n == "" {
		return concurrentStatus
	}
	if isStatusMessage(n) {
		return concurrentStatus
	}
	if isCancelOnlyMessage(n) {
		return concurrentCancel
	}
	if isSupersedeMessage(n) {
		return concurrentSupersede
	}
	return concurrentEnqueue
}

func normalizeConcurrentText(text string) string {
	n := strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
	n = replacer.Replace(n)
	n = strings.Trim(n, ".,!?:; ")
	return strings.Join(strings.Fields(n), " ")
}

func isCancelOnlyMessage(n string) bool {
	exact := map[string]bool{
		"para": true, "pare": true, "parar": true, "stop": true,
		"cancela": true, "cancelar": true, "cancele": true,
		"interrompe": true, "interrompa": true,
		"esquece": true, "deixa pra la": true, "nao precisa": true,
	}
	if exact[n] {
		return true
	}
	needles := []string{"pode parar", "pode cancelar", "nao precisa mais", "para isso", "cancela isso", "cancele isso"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isSupersedeMessage(n string) bool {
	needles := []string{
		"na verdade", "corrigindo", "em vez", "ao inves", "melhor", "mudei", "troque",
		"nao corrija", "apenas", "so faca", "so teste", "topico errado", "lugar errado",
		"nao era", "errado", "pare e", "cancele e", "ignore o anterior",
	}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}

func isStatusMessage(n string) bool {
	needles := []string{"conseguiu", "terminou", "acabou", "status", "andamento", "ja foi", "ta pronto", "esta pronto"}
	for _, needle := range needles {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}
