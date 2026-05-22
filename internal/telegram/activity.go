package telegram

import (
	"log"
	"sync"
	"time"

	"gopkg.in/telebot.v3"
)

type actionSender interface {
	Notify(to telebot.Recipient, action telebot.ChatAction, until ...int) error
}

func startChatActionLoop(sender actionSender, recipient telebot.Recipient, action telebot.ChatAction, interval time.Duration, threadID int) func() {
	if sender == nil || recipient == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = typingIndicatorInterval
	}

	notify := func() {
		if threadID > 0 {
			_ = sender.Notify(recipient, action, threadID)
		} else {
			_ = sender.Notify(recipient, action)
		}
	}

	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("telegram: panic in chatActionLoop: %v", r)
			}
		}()

		notify()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				notify()
			case <-done:
				return
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
		})
	}
}
