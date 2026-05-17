package telegram

import "github.com/igormaneschy/aurelia/internal/i18n"

var bundle = i18n.NewBundle(i18n.DefaultLocale)

func unsupportedDocumentMessage() string  { return bundle.T("unsupported_document") }
func downloadFailureMessage() string      { return bundle.T("download_failure") }
func audioNotConfiguredMessage() string   { return bundle.T("audio_not_configured") }
func audioProcessingFailureMessage() string { return bundle.T("audio_processing_failure") }
func emptyAudioMessage() string           { return bundle.T("empty_audio") }
func alreadyConfiguredMessage() string    { return bundle.T("already_configured") }
func bootstrapWelcomeMessage() string     { return bundle.T("bootstrap_welcome") }
func bootstrapFailureMessage() string      { return bundle.T("bootstrap_failure") }
func bootstrapAssistantMessage() string   { return bundle.T("bootstrap_assistant") }
func bootstrapProfileMessage() string     { return bundle.T("bootstrap_profile") }
func bootstrapSuccessMessage() string     { return bundle.T("bootstrap_success") }
