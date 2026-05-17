package i18n

func (b *Bundle) loadEN() {
	b.translations = map[string]string{
		"unsupported_document": "⚠️ **Unsupported format**\n\n" +
			"I can currently process:\n" +
			"- `.md` files\n" +
			"- `.pdf` files\n" +
			"- images in `.jpg`, `.png`, `.gif`, or `.webp`\n" +
			"- audio and voice\n\n" +
			"💡 Tip: convert to `.pdf` or copy the text directly.",

		"download_failure": "❌ **Download failed**\n\n" +
			"I couldn't download the file sent via Telegram. Please try again.",

		"audio_not_configured": "⚠️ **Audio unavailable**\n\n" +
			"My transcription module is not configured.\n\n" +
			"Set `groq_api_key` in `~/.aurelia/config/app.json`.",

		"audio_processing_failure": "❌ **Transcription failed**\n\n" +
			"I couldn't understand the audio. Try speaking more clearly or closer to the microphone.",

		"empty_audio": "⚠️ **Empty audio**\n\n" +
			"I didn't catch any useful content. Can you resend?",

		"already_configured": "✅ **Aurelia online**\n\n" +
			"I'm configured and ready. How can I help?",

		"bootstrap_welcome": "# Welcome\n\n" +
			"I'm **Aurelia**, freshly started.\n\n" +
			"Choose how you want me to act primarily today.",

		"bootstrap_failure": "❌ **Bootstrap failed**\n\n" +
			"I couldn't create the base persona files.",

		"bootstrap_assistant": "✅ **Initial mode selected**\n\n" +
			"Now describe how you want me to be: personality, tone, style.\n\n" +
			"Example: `I want a direct assistant, no fluff, who uses dry humor when appropriate.`",

		"bootstrap_profile": "✅ **Personality configured**\n\n" +
			"Now tell me your name and how you prefer me to work with you.\n\n" +
			"Example: `My name is Igor, I'm a dev and I want direct answers.`",

		"bootstrap_success": "✅ **Personas created**\n\n" +
			"Your base settings have been saved to `~/.aurelia/memory/personas/`.\n\n" +
			"You can now chat with me or edit:\n" +
			"- `IDENTITY.md`\n" +
			"- `SOUL.md`\n" +
			"- `USER.md`\n\n" +
			"to refine our behavior.",
	}
}
