package telegram

import "gopkg.in/telebot.v3"

type contextSender interface {
	Send(what interface{}, opts ...interface{}) error
}

func SendContextText(c contextSender, text string, opts ...interface{}) error {
	html := MarkdownToHTML(text)

	// Look for caller-provided SendOptions to merge ParseMode into,
	// preserving fields like ThreadID. If none found, prepend one.
	sendOpts := &telebot.SendOptions{ParseMode: telebot.ModeHTML}
	hasCallerOpts := false
	for _, opt := range opts {
		if so, ok := opt.(*telebot.SendOptions); ok {
			sendOpts = so
			if sendOpts.ParseMode == "" {
				sendOpts.ParseMode = telebot.ModeHTML
			}
			hasCallerOpts = true
			break
		}
	}

	if !hasCallerOpts {
		opts = append([]interface{}{sendOpts}, opts...)
	}

	if err := c.Send(html, opts...); err == nil {
		return nil
	}

	// Fallback: retry without HTML formatting
	sendOpts.ParseMode = ""
	return c.Send(text, opts...)
}
