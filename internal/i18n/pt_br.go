package i18n

func (b *Bundle) loadPTBR() {
	b.translations = map[string]string{
		"unsupported_document": "⚠️ **Formato não suportado**\n\n" +
			"No momento eu consigo processar:\n" +
			"- arquivos `.md`\n" +
			"- arquivos `.pdf`\n" +
			"- imagens em `.jpg`, `.png`, `.gif` ou `.webp`\n" +
			"- áudio e voz\n\n" +
			"💡 Dica: converta para `.pdf` ou copie o texto diretamente.",

		"download_failure": "❌ **Falha no download**\n\n" +
			"Não consegui baixar o arquivo enviado pelo Telegram. Tente novamente.",

		"audio_not_configured": "⚠️ **Áudio indisponível**\n\n" +
			"Meu módulo de transcrição não está configurado.\n\n" +
			"Configure `groq_api_key` no arquivo `~/.aurelia/config/app.json`.",

		"audio_processing_failure": "❌ **Falha na transcrição**\n\n" +
			"Não consegui compreender o áudio. Tente falar mais claro ou mais perto do microfone.",

		"empty_audio": "⚠️ **Áudio vazio**\n\n" +
			"Não captei conteúdo útil. Pode reenviar?",

		"already_configured": "✅ **Aurelia online**\n\n" +
			"Já estou configurado e pronto. Como posso ajudar?",

		"bootstrap_welcome": "# Boas-vindas\n\n" +
			"Eu sou o **Aurelia** recém-iniciado.\n\n" +
			"Escolha como você quer que eu atue primariamente hoje.",

		"bootstrap_failure": "❌ **Falha no bootstrap**\n\n" +
			"Não consegui criar os arquivos base de persona.",

		"bootstrap_assistant": "✅ **Modo inicial selecionado**\n\n" +
			"Agora descreva como você quer que eu seja: personalidade, tom, estilo.\n\n" +
			"Exemplo: `Quero um assistente direto, sem floreios, que use humor seco quando apropriado.`",

		"bootstrap_profile": "✅ **Personalidade configurada**\n\n" +
			"Agora me diga seu nome e como prefere que eu trabalhe com você.\n\n" +
			"Exemplo: `Me chamo Igor, sou dev e quero respostas diretas.`",

		"bootstrap_success": "✅ **Personas criadas**\n\n" +
			"Suas configurações base foram salvas em `~/.aurelia/memory/personas/`.\n\n" +
			"Você já pode conversar comigo ou editar:\n" +
			"- `IDENTITY.md`\n" +
			"- `SOUL.md`\n" +
			"- `USER.md`\n\n" +
			"para refinar nosso comportamento.",
	}
}
