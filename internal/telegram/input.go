package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

func (bc *BotController) handleText(c telebot.Context) error {
	return bc.processInput(c, c.Text())
}

func (bc *BotController) handlePhoto(c telebot.Context) error {
	photo := c.Message().Photo
	if photo == nil {
		return nil
	}

	if c.Message().AlbumID != "" {
		return bc.handlePhotoAlbum(c, photo)
	}

	return bc.processPhotoInput(c, strings.TrimSpace(c.Message().Caption), []albumPhoto{{
		messageID: c.Message().ID,
		photo:     *photo,
	}})
}

func (bc *BotController) handlePhotoAlbum(c telebot.Context, photo *telebot.Photo) error {
	albumID := c.Message().AlbumID
	isOwner := bc.albums.store(
		albumID, c.Message().ID, strings.TrimSpace(c.Message().Caption), *photo,
		c.Chat().ID, c.Message().ThreadID, c.Sender().ID,
	)
	if !isOwner {
		bc.confirmMessage(c.Message())
		return nil
	}

	// Schedule async flush after 900ms window — handler returns immediately
	time.AfterFunc(900*time.Millisecond, func() {
		bc.flushAlbumAndProcess(albumID)
	})

	return nil
}

func (bc *BotController) processPhotoInput(c telebot.Context, caption string, photos []albumPhoto) error {
	if len(photos) == 0 {
		return nil
	}

	stopTyping := startChatActionLoop(bc.bot, c.Chat(), telebot.UploadingPhoto, typingIndicatorInterval, c.Message().ThreadID)
	defer stopTyping()

	text := strings.TrimSpace(caption)
	if text == "" {
		if len(photos) > 1 {
			text = "Analise estas imagens."
		} else {
			text = "Analise esta imagem."
		}
	}

	images, downloaded, partialMsg := bc.collectPhotoAttachments(photos)
	defer func() { removeAll(downloaded) }()

	if len(images) == 0 && partialMsg != "" {
		bc.confirmMessage(c.Message())
		return SendContextText(c, partialMsg)
	}
	if partialMsg != "" {
		// Partial success — let the user know before kicking off the LLM call so
		// they don't wonder why their 4-photo album was analyzed as 3.
		_ = SendContextText(c, partialMsg)
	}
	return bc.processInputWithImages(c, text, images)
}

// collectPhotoAttachments downloads and encodes each photo, returning the
// successful attachments, the local temp paths to clean up, and (when any
// failed) a user-visible message describing what was dropped.
func (bc *BotController) collectPhotoAttachments(photos []albumPhoto) ([]bridge.ImageAttachment, []string, string) {
	var (
		images      []bridge.ImageAttachment
		downloaded  []string
		downloadErr int
		tooLargeMsg string
	)

	for _, p := range photos {
		filePath, err := bc.downloadTelegramFile(&p.photo.File, fmt.Sprintf("photo_%d.jpg", p.messageID))
		if err != nil {
			log.Printf("Failed to download photo: %v", err)
			downloadErr++
			continue
		}
		downloaded = append(downloaded, filePath)
		img, err := encodeImageAttachment(filePath, "image/jpeg", bc.maxImageBytes())
		if err != nil {
			log.Printf("Failed to encode photo: %v", err)
			var tooLarge imageTooLargeError
			if errors.As(err, &tooLarge) {
				if tooLargeMsg == "" {
					tooLargeMsg = tooLarge.UserMessage()
				}
			} else {
				downloadErr++
			}
			continue
		}
		images = append(images, img)
	}

	return images, downloaded, buildPartialMsg(len(images), len(photos), downloadErr, tooLargeMsg)
}

func buildPartialMsg(ok, total, downloadErr int, tooLargeMsg string) string {
	if ok == total {
		return ""
	}
	if ok == 0 {
		if tooLargeMsg != "" {
			return tooLargeMsg
		}
		return fmt.Sprintf("Não consegui processar nenhuma das %d imagens enviadas.", total)
	}
	base := fmt.Sprintf("⚠️ Consegui processar apenas %d de %d imagens.", ok, total)
	if tooLargeMsg != "" {
		return base + "\n" + tooLargeMsg
	}
	if downloadErr > 0 {
		return base + "\nFalha ao baixar/decodificar as outras — tente reenviar."
	}
	return base
}

const fallbackMaxImageBytes = 10 * 1024 * 1024

type imageTooLargeError struct {
	path  string
	size  int
	limit int
}

func (e imageTooLargeError) Error() string {
	return fmt.Sprintf("image %q is %d bytes, exceeds %d byte limit", e.path, e.size, e.limit)
}

func (e imageTooLargeError) UserMessage() string {
	return fmt.Sprintf("Imagem muito grande (%s). O limite configurado é %s.", humanBytes(e.size), humanBytes(e.limit))
}

func humanBytes(n int) string {
	const (
		kib = 1024
		mib = kib * 1024
	)

	if n < kib {
		return fmt.Sprintf("%d B", n)
	}
	if n < mib {
		return fmt.Sprintf("%.1f KB", float64(n)/kib)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/mib)
}

func (bc *BotController) maxImageBytes() int {
	if bc != nil && bc.config != nil && bc.config.MaxImageBytes > 0 {
		return bc.config.MaxImageBytes
	}
	return fallbackMaxImageBytes
}

// encodeImageAttachment reads an image file, base64-encodes it, and returns
// an ImageAttachment suitable for the bridge protocol.
// If the file exceeds maxImageBytes, it returns an error.
func encodeImageAttachment(filePath, defaultMIME string, maxImageBytes int) (bridge.ImageAttachment, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return bridge.ImageAttachment{}, fmt.Errorf("read image %q: %w", filePath, err)
	}
	if maxImageBytes <= 0 {
		maxImageBytes = fallbackMaxImageBytes
	}
	if len(data) > maxImageBytes {
		return bridge.ImageAttachment{}, imageTooLargeError{path: filePath, size: len(data), limit: maxImageBytes}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return bridge.ImageAttachment{
		Path:      filePath,
		Data:      encoded,
		MediaType: defaultMIME,
	}, nil
}

func (bc *BotController) handleDocument(c telebot.Context) error {
	doc := c.Message().Document
	if doc == nil {
		return nil
	}

	if isSupportedImageDocument(doc.FileName, doc.MIME) {
		return bc.handleImageDocument(c, doc)
	}

	if !isSupportedDocument(doc.FileName, doc.MIME) {
		log.Println("Unsupported document type:", doc.MIME)
		bc.confirmMessage(c.Message())
		return SendContextText(c, unsupportedDocumentMessage)
	}

	stopTyping := startChatActionLoop(bc.bot, c.Chat(), telebot.Typing, typingIndicatorInterval, c.Message().ThreadID)
	defer stopTyping()

	filePath, err := bc.downloadTelegramFile(&doc.File, doc.FileID+"_"+doc.FileName)
	if err != nil {
		log.Println("Failed to download file:", err)
		bc.confirmMessage(c.Message())
		return SendContextText(c, downloadFailureMessage)
	}
	defer func() { _ = os.Remove(filePath) }()

	finalInput := buildDocumentInput(c.Message().Caption, doc.FileName, doc.MIME, filePath)
	return bc.processInput(c, finalInput)
}

func (bc *BotController) handleImageDocument(c telebot.Context, doc *telebot.Document) error {
	stopTyping := startChatActionLoop(bc.bot, c.Chat(), telebot.UploadingPhoto, typingIndicatorInterval, c.Message().ThreadID)
	defer stopTyping()

	text := strings.TrimSpace(c.Message().Caption)
	if text == "" {
		text = "Analise esta imagem."
	}

	filePath, err := bc.downloadTelegramFile(&doc.File, doc.FileID+"_"+doc.FileName)
	if err != nil {
		log.Printf("Failed to download image document: %v", err)
		return bc.processInput(c, text)
	}
	defer func() { _ = os.Remove(filePath) }()

	// Determine MIME from filename extension, fall back to doc MIME
	mimeType := doc.MIME
	if mimeType == "" {
		ext := strings.ToLower(filepath.Ext(doc.FileName))
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		default:
			mimeType = "image/jpeg"
		}
	}

	img, err := encodeImageAttachment(filePath, mimeType, bc.maxImageBytes())
	if err != nil {
		log.Printf("Failed to encode image document: %v", err)
		var tooLarge imageTooLargeError
		if errors.As(err, &tooLarge) {
			bc.confirmMessage(c.Message())
			return SendContextText(c, tooLarge.UserMessage())
		}
		return bc.processInput(c, text)
	}

	return bc.processInputWithImages(c, text, []bridge.ImageAttachment{img})
}

func (bc *BotController) handleVoice(c telebot.Context) error {
	fileID, filename, ok := resolveAudioAttachment(c)
	if !ok {
		return nil
	}

	stopRecording := startChatActionLoop(bc.bot, c.Chat(), telebot.RecordingAudio, typingIndicatorInterval, c.Message().ThreadID)
	defer stopRecording()

	filePath, err := bc.downloadTelegramFile(&telebot.File{FileID: fileID}, fileID+"_"+filename)
	if err != nil {
		log.Println("Failed to download audio:", err)
		bc.confirmMessage(c.Message())
		return SendContextText(c, downloadFailureMessage)
	}
	defer func() { _ = os.Remove(filePath) }()

	transcribedText, err := bc.transcribeAudioFile(filePath)
	if err != nil {
		bc.confirmMessage(c.Message())
		var msgErr sendContextTextError
		if ok := errorAs(err, &msgErr); ok {
			return SendContextText(c, msgErr.Error())
		}
		return SendContextText(c, audioProcessingFailureMessage)
	}
	return bc.processInput(c, transcribedText)
}

func isSupportedDocument(filename, mimeType string) bool {
	return strings.HasSuffix(filename, ".md") || mimeType == "application/pdf"
}

func isSupportedImageDocument(filename, mimeType string) bool {
	if isSupportedImageMIME(mimeType) {
		return true
	}
	guessed := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	return isSupportedImageMIME(guessed)
}

func isSupportedImageMIME(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func (bc *BotController) downloadTelegramFile(file *telebot.File, filename string) (string, error) {
	filePath := filepath.Join(os.TempDir(), filename)
	if err := bc.bot.Download(file, filePath); err != nil {
		return "", err
	}
	return filePath, nil
}

func buildDocumentInput(caption, filename, mimeType, filePath string) string {
	var extractedText string
	if strings.HasSuffix(filename, ".md") {
		content, err := os.ReadFile(filePath)
		if err == nil {
			extractedText = string(content)
		}
	} else if mimeType == "application/pdf" {
		extractedText = fmt.Sprintf("[Parsed content of PDF %s]", filename)
	}

	return fmt.Sprintf("%s\n\n[Analise o anexo %s]:\n%s", caption, filename, extractedText)
}

// flushAlbumAndProcess flushes a pending album and processes it asynchronously.
// Called by the 900ms flush timer; runs outside the telebot handler goroutine.
func (bc *BotController) flushAlbumAndProcess(albumID string) {
	fa, ok := bc.albums.flush(albumID)
	if !ok {
		return
	}

	stopTyping := startChatActionLoop(bc.bot, &telebot.Chat{ID: fa.chatID}, telebot.UploadingPhoto, typingIndicatorInterval, fa.threadID)
	defer stopTyping()

	text := strings.TrimSpace(fa.caption)
	if text == "" {
		if len(fa.photos) > 1 {
			text = "Analise estas imagens."
		} else {
			text = "Analise esta imagem."
		}
	}

	images, downloaded, partialMsg := bc.collectPhotoAttachments(fa.photos)
	defer func() { removeAll(downloaded) }()

	if len(images) == 0 && partialMsg != "" {
		_ = SendTextWithThread(bc.bot, &telebot.Chat{ID: fa.chatID}, partialMsg, fa.threadID)
		ReactToMessage(bc.bot, &telebot.Chat{ID: fa.chatID}, fa.messageID, "✅")
		return
	}
	if partialMsg != "" {
		_ = SendTextWithThread(bc.bot, &telebot.Chat{ID: fa.chatID}, partialMsg, fa.threadID)
	}
	if err := bc.runPipeline(fa.chatID, fa.threadID, fa.messageID, text, images); err != nil {
		log.Printf("album: pipeline error for %s: %v", albumID, err)
	}
}

func (ab *albumBuffer) store(albumID string, messageID int, caption string, photo telebot.Photo, chatID int64, threadID int, senderID int64) bool {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	album, ok := ab.pending[albumID]
	if !ok {
		album = &pendingAlbum{
			ownerMessageID: messageID,
			chatID:         chatID,
			threadID:       threadID,
			senderID:       senderID,
			firstMessageID: messageID,
		}
		ab.pending[albumID] = album
		// Schedule GC: if album owner never arrives, clean up after 5 minutes
		time.AfterFunc(5*time.Minute, func() { ab.gcExpired(albumID) })
	}
	if caption != "" && album.caption == "" {
		album.caption = caption
	}
	album.photos = append(album.photos, albumPhoto{messageID: messageID, photo: photo})
	return album.ownerMessageID == messageID
}

// flushedAlbum holds all data extracted from a pending album after flush.
type flushedAlbum struct {
	caption   string
	photos    []albumPhoto
	chatID    int64
	threadID  int
	senderID  int64
	messageID int
}

func (ab *albumBuffer) flush(albumID string) (*flushedAlbum, bool) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	album, ok := ab.pending[albumID]
	if !ok {
		return nil, false
	}
	delete(ab.pending, albumID)

	photos := append([]albumPhoto(nil), album.photos...)
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i].messageID < photos[j].messageID
	})
	return &flushedAlbum{
		caption:   album.caption,
		photos:    photos,
		chatID:    album.chatID,
		threadID:  album.threadID,
		senderID:  album.senderID,
		messageID: album.firstMessageID,
	}, true
}

// gcExpired removes an album from pending if it still exists.
// Called by the TTL timer when the album owner never arrives.
func (ab *albumBuffer) gcExpired(albumID string) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	if _, ok := ab.pending[albumID]; ok {
		delete(ab.pending, albumID)
		log.Printf("album: gc orphan %s", albumID)
	}
}

func resolveAudioAttachment(c telebot.Context) (string, string, bool) {
	switch {
	case c.Message().Voice != nil:
		return c.Message().Voice.FileID, "voice.ogg", true
	case c.Message().Audio != nil:
		return c.Message().Audio.FileID, "audio.mp3", true
	default:
		return "", "", false
	}
}

func (bc *BotController) transcribeAudioFile(filePath string) (string, error) {
	if bc.stt == nil || !bc.stt.IsAvailable() {
		return "", SendContextTextError(audioNotConfiguredMessage)
	}

	log.Printf("Enviando audio [%s] para transcricao via Groq API...", filePath)
	transcribedText, err := bc.stt.Transcribe(context.Background(), filePath)
	if err != nil {
		log.Printf("Groq STT error: %v\n", err)
		return "", SendContextTextError(audioProcessingFailureMessage)
	}
	if strings.TrimSpace(transcribedText) == "" {
		return "", SendContextTextError(emptyAudioMessage)
	}
	return transcribedText, nil
}

type sendContextTextError string

// SendContextTextError creates a sendContextTextError.
func SendContextTextError(message string) error {
	return sendContextTextError(message)
}

func (e sendContextTextError) Error() string {
	return string(e)
}

// removeAll removes every path in the slice, ignoring errors.
// Used by deferred cleanup for downloaded temp files.
func removeAll(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func errorAs(err error, target *sendContextTextError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(sendContextTextError)
	if !ok {
		return false
	}
	*target = value
	return true
}
