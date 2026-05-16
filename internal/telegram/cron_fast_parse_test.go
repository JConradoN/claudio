package telegram

import (
	"testing"
	"time"
)

func TestCronFastParse_Daily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in       string
		wantExpr string
		wantPrm  string
	}{
		{"agenda todo dia às 9h revisar emails", "0 9 * * *", "revisar emails"},
		{"agendar todo dia 8h tomar café", "0 8 * * *", "tomar cafe"},
		{"diariamente às 18h30 verificar logs", "30 18 * * *", "verificar logs"},
		{"todos os dias 7:15 alongar", "15 7 * * *", "alongar"},
		{"agenda todo dia às 9h de revisar emails", "0 9 * * *", "revisar emails"},
	}
	for _, tc := range cases {
		got := cronFastParse(tc.in, now)
		if got == nil {
			t.Errorf("expected match for %q, got nil", tc.in)
			continue
		}
		if got.Type != "cron" {
			t.Errorf("%q: expected type cron, got %q", tc.in, got.Type)
		}
		if got.CronExpr != tc.wantExpr {
			t.Errorf("%q: cron_expr = %q, want %q", tc.in, got.CronExpr, tc.wantExpr)
		}
		if got.Prompt != tc.wantPrm {
			t.Errorf("%q: prompt = %q, want %q", tc.in, got.Prompt, tc.wantPrm)
		}
	}
}

func TestCronFastParse_Weekly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in       string
		wantExpr string
		wantPrm  string
	}{
		{"agenda toda segunda às 10h standup", "0 10 * * 1", "standup"},
		{"toda sexta-feira às 17h retro", "0 17 * * 5", "retro"},
		{"agendar toda quarta 14h30 sync com time", "30 14 * * 3", "sync com time"},
		{"todas as terças às 8h café", "0 8 * * 2", "cafe"},
	}
	for _, tc := range cases {
		got := cronFastParse(tc.in, now)
		if got == nil {
			t.Errorf("expected match for %q, got nil", tc.in)
			continue
		}
		if got.CronExpr != tc.wantExpr {
			t.Errorf("%q: cron_expr = %q, want %q", tc.in, got.CronExpr, tc.wantExpr)
		}
		if got.Prompt != tc.wantPrm {
			t.Errorf("%q: prompt = %q, want %q", tc.in, got.Prompt, tc.wantPrm)
		}
	}
}

func TestCronFastParse_Tomorrow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	got := cronFastParse("me lembra amanhã às 15h de fazer deploy", now)
	if got == nil {
		t.Fatal("expected match, got nil")
	}
	if got.Type != "once" {
		t.Fatalf("expected type once, got %q", got.Type)
	}
	parsed, err := time.Parse(time.RFC3339, got.RunAt)
	if err != nil {
		t.Fatalf("invalid RunAt %q: %v", got.RunAt, err)
	}
	if parsed.Day() != 17 || parsed.Hour() != 15 || parsed.Minute() != 0 {
		t.Fatalf("expected 2026-05-17 15:00, got %v", parsed)
	}
	if got.Prompt != "fazer deploy" {
		t.Fatalf("prompt = %q, want %q", got.Prompt, "fazer deploy")
	}
}

func TestCronFastParse_Today(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	got := cronFastParse("me lembre hoje às 18h30 desligar máquina", now)
	if got == nil {
		t.Fatal("expected match")
	}
	parsed, _ := time.Parse(time.RFC3339, got.RunAt)
	if parsed.Day() != 16 || parsed.Hour() != 18 || parsed.Minute() != 30 {
		t.Fatalf("expected 2026-05-16 18:30, got %v", parsed)
	}
	if got.Prompt != "desligar maquina" {
		t.Fatalf("prompt = %q, want %q", got.Prompt, "desligar maquina")
	}
}

func TestCronFastParse_Relative(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		prm  string
	}{
		{"daqui 30 minutos tomar água", 30 * time.Minute, "tomar agua"},
		{"daqui a 2 horas revisar PR", 2 * time.Hour, "revisar pr"},
		{"em 45 min sair", 45 * time.Minute, "sair"},
		{"em 3 h checar deploy", 3 * time.Hour, "checar deploy"},
		{"agenda daqui 10 min levantar", 10 * time.Minute, "levantar"},
	}
	for _, tc := range cases {
		got := cronFastParse(tc.in, now)
		if got == nil {
			t.Errorf("expected match for %q, got nil", tc.in)
			continue
		}
		if got.Type != "once" {
			t.Errorf("%q: expected once, got %q", tc.in, got.Type)
		}
		parsed, _ := time.Parse(time.RFC3339, got.RunAt)
		if !parsed.Equal(now.Add(tc.want)) {
			t.Errorf("%q: RunAt = %v, want %v", tc.in, parsed, now.Add(tc.want))
		}
		if got.Prompt != tc.prm {
			t.Errorf("%q: prompt = %q, want %q", tc.in, got.Prompt, tc.prm)
		}
	}
}

func TestCronFastParse_FallsThroughOnUnknown(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []string{
		"agenda algum dia uma reunião",      // no time spec
		"me lembra que preciso comprar pão", // no time
		"agenda toda lua cheia jantar",      // not a real weekday
		"todo dia às 25h fazer X",           // invalid hour
		"daqui 0 minutos checar",            // zero duration not useful
		"hoje às 9h",                        // no prompt
		"",
		"oi tudo bem",
	}
	for _, tc := range cases {
		if got := cronFastParse(tc, now); got != nil {
			t.Errorf("expected nil for %q, got %+v", tc, got)
		}
	}
}

func TestCronFastParse_StripsVerbPrefixes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	prefixes := []string{
		"agenda ", "agendar ", "agende ",
		"me lembra ", "me lembre ",
		"me lembra de ", "me lembre de ",
		"cria um lembrete ", "criar lembrete ",
	}
	for _, p := range prefixes {
		input := p + "todo dia às 9h checar"
		got := cronFastParse(input, now)
		if got == nil {
			t.Errorf("expected match for %q, got nil", input)
			continue
		}
		if got.Prompt != "checar" {
			t.Errorf("%q: prompt = %q, want %q", input, got.Prompt, "checar")
		}
	}
}
