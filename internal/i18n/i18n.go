package i18n

import (
	"fmt"
)

// Locale represents a supported language.
type Locale string

const (
	LocalePTBR Locale = "pt-BR"
	LocaleEN   Locale = "en"
)

// DefaultLocale is the fallback when no locale is specified.
var DefaultLocale = LocalePTBR

// Bundle holds translations for a locale.
type Bundle struct {
	locale       Locale
	translations map[string]string
}

// NewBundle creates a translation bundle for the given locale.
func NewBundle(locale Locale) *Bundle {
	b := &Bundle{
		locale:       locale,
		translations: make(map[string]string),
	}
	b.load()
	return b
}

// T returns the translation for the given key, or the key itself if not found.
func (b *Bundle) T(key string) string {
	if v, ok := b.translations[key]; ok {
		return v
	}
	return key
}

// Tf returns a formatted translation.
func (b *Bundle) Tf(key string, args ...any) string {
	return fmt.Sprintf(b.T(key), args...)
}

func (b *Bundle) load() {
	switch b.locale {
	case LocalePTBR:
		b.loadPTBR()
	case LocaleEN:
		b.loadEN()
	default:
		b.loadPTBR()
	}
}
