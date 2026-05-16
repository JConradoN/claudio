package telegram

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// cronFastParse extracts scheduling parameters from common phrasings without
// an LLM round-trip. Returns nil when the message doesn't fit any supported
// pattern — the caller falls back to the LLM parser.
//
// Supported (Portuguese, accent-insensitive):
//   - daily:    "todo dia às Nh[MM] PROMPT" / "diariamente às Nh[MM] PROMPT"
//   - weekly:   "toda <weekday> às Nh[MM] PROMPT" (incl. "toda segunda-feira")
//   - tomorrow: "amanha às Nh[MM] PROMPT"
//   - today:    "hoje às Nh[MM] PROMPT"
//   - relative: "daqui [a] N min[utos]|h[oras] PROMPT" / "em N horas PROMPT"
//
// `now` is injected so tests can pin time; production callers pass time.Now().
func cronFastParse(text string, now time.Time) *cronCreateParsed {
	s := preprocessCronText(text)
	if s == "" {
		return nil
	}

	if r := matchDaily(s); r != nil {
		return r
	}
	if r := matchWeekly(s); r != nil {
		return r
	}
	if r := matchDay(s, "amanha", now.AddDate(0, 0, 1)); r != nil {
		return r
	}
	if r := matchDay(s, "hoje", now); r != nil {
		return r
	}
	if r := matchRelative(s, now); r != nil {
		return r
	}
	return nil
}

// preprocessCronText lowercases, strips accents, and removes the command verb
// prefix so the pattern matchers see only the scheduling clause.
func preprocessCronText(text string) string {
	s := stripAccents(strings.ToLower(strings.TrimSpace(text)))
	if s == "" {
		return ""
	}
	// Order matters: longer prefixes first so "cria um lembrete de " is
	// stripped before "cria um lembrete ".
	prefixes := []string{
		"me lembre de ", "me lembre que ", "me lembre ",
		"me lembra de ", "me lembra que ", "me lembra ",
		"cria um lembrete de ", "cria um lembrete que ", "cria um lembrete ",
		"crie um lembrete de ", "crie um lembrete que ", "crie um lembrete ",
		"criar lembrete de ", "criar lembrete que ", "criar lembrete ",
		"cria um agendamento de ", "cria um agendamento que ", "cria um agendamento ",
		"crie um agendamento de ", "crie um agendamento que ", "crie um agendamento ",
		"criar agendamento de ", "criar agendamento que ", "criar agendamento ",
		"agendar ", "agende ", "agenda ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	return strings.TrimSpace(s)
}

var (
	dailyPrefix    = regexp.MustCompile(`^(?:todo\s+dia|todos\s+os\s+dias|diariamente)\s+`)
	weeklyPrefix   = regexp.MustCompile(`^(?:toda(?:s\s+as)?)\s+([a-z-]+)\s+`)
	relativePrefix = regexp.MustCompile(`^(?:daqui(?:\s+a)?|em)\s+(\d{1,3})\s+(minutos?|mins?|horas?|hora|h)\b\s*`)
	timeSpecRE     = regexp.MustCompile(`^(?:as\s+)?(\d{1,2})(?:h(\d{0,2})?|:(\d{1,2})|\s+horas?)\s*`)
)

// weekdays maps Portuguese weekday names (accent-stripped, singular) to time.Weekday.
// Plural forms ("segundas") and "-feira" suffix are handled by matchWeekday.
var weekdays = map[string]time.Weekday{
	"domingo": time.Sunday,
	"segunda": time.Monday,
	"terca":   time.Tuesday,
	"quarta":  time.Wednesday,
	"quinta":  time.Thursday,
	"sexta":   time.Friday,
	"sabado":  time.Saturday,
}

func matchWeekday(w string) (time.Weekday, bool) {
	w = strings.TrimSuffix(w, "-feira")
	w = strings.TrimSuffix(w, "s") // "segundas" → "segunda"
	wd, ok := weekdays[w]
	return wd, ok
}

func matchDaily(s string) *cronCreateParsed {
	loc := dailyPrefix.FindStringIndex(s)
	if loc == nil {
		return nil
	}
	hour, minute, prompt, ok := parseTimeAndPrompt(s[loc[1]:])
	if !ok {
		return nil
	}
	return &cronCreateParsed{
		Type:     "cron",
		CronExpr: fmt.Sprintf("%d %d * * *", minute, hour),
		Prompt:   prompt,
	}
}

func matchWeekly(s string) *cronCreateParsed {
	m := weeklyPrefix.FindStringSubmatchIndex(s)
	if m == nil {
		return nil
	}
	weekdayWord := s[m[2]:m[3]]
	wd, ok := matchWeekday(weekdayWord)
	if !ok {
		return nil
	}
	hour, minute, prompt, ok := parseTimeAndPrompt(s[m[1]:])
	if !ok {
		return nil
	}
	return &cronCreateParsed{
		Type:     "cron",
		CronExpr: fmt.Sprintf("%d %d * * %d", minute, hour, int(wd)),
		Prompt:   prompt,
	}
}

// matchDay handles "amanha ..." / "hoje ..." — baseDate provides the date,
// the parsed time spec provides the wall clock.
func matchDay(s, marker string, baseDate time.Time) *cronCreateParsed {
	if !strings.HasPrefix(s, marker+" ") && s != marker {
		return nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, marker))
	hour, minute, prompt, ok := parseTimeAndPrompt(rest)
	if !ok {
		return nil
	}
	runAt := time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(),
		hour, minute, 0, 0, baseDate.Location())
	return &cronCreateParsed{
		Type:   "once",
		RunAt:  runAt.Format(time.RFC3339),
		Prompt: prompt,
	}
}

func matchRelative(s string, now time.Time) *cronCreateParsed {
	m := relativePrefix.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return nil
	}
	unit := m[2]
	var dur time.Duration
	switch {
	case strings.HasPrefix(unit, "min"):
		dur = time.Duration(n) * time.Minute
	case strings.HasPrefix(unit, "hora") || unit == "h":
		dur = time.Duration(n) * time.Hour
	default:
		return nil
	}
	prompt := stripCronFiller(strings.TrimSpace(s[len(m[0]):]))
	if prompt == "" {
		return nil
	}
	return &cronCreateParsed{
		Type:   "once",
		RunAt:  now.Add(dur).Format(time.RFC3339),
		Prompt: prompt,
	}
}

// parseTimeAndPrompt reads a time spec at the start of rest and returns the
// extracted hour/minute and remaining prompt text (with leading filler words
// stripped). Returns ok=false when no recognizable time fits.
func parseTimeAndPrompt(rest string) (hour, minute int, prompt string, ok bool) {
	rest = strings.TrimSpace(rest)
	m := timeSpecRE.FindStringSubmatch(rest)
	if m == nil {
		return 0, 0, "", false
	}
	h, err := strconv.Atoi(m[1])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, "", false
	}
	mi := 0
	if m[2] != "" {
		mi, _ = strconv.Atoi(m[2])
	} else if m[3] != "" {
		mi, _ = strconv.Atoi(m[3])
	}
	if mi < 0 || mi > 59 {
		return 0, 0, "", false
	}
	rest = strings.TrimSpace(rest[len(m[0]):])
	rest = stripCronFiller(rest)
	if rest == "" {
		return 0, 0, "", false
	}
	return h, mi, rest, true
}

// stripCronFiller drops connector words that often appear between the time
// spec and the actual action — "às 9h **de** revisar emails".
func stripCronFiller(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"de ", "para ", "pra ", "que "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	return s
}
