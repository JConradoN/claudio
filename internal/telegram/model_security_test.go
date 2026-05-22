package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"gopkg.in/telebot.v3"
)

type fakeModelLister struct {
	calls  int
	models [][]bridge.ModelInfo
}

func (f *fakeModelLister) ListModels(context.Context) ([]bridge.ModelInfo, error) {
	f.calls++
	idx := f.calls - 1
	if idx >= len(f.models) {
		idx = len(f.models) - 1
	}
	return f.models[idx], nil
}

func TestCmdSetModel_NonOwnerAutoDeniedWithoutMutation(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 200, "/tmp/non-owner.jsonl")
	bc := testModelController(sessions)
	c := newTestTelegramContext(42, 0, 200, "")

	reply, err := bc.cmdSetModel(c, "/model auto")
	if err != nil {
		t.Fatalf("cmdSetModel() error = %v", err)
	}
	assertDeniedWithoutMutation(t, reply, bc, sessions, 42, 0, 200, "/tmp/non-owner.jsonl")
}

func TestCmdSetModel_NonOwnerModelDeniedWithoutMutation(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 200, "/tmp/non-owner.jsonl")
	bc := testModelController(sessions)
	bc.modelCache = []bridge.ModelInfo{{Provider: "openai", ID: "gpt-5.1"}}
	bc.modelCacheExpiry = time.Now().Add(time.Hour)
	c := newTestTelegramContext(42, 0, 200, "")

	reply, err := bc.cmdSetModel(c, "/model openai/gpt-5.1")
	if err != nil {
		t.Fatalf("cmdSetModel() error = %v", err)
	}
	assertDeniedWithoutMutation(t, reply, bc, sessions, 42, 0, 200, "/tmp/non-owner.jsonl")
}

func TestHandleModelCallback_NonOwnerSetDeniedWithoutMutation(t *testing.T) {
	t.Parallel()

	bot, err := telebot.NewBot(telebot.Settings{Offline: true})
	if err != nil {
		t.Fatalf("NewBot() error = %v", err)
	}
	sessions := session.NewStore()
	sessions.SetSession(42, 99, 200, "/tmp/non-owner-topic.jsonl")
	bc := testModelController(sessions)
	bc.bot = bot
	c := newTestTelegramContext(42, 99, 200, "\fmdl_set_openai_gpt-5.1")

	if err := bc.handleModelCallback(c); err != nil {
		t.Fatalf("handleModelCallback() error = %v", err)
	}
	assertDeniedWithoutMutation(t, c.editedText, bc, sessions, 42, 99, 200, "/tmp/non-owner-topic.jsonl")
}

func TestSetModelFromCallback_RejectsUnavailableModelWithoutMutation(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 99, 100, "/tmp/topic-session.jsonl")
	bc := testModelController(sessions)
	bc.modelCache = []bridge.ModelInfo{{Provider: "anthropic", ID: "claude-sonnet-4-6"}}
	bc.modelCacheExpiry = time.Now().Add(time.Hour)
	c := newTestTelegramContext(42, 99, 100, "")

	if err := bc.setModelFromCallback(c, "openai_gpt-5.1"); err != nil {
		t.Fatalf("setModelFromCallback() error = %v", err)
	}
	if !strings.Contains(c.editedText, "Modelo indisponível") {
		t.Fatalf("expected unavailable model message, got %q", c.editedText)
	}
	assertModelState(t, bc, sessions, 42, 99, 100, "/tmp/topic-session.jsonl")
}

func TestSetModelFromCallback_OwnerValidModelPersistsAndResetsCurrentScope(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AURELIA_HOME", tmpDir)

	r, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New() unexpected error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.AppConfig()), 0o700); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}
	initial := `{"default_provider":"anthropic","default_model":"claude-sonnet-4-6","providers":{}}`
	if err := os.WriteFile(r.AppConfig(), []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetSession(42, 99, 100, "/tmp/current-topic-session.jsonl")
	sessions.SetSession(42, 0, 100, "/tmp/general-session.jsonl")
	sessions.SetSession(42, 99, 200, "/tmp/other-topic-session.jsonl")
	bc := testModelController(sessions)
	bc.modelCache = []bridge.ModelInfo{{Provider: "openai", ID: "gpt-5.1"}}
	bc.modelCacheExpiry = time.Now().Add(time.Hour)
	c := newTestTelegramContext(42, 99, 100, "")

	if err := bc.setModelFromCallback(c, "openai_gpt-5.1"); err != nil {
		t.Fatalf("setModelFromCallback() error = %v", err)
	}
	if !strings.Contains(c.editedText, "Modelo alterado") {
		t.Fatalf("expected success message, got %q", c.editedText)
	}
	if bc.config.DefaultProvider != "openai" || bc.config.DefaultModel != "gpt-5.1" {
		t.Fatalf("config not updated: provider=%q model=%q", bc.config.DefaultProvider, bc.config.DefaultModel)
	}
	if sid := sessions.GetSession(42, 99, 100); sid != "" {
		t.Fatalf("current topic session should reset, got %q", sid)
	}
	if sid := sessions.GetSession(42, 0, 100); sid != "/tmp/general-session.jsonl" {
		t.Fatalf("general session should be preserved, got %q", sid)
	}
	if sid := sessions.GetSession(42, 99, 200); sid != "/tmp/other-topic-session.jsonl" {
		t.Fatalf("other user session should be preserved, got %q", sid)
	}
}

func TestCmdRefreshModels_BridgeUnavailable(t *testing.T) {
	t.Parallel()

	bc := testModelController(session.NewStore())
	// no modelLister and no bridge → activeModelLister() returns nil
	reply, err := bc.cmdRefreshModels()
	if err != nil {
		t.Fatalf("cmdRefreshModels() error = %v", err)
	}
	if !strings.Contains(reply, "não disponível") {
		t.Fatalf("expected bridge unavailable message, got %q", reply)
	}
}

func TestCmdSetModel_NonOwnerRefreshDeniedWithoutFetch(t *testing.T) {
	t.Parallel()

	fake := &fakeModelLister{models: [][]bridge.ModelInfo{{{Provider: "openai", ID: "gpt-5.1"}}}}
	bc := testModelController(session.NewStore())
	bc.modelLister = fake
	c := newTestTelegramContext(42, 0, 200, "")

	reply, err := bc.cmdSetModel(c, "/model refresh")
	if err != nil {
		t.Fatalf("cmdSetModel() error = %v", err)
	}
	if !strings.Contains(reply, "Permissão negada") {
		t.Fatalf("expected permission denied, got %q", reply)
	}
	if fake.calls != 0 {
		t.Fatalf("non-owner refresh should not fetch models, calls=%d", fake.calls)
	}
}

func TestCmdSetModel_OwnerRefreshBypassesFreshCache(t *testing.T) {
	t.Parallel()

	fake := &fakeModelLister{models: [][]bridge.ModelInfo{
		{{Provider: "anthropic", ID: "claude-sonnet-4-6"}},
		{{Provider: "newpi", ID: "new-model"}},
	}}
	bc := testModelController(session.NewStore())
	bc.modelLister = fake
	bc.bot = newOfflineTestBot(t)
	bc.modelCache = []bridge.ModelInfo{{Provider: "old", ID: "old-model"}}
	bc.modelCacheExpiry = time.Now().Add(time.Hour)
	c := newTestTelegramContext(42, 0, 100, "")

	reply, err := bc.cmdSetModel(c, "/model refresh")
	if err != nil {
		t.Fatalf("cmdSetModel() error = %v", err)
	}
	if !strings.Contains(reply, "Modelos atualizados") || !strings.Contains(reply, "1") {
		t.Fatalf("expected refresh count reply, got %q", reply)
	}
	if fake.calls != 1 {
		t.Fatalf("expected one forced fetch, got %d", fake.calls)
	}
	if !modelExists(bc.modelCache, "anthropic", "claude-sonnet-4-6") {
		t.Fatalf("expected cache refreshed from PI, got %#v", bc.modelCache)
	}
}

func TestRefreshModelsFromCallback_RedrawsProvidersWithNewModel(t *testing.T) {
	t.Parallel()

	fake := &fakeModelLister{models: [][]bridge.ModelInfo{
		{{Provider: "newpi", ID: "new-model"}},
	}}
	bc := testModelController(session.NewStore())
	bc.modelLister = fake
	bc.bot = newOfflineTestBot(t)
	bc.modelCache = []bridge.ModelInfo{{Provider: "old", ID: "old-model"}}
	bc.modelCacheExpiry = time.Now().Add(time.Hour)
	c := newTestTelegramContext(42, 99, 100, "\fmdl_refresh")

	if err := bc.handleModelCallback(c); err != nil {
		t.Fatalf("handleModelCallback() error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected forced callback fetch, got %d", fake.calls)
	}
	if !strings.Contains(c.editedText, "Selecione o provedor") {
		t.Fatalf("expected provider menu redraw, got %q", c.editedText)
	}
	if !modelExists(bc.modelCache, "newpi", "new-model") {
		t.Fatalf("expected new PI model in cache, got %#v", bc.modelCache)
	}
}

func TestSendProviderMenu_IncludesRefreshButton(t *testing.T) {
	t.Parallel()

	bc := testModelController(session.NewStore())
	bc.modelCache = []bridge.ModelInfo{{Provider: "openai", ID: "gpt-5.1"}}
	c := newTestTelegramContext(42, 99, 100, "")

	if err := bc.sendProviderMenu(c, true); err != nil {
		t.Fatalf("sendProviderMenu() error = %v", err)
	}
	if len(c.editedOpts) == 0 {
		t.Fatalf("expected reply markup option")
	}
	markup, ok := c.editedOpts[0].(*telebot.ReplyMarkup)
	if !ok {
		t.Fatalf("expected *telebot.ReplyMarkup, got %T", c.editedOpts[0])
	}
	if len(markup.InlineKeyboard) == 0 || len(markup.InlineKeyboard[0]) == 0 {
		t.Fatalf("expected inline keyboard rows, got %#v", markup.InlineKeyboard)
	}
	if got := markup.InlineKeyboard[0][0].Text; got != "🔄 Atualizar modelos" {
		t.Fatalf("expected refresh button first, got %q", got)
	}
}

func TestHandleModelCallback_NonOwnerRefreshDeniedWithoutFetch(t *testing.T) {
	t.Parallel()

	fake := &fakeModelLister{models: [][]bridge.ModelInfo{{{Provider: "openai", ID: "gpt-5.1"}}}}
	bc := testModelController(session.NewStore())
	bc.modelLister = fake
	bc.bot = newOfflineTestBot(t)
	c := newTestTelegramContext(42, 99, 200, "\fmdl_refresh")

	if err := bc.handleModelCallback(c); err != nil {
		t.Fatalf("handleModelCallback() error = %v", err)
	}
	if !strings.Contains(c.editedText, "Permissão negada") {
		t.Fatalf("expected permission denied, got %q", c.editedText)
	}
	if fake.calls != 0 {
		t.Fatalf("non-owner callback refresh should not fetch, calls=%d", fake.calls)
	}
}

func testModelController(sessions *session.Store) *BotController {
	return &BotController{
		config: &config.AppConfig{
			DefaultOwnerUserID: 100,
			DefaultProvider:    "anthropic",
			DefaultModel:       "claude-sonnet-4-6",
			Providers:          map[string]config.ProviderConfig{},
		},
		sessions: sessions,
	}
}

func assertDeniedWithoutMutation(t *testing.T, reply string, bc *BotController, sessions *session.Store, chatID int64, threadID int, userID int64, wantSession string) {
	t.Helper()
	if !strings.Contains(reply, "Permissão negada") {
		t.Fatalf("expected permission denial, got %q", reply)
	}
	assertModelState(t, bc, sessions, chatID, threadID, userID, wantSession)
}

func assertModelState(t *testing.T, bc *BotController, sessions *session.Store, chatID int64, threadID int, userID int64, wantSession string) {
	t.Helper()
	if bc.config.DefaultProvider != "anthropic" || bc.config.DefaultModel != "claude-sonnet-4-6" {
		t.Fatalf("config mutated: provider=%q model=%q", bc.config.DefaultProvider, bc.config.DefaultModel)
	}
	if sid := sessions.GetSession(chatID, threadID, userID); sid != wantSession {
		t.Fatalf("session mutated, got %q", sid)
	}
}

func newOfflineTestBot(t *testing.T) *telebot.Bot {
	t.Helper()
	bot, err := telebot.NewBot(telebot.Settings{Offline: true})
	if err != nil {
		t.Fatalf("NewBot() error = %v", err)
	}
	return bot
}

type testTelegramContext struct {
	bot        *telebot.Bot
	message    *telebot.Message
	callback   *telebot.Callback
	sender     *telebot.User
	chat       *telebot.Chat
	data       string
	editedText string
	editedOpts []interface{}
	values     map[string]interface{}
}

func newTestTelegramContext(chatID int64, threadID int, senderID int64, data string) *testTelegramContext {
	chat := &telebot.Chat{ID: chatID}
	sender := &telebot.User{ID: senderID}
	msg := &telebot.Message{Chat: chat, Sender: sender, ThreadID: threadID}
	return &testTelegramContext{
		message:  msg,
		callback: &telebot.Callback{Message: msg, Sender: sender, Data: data},
		sender:   sender,
		chat:     chat,
		data:     data,
		values:   map[string]interface{}{},
	}
}

func (c *testTelegramContext) Bot() *telebot.Bot                                         { return c.bot }
func (c *testTelegramContext) Update() telebot.Update                                    { return telebot.Update{} }
func (c *testTelegramContext) Message() *telebot.Message                                 { return c.message }
func (c *testTelegramContext) Callback() *telebot.Callback                               { return c.callback }
func (c *testTelegramContext) Query() *telebot.Query                                     { return nil }
func (c *testTelegramContext) InlineResult() *telebot.InlineResult                       { return nil }
func (c *testTelegramContext) ShippingQuery() *telebot.ShippingQuery                     { return nil }
func (c *testTelegramContext) PreCheckoutQuery() *telebot.PreCheckoutQuery               { return nil }
func (c *testTelegramContext) Poll() *telebot.Poll                                       { return nil }
func (c *testTelegramContext) PollAnswer() *telebot.PollAnswer                           { return nil }
func (c *testTelegramContext) ChatMember() *telebot.ChatMemberUpdate                     { return nil }
func (c *testTelegramContext) ChatJoinRequest() *telebot.ChatJoinRequest                 { return nil }
func (c *testTelegramContext) Migration() (int64, int64)                                 { return 0, 0 }
func (c *testTelegramContext) Topic() *telebot.Topic                                     { return nil }
func (c *testTelegramContext) Boost() *telebot.BoostUpdated                              { return nil }
func (c *testTelegramContext) BoostRemoved() *telebot.BoostRemoved                       { return nil }
func (c *testTelegramContext) Sender() *telebot.User                                     { return c.sender }
func (c *testTelegramContext) Chat() *telebot.Chat                                       { return c.chat }
func (c *testTelegramContext) Recipient() telebot.Recipient                              { return c.chat }
func (c *testTelegramContext) Text() string                                              { return c.message.Text }
func (c *testTelegramContext) Entities() telebot.Entities                                { return nil }
func (c *testTelegramContext) Data() string                                              { return c.data }
func (c *testTelegramContext) Args() []string                                            { return nil }
func (c *testTelegramContext) Send(what interface{}, opts ...interface{}) error          { return nil }
func (c *testTelegramContext) SendAlbum(a telebot.Album, opts ...interface{}) error      { return nil }
func (c *testTelegramContext) Reply(what interface{}, opts ...interface{}) error         { return nil }
func (c *testTelegramContext) Forward(msg telebot.Editable, opts ...interface{}) error   { return nil }
func (c *testTelegramContext) ForwardTo(to telebot.Recipient, opts ...interface{}) error { return nil }
func (c *testTelegramContext) Edit(what interface{}, opts ...interface{}) error {
	c.editedText = fmt.Sprint(what)
	c.editedOpts = opts
	return nil
}
func (c *testTelegramContext) EditCaption(caption string, opts ...interface{}) error { return nil }
func (c *testTelegramContext) EditOrSend(what interface{}, opts ...interface{}) error {
	return c.Edit(what, opts...)
}
func (c *testTelegramContext) EditOrReply(what interface{}, opts ...interface{}) error {
	return c.Edit(what, opts...)
}
func (c *testTelegramContext) Delete() error                                   { return nil }
func (c *testTelegramContext) DeleteAfter(d time.Duration) *time.Timer         { return time.NewTimer(d) }
func (c *testTelegramContext) Notify(action telebot.ChatAction) error          { return nil }
func (c *testTelegramContext) Ship(what ...interface{}) error                  { return nil }
func (c *testTelegramContext) Accept(errorMessage ...string) error             { return nil }
func (c *testTelegramContext) Answer(resp *telebot.QueryResponse) error        { return nil }
func (c *testTelegramContext) Respond(resp ...*telebot.CallbackResponse) error { return nil }
func (c *testTelegramContext) RespondText(text string) error                   { return nil }
func (c *testTelegramContext) RespondAlert(text string) error                  { return nil }
func (c *testTelegramContext) Get(key string) interface{}                      { return c.values[key] }
func (c *testTelegramContext) Set(key string, val interface{})                 { c.values[key] = val }
