package chatapi

import (
	"sync"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
)

// channelOutput routes pipeline responses to per-request channels keyed by chatID.
// This lets the HTTP handler wait synchronously for the async pipeline to finish.
type channelOutput struct {
	mu      sync.Mutex
	pending map[int64]chan<- string
}

func newChannelOutput() *channelOutput {
	return &channelOutput{pending: make(map[int64]chan<- string)}
}

// register allocates a response channel for chatID and returns a receive-only
// handle plus a cleanup function the caller must defer.
func (o *channelOutput) register(chatID int64) (<-chan string, func()) {
	ch := make(chan string, 4)
	o.mu.Lock()
	o.pending[chatID] = ch
	o.mu.Unlock()
	return ch, func() {
		o.mu.Lock()
		delete(o.pending, chatID)
		o.mu.Unlock()
	}
}

func (o *channelOutput) send(chatID int64, text string) {
	o.mu.Lock()
	ch, ok := o.pending[chatID]
	o.mu.Unlock()
	if ok {
		select {
		case ch <- text:
		default:
		}
	}
}

// --- pipeline.Output implementation ---

func (o *channelOutput) StartTyping(_ int64, _ int) func()  { return func() {} }
func (o *channelOutput) DeleteMessage(_ any)                 {}
func (o *channelOutput) ConfirmMessage(_ int64, _ int)       {}

func (o *channelOutput) NewProgress(_ int64, _ int) pipelinepkg.ProgressReporter {
	return noopProgress{}
}

func (o *channelOutput) SendError(chatID int64, _ int, text string) error {
	o.send(chatID, "[error] "+text)
	return nil
}

func (o *channelOutput) SendReply(chatID int64, _ int, text string) error {
	o.send(chatID, text)
	return nil
}

func (o *channelOutput) SendText(chatID int64, _ int, text string) (any, error) {
	o.send(chatID, text)
	return nil, nil
}

func (o *channelOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {}

type noopProgress struct{}

func (noopProgress) ReportTool(string)       {}
func (noopProgress) ReportToolResult(string) {}
func (noopProgress) ReportText(string)       {}
func (noopProgress) Delete()                 {}
