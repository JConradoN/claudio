# Telebot callback tests need an offline Bot

**Date**: 2026-05-22
**Change**: pi-model-catalog-refresh
**Category**: pattern

## What happened

Model callback tests exercised `handleModelCallback`, which calls `bc.bot.Respond` before using the test context.
Tests without an offline `telebot.Bot` panicked before reaching the behavior under test.

## How to avoid

Use `telebot.NewBot(telebot.Settings{Offline: true})` for handler tests that pass through controller-level callback acknowledgement.
Context-only doubles are enough only for helpers that do not touch `bc.bot`.

## Tags

#lesson #change-pi-model-catalog-refresh #pattern #testing #telebot
