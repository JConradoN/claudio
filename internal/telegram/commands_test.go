package telegram

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want *CommandType // nil means no match
	}{
		// --- cron_create ---
		{name: "agenda with details", text: "agenda uma reunião amanhã às 10h", want: cmdPtr(CmdCronCreate)},
		{name: "agendar keyword", text: "agendar lembrete pra sexta", want: cmdPtr(CmdCronCreate)},
		{name: "cria um lembrete", text: "cria um lembrete pra amanhã", want: cmdPtr(CmdCronCreate)},
		{name: "me lembra", text: "me lembra de revisar o PR às 15h", want: cmdPtr(CmdCronCreate)},

		// --- cron_list ---
		{name: "meus agendamentos", text: "meus agendamentos", want: cmdPtr(CmdCronList)},
		{name: "o que ta agendado", text: "o que tá agendado?", want: cmdPtr(CmdCronList)},
		{name: "lista agendamentos", text: "lista agendamentos", want: cmdPtr(CmdCronList)},

		// --- cron_cancel ---
		{name: "cancela agendamento", text: "cancela o agendamento abc123", want: cmdPtr(CmdCronCancel)},
		{name: "cancele agendamento", text: "cancele o agendamento das 7h", want: cmdPtr(CmdCronCancel)},
		{name: "remove lembrete", text: "remove o lembrete de reunião", want: cmdPtr(CmdCronCancel)},
		{name: "desativa agendamento", text: "desativa agendamento abc", want: cmdPtr(CmdCronCancel)},
		{name: "exclui agendamento", text: "exclui agendamento abc123", want: cmdPtr(CmdCronCancel)},
		{name: "apaga agendamento", text: "apaga agendamento abc123", want: cmdPtr(CmdCronCancel)},

		// --- session_reset ---
		{name: "nova conversa", text: "nova conversa", want: cmdPtr(CmdSessionReset)},
		{name: "limpa o contexto", text: "limpa o contexto", want: cmdPtr(CmdSessionReset)},
		{name: "reset", text: "reset", want: cmdPtr(CmdSessionReset)},
		{name: "comeca de novo", text: "começa de novo", want: cmdPtr(CmdSessionReset)},

		// --- status ---
		{name: "status", text: "status", want: cmdPtr(CmdStatus)},

		// --- list_agents ---
		{name: "quais agents", text: "quais agents?", want: cmdPtr(CmdListAgents)},
		{name: "lista agents", text: "lista agents", want: cmdPtr(CmdListAgents)},
		{name: "meus agents", text: "meus agents", want: cmdPtr(CmdListAgents)},

		// --- list_models ---
		{name: "quais modelos", text: "quais modelos?", want: cmdPtr(CmdListModels)},
		{name: "lista modelos", text: "lista modelos", want: cmdPtr(CmdListModels)},
		{name: "lista provedores", text: "lista provedores", want: cmdPtr(CmdListModels)},

		// --- NO match: normal conversation ---
		{name: "greeting", text: "bom dia", want: nil},
		{name: "question", text: "como funciona o bridge?", want: nil},
		{name: "code request", text: "escreve um teste pro handler", want: nil},

		// --- NO match: narrative context (anti-false-positive) ---
		{name: "narrative agendar", text: "ontem eu tentei agendar uma reunião", want: nil},
		{name: "narrative lembrete", text: "ele me lembrou de fazer o deploy", want: nil},
		{name: "narrative status", text: "o status do PR tá verde", want: nil},
		{name: "narrative reset", text: "depois do reset o servidor voltou", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MatchCommand(tt.text)

			if tt.want == nil {
				if got != nil {
					t.Fatalf("MatchCommand(%q) = %v, want nil", tt.text, got.Type)
				}
				return
			}

			if got == nil {
				t.Fatalf("MatchCommand(%q) = nil, want %v", tt.text, *tt.want)
				return
			}
			if got.Type != *tt.want {
				t.Fatalf("MatchCommand(%q).Type = %v, want %v", tt.text, got.Type, *tt.want)
			}
		})
	}
}

func cmdPtr(c CommandType) *CommandType { return &c }

// --- T4: session_reset tests ---

func TestCmdSessionReset(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	tracker := session.NewTracker()
	sessions.Set(42, 0, "sess-abc")
	tracker.Add(42, 1000, 500, 1, 0.01)

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: sessions,
		tracker:  tracker,
	}

	reply, err := bc.cmdSessionReset(42, 0)
	if err != nil {
		t.Fatalf("cmdSessionReset() error = %v", err)
	}

	// Session should be cleared
	if sid := sessions.Get(42, 0); sid != "" {
		t.Fatalf("session should be cleared, got %q", sid)
	}

	// Tracker should be cleared
	usage := tracker.Get(42)
	if usage.NumTurns != 0 {
		t.Fatalf("tracker should be cleared, got %d turns", usage.NumTurns)
	}

	// Real token counts from Tracker.Add — no "~" prefix expected.
	if !strings.Contains(reply, "1 mensagens") || !strings.Contains(reply, "1500 tokens") {
		t.Fatalf("expected reset summary in reply, got %q", reply)
	}
	if strings.Contains(reply, "~1500") {
		t.Fatalf("real token count should not be tildified: %q", reply)
	}
}

func TestCmdSessionReset_EmptySessionUsesSimpleMessage(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: session.NewStore(),
		tracker:  session.NewTracker(),
	}

	reply, err := bc.cmdSessionReset(42, 0)
	if err != nil {
		t.Fatalf("cmdSessionReset() error = %v", err)
	}
	if reply != "Sessão resetada. Próxima mensagem inicia conversa nova." {
		t.Fatalf("unexpected empty reset reply: %q", reply)
	}
}

// --- T5: cron_list tests ---

func TestCmdCronList_WithJobs(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "abc12345-full-uuid", ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "bom dia", Active: true, LastStatus: "idle"},
			{ID: "def67890-full-uuid", ScheduleType: "once", Prompt: "lembrete", Active: true, LastStatus: "pending"},
		},
	}

	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}

	if reply == "" {
		t.Fatal("expected non-empty reply")
	}
	// Should contain job info
	if !contains(reply, "abc12345") || !contains(reply, "bom dia") {
		t.Fatalf("reply should contain job info, got %q", reply)
	}
}

func TestCmdCronList_Empty(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{jobs: nil}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}
	if reply == "" {
		t.Fatal("expected non-empty reply even when no jobs")
	}
}

// --- T6: cron_cancel tests ---

func TestCmdCronCancel_WithID(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronCancel(42, "cancela agendamento abc123")
	if err != nil {
		t.Fatalf("cmdCronCancel() error = %v", err)
	}

	if len(service.deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(service.deleteCalls))
	}
	if service.deleteCalls[0] != "abc123" {
		t.Fatalf("expected delete of 'abc123', got %q", service.deleteCalls[0])
	}
	if reply == "" {
		t.Fatal("expected non-empty reply")
	}
}

func TestCmdCronCancel_NoID(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronCancel(42, "cancela agendamento")
	if err != nil {
		t.Fatalf("cmdCronCancel() error = %v", err)
	}

	// Should not attempt delete without an ID
	if len(service.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls, got %d", len(service.deleteCalls))
	}
	if reply == "" {
		t.Fatal("expected guidance reply")
	}
}

// --- T8: cron_create tests ---

func TestParseCronCreateResponse_RecurringJSON(t *testing.T) {
	t.Parallel()

	raw := `{"type":"cron","cron_expr":"0 9 * * *","prompt":"revisar emails"}`
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "cron" || parsed.CronExpr != "0 9 * * *" || parsed.Prompt != "revisar emails" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_OnceJSON(t *testing.T) {
	t.Parallel()

	raw := `{"type":"once","run_at":"2026-03-27T15:00:00-03:00","prompt":"fazer deploy"}`
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "once" || parsed.RunAt != "2026-03-27T15:00:00-03:00" || parsed.Prompt != "fazer deploy" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_MarkdownFences(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"type\":\"cron\",\"cron_expr\":\"0 9 * * 1\",\"prompt\":\"standup\"}\n```"
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "cron" || parsed.CronExpr != "0 9 * * 1" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_MissingPrompt(t *testing.T) {
	t.Parallel()

	raw := `{"type":"cron","cron_expr":"0 9 * * *"}`
	_, err := parseCronCreateResponse(raw)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestParseCronCreateResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseCronCreateResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- T9: status tests ---

func TestCmdStatus(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "j1", Active: true},
			{ID: "j2", Active: false},
		},
	}
	sessions := session.NewStore()
	sessions.Set(42, 0, "sess-abc-12345678")
	sessions.SetCwd(42, 0, "/repo/aurelia")
	tracker := session.NewTracker()
	tracker.Add(42, 1200, 300, 3, 0.0123)

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		cronHandler: NewCronCommandHandler(service),
		sessions:    sessions,
		tracker:     tracker,
	}

	reply, err := bc.cmdStatus(42, 0)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "kimi-k2-thinking") {
		t.Fatalf("expected model in status, got %q", reply)
	}
	if !strings.Contains(reply, "1") { // 1 active job
		t.Fatalf("expected active job count in status, got %q", reply)
	}
	if strings.Contains(reply, "sid=") || strings.Contains(reply, "sess-abc") || strings.Contains(reply, "warm") || strings.Contains(reply, "cold") {
		t.Fatalf("status should not expose session internals, got %q", reply)
	}
	if !strings.Contains(reply, "/repo/aurelia") {
		t.Fatalf("expected cwd in status, got %q", reply)
	}
	if !strings.Contains(reply, "3 mensagens") || !strings.Contains(reply, "1500 tokens") {
		t.Fatalf("expected human session summary in status, got %q", reply)
	}
}

func TestStatusWorkLinesIncludeActiveDescriptionAndQueue(t *testing.T) {
	t.Parallel()

	lines := statusWorkLines("\"teste\" rodando há 12s", 1)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "teste") || !strings.Contains(joined, "Fila") || !strings.Contains(joined, "1 mensagem") {
		t.Fatalf("expected active work and queue info, got %q", joined)
	}
	if got := statusWorkLines("", 1); got != nil {
		t.Fatalf("empty work should produce no lines, got %v", got)
	}
}

func TestCmdStatus_NoActiveSessionUsesClearText(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: session.NewStore(),
		tracker:  session.NewTracker(),
	}

	reply, err := bc.cmdStatus(42, 0)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "Nenhuma conversa ativa no momento") {
		t.Fatalf("expected clear no-session status, got %q", reply)
	}
}

func TestCmdStatus_UsesTopicCwd(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/group/repo")
	sessions.SetCwd(42, 99, "/topic/repo")

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		sessions: sessions,
		tracker:  session.NewTracker(),
	}

	reply, err := bc.cmdStatus(42, 99)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "/topic/repo") {
		t.Fatalf("expected topic cwd in status, got %q", reply)
	}
	if strings.Contains(reply, "/group/repo") {
		t.Fatalf("expected topic cwd to override group cwd, got %q", reply)
	}
}

func TestCmdStatus_FallsBackToGroupCwd(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/group/repo")

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		sessions: sessions,
		tracker:  session.NewTracker(),
	}

	reply, err := bc.cmdStatus(42, 99)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "/group/repo") {
		t.Fatalf("expected group cwd fallback in status, got %q", reply)
	}
}

// --- T10: list_agents tests ---

func TestCmdListAgents_WithAgents(t *testing.T) {
	t.Parallel()

	// Create a temp dir with agent files
	reg := buildTestRegistry(t, map[string]string{
		"coder":      "Writes and debugs code",
		"prospector": "Busca leads e prospecta clientes",
	})

	bc := &BotController{
		config: &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		agents: reg,
	}

	reply, err := bc.cmdListAgents()
	if err != nil {
		t.Fatalf("cmdListAgents() error = %v", err)
	}
	if !strings.Contains(reply, "coder") || !strings.Contains(reply, "prospector") {
		t.Fatalf("expected agent names, got %q", reply)
	}
}

func TestCmdListAgents_Empty(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config: &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
	}

	reply, err := bc.cmdListAgents()
	if err != nil {
		t.Fatalf("cmdListAgents() error = %v", err)
	}
	if !strings.Contains(reply, "Nenhum") {
		t.Fatalf("expected 'nenhum' message, got %q", reply)
	}
}

// buildTestRegistry creates a Registry with agents for testing.
func buildTestRegistry(t *testing.T, agentMap map[string]string) *agents.Registry {
	t.Helper()
	dir := t.TempDir()
	for name, desc := range agentMap {
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\nYou are %s.", name, desc, name)
		path := dir + "/" + name + ".md"
		if err := writeTestFile(path, content); err != nil {
			t.Fatalf("failed to write agent file: %v", err)
		}
	}
	reg, err := agents.Load(dir)
	if err != nil {
		t.Fatalf("agents.Load() error = %v", err)
	}
	return reg
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// --- T11: list_models tests ---

func TestCmdListModels_RequiresBridge(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers: map[string]config.ProviderConfig{
				"anthropic": {APIKey: "sk-test"},
			},
		},
	}

	reply, err := bc.cmdListModels()
	if err != nil {
		t.Fatalf("cmdListModels() error = %v", err)
	}
	// Without a bridge, cmdListModels returns a user-friendly message.
	if !strings.Contains(reply, "disponível") {
		t.Fatalf("expected bridge-not-available message, got %q", reply)
	}
}

func TestExtractModelName(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"slash model", "/model claude-opus-4-6", "claude-opus-4-6"},
		{"slash model with extra spaces", "  /model  kimi-k2.5  ", "kimi-k2.5"},
		{"muda modelo para", "muda modelo para claude-sonnet", "claude-sonnet"},
		{"trocar modelo para", "trocar modelo para gpt-4", "gpt-4"},
		{"escolhe modelo", "escolhe modelo gemini", "gemini"},
		{"no known prefix returns empty (regression)", "olá tudo bem amigo", ""},
		{"empty input", "", ""},
		{"prefix without name returns empty", "muda modelo para ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractModelName(tc.text); got != tc.want {
				t.Errorf("extractModelName(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestResetCurrentModelSession_ClearsOnlyCurrentThread(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	tracker := session.NewTracker()
	sessions.Set(42, 0, "general-session")
	sessions.Set(42, 99, "topic-session")
	sessions.SetCwd(42, 99, "/topic/repo")
	tracker.Add(42, 100, 50, 1, 0.01)

	bc := &BotController{sessions: sessions, tracker: tracker}
	msg := bc.resetCurrentModelSession(42, 99)

	if sid := sessions.Get(42, 99); sid != "" {
		t.Fatalf("current topic session should be cleared, got %q", sid)
	}
	if sid := sessions.Get(42, 0); sid != "general-session" {
		t.Fatalf("general session should be preserved, got %q", sid)
	}
	if cwd := sessions.GetCwd(42, 99); cwd != "/topic/repo" {
		t.Fatalf("topic cwd should be preserved, got %q", cwd)
	}
	if usage := tracker.Get(42); usage.NumTurns != 0 {
		t.Fatalf("tracker should be cleared, got %d turns", usage.NumTurns)
	}
	if !strings.Contains(msg, "tópico") || !strings.Contains(msg, "1 mensagens") || !strings.Contains(msg, "150 tokens") {
		t.Fatalf("expected topic reset summary, got %q", msg)
	}
	if strings.Contains(msg, "~150") {
		t.Fatalf("real token count should not be tildified: %q", msg)
	}
}

func TestResetCurrentModelSession_ClearsPrivateChatSession(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.Set(42, 0, "private-session")

	bc := &BotController{sessions: sessions}
	msg := bc.resetCurrentModelSession(42, 0)

	if sid := sessions.Get(42, 0); sid != "" {
		t.Fatalf("private chat session should be cleared, got %q", sid)
	}
	if !strings.Contains(msg, "Sessão privada resetada") {
		t.Fatalf("expected private-session reset message, got %q", msg)
	}
}

func TestHelpMessageIncludesNaturalExamples(t *testing.T) {
	t.Parallel()

	help := helpMessage()
	for _, want := range []string{"💡", "agenda todo dia às 9h", "muda modelo", "limpa o contexto"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q: %q", want, help)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
