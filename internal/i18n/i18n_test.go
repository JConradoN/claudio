package i18n

import (
	"testing"
)

func TestBundle_T_ReturnsTranslationForExistingKey(t *testing.T) {
	b := NewBundle(LocalePTBR)
	got := b.T("unsupported_document")
	if got == "" {
		t.Fatal("expected non-empty translation for unsupported_document")
	}
	if got == "unsupported_document" {
		t.Fatal("expected translated string, got key as fallback")
	}
}

func TestBundle_T_ReturnsKeyForMissingKey(t *testing.T) {
	b := NewBundle(LocalePTBR)
	got := b.T("nonexistent_key_xyz")
	if got != "nonexistent_key_xyz" {
		t.Fatalf("expected key itself for missing key, got %q", got)
	}
}

func TestBundle_Tf_FormatsTranslation(t *testing.T) {
	b := NewBundle(LocaleEN)
	got := b.Tf("bootstrap_welcome")
	if got == "" {
		t.Fatal("expected non-empty formatted translation")
	}
}

func TestBundle_PTBR_HasAllKeys(t *testing.T) {
	keys := []string{
		"unsupported_document",
		"download_failure",
		"audio_not_configured",
		"audio_processing_failure",
		"empty_audio",
		"already_configured",
		"bootstrap_welcome",
		"bootstrap_failure",
		"bootstrap_assistant",
		"bootstrap_profile",
		"bootstrap_success",
	}
	b := NewBundle(LocalePTBR)
	for _, k := range keys {
		if got := b.T(k); got == k {
			t.Errorf("pt-BR translation missing for key %q", k)
		}
	}
}

func TestBundle_EN_HasAllKeys(t *testing.T) {
	keys := []string{
		"unsupported_document",
		"download_failure",
		"audio_not_configured",
		"audio_processing_failure",
		"empty_audio",
		"already_configured",
		"bootstrap_welcome",
		"bootstrap_failure",
		"bootstrap_assistant",
		"bootstrap_profile",
		"bootstrap_success",
	}
	b := NewBundle(LocaleEN)
	for _, k := range keys {
		if got := b.T(k); got == k {
			t.Errorf("en translation missing for key %q", k)
		}
	}
}

func TestDefaultLocale_IsPTBR(t *testing.T) {
	if DefaultLocale != LocalePTBR {
		t.Fatalf("expected default locale pt-BR, got %s", DefaultLocale)
	}
}

func TestNewBundle_DefaultFallback(t *testing.T) {
	b := NewBundle("invalid-locale")
	got := b.T("unsupported_document")
	if got == "" || got == "unsupported_document" {
		t.Fatal("expected fallback translation for unknown locale")
	}
}
