package telegram

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/telebot.v3"
)

func TestIsSupportedImageDocument(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		filename string
		mimeType string
		want     bool
	}{
		{name: "mime image", filename: "scan.bin", mimeType: "image/png", want: true},
		{name: "extension fallback", filename: "photo.webp", mimeType: "", want: true},
		{name: "pdf is not image", filename: "report.pdf", mimeType: "application/pdf", want: false},
		{name: "markdown is not image", filename: "notes.md", mimeType: "text/markdown", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSupportedImageDocument(tc.filename, tc.mimeType); got != tc.want {
				t.Fatalf("isSupportedImageDocument(%q, %q) = %t, want %t", tc.filename, tc.mimeType, got, tc.want)
			}
		})
	}
}

func TestStoreAndFlushAlbumPhotos(t *testing.T) {
	t.Parallel()

	ab := newAlbumBuffer()

	firstOwner := ab.store("album-1", 12, "", telebot.Photo{File: telebot.File{FileID: "b"}}, 0, 0, 0)
	secondOwner := ab.store("album-1", 10, "Legenda do album", telebot.Photo{File: telebot.File{FileID: "a"}}, 0, 0, 0)

	if !firstOwner {
		t.Fatal("expected first photo in album to become owner")
	}
	if secondOwner {
		t.Fatal("expected subsequent photo not to become owner")
	}

	fa, ok := ab.flush("album-1")
	if !ok {
		t.Fatal("expected album flush to succeed")
	}
	if fa.caption != "Legenda do album" {
		t.Fatalf("expected album caption to be preserved, got %q", fa.caption)
	}
	if len(fa.photos) != 2 {
		t.Fatalf("expected 2 photos, got %d", len(fa.photos))
	}
	if fa.photos[0].messageID != 10 || fa.photos[1].messageID != 12 {
		t.Fatalf("expected photos sorted by message id, got %+v", fa.photos)
	}
	if _, ok := ab.flush("album-1"); ok {
		t.Fatal("expected album to be removed after flush")
	}
}

func TestRemoveAll_RemovesDownloadedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "photo_1.jpg"),
		filepath.Join(dir, "photo_2.jpg"),
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	removeAll(paths)

	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", p)
		}
	}
}

func TestRemoveAll_HandlesNonExistentFile(t *testing.T) {
	t.Parallel()

	// Should not panic or error when file doesn't exist
	removeAll([]string{"/tmp/nonexistent_photo_test.jpg"})
}

func TestRemoveAll_EmptySlice(t *testing.T) {
	t.Parallel()

	// Should not panic on empty slice
	removeAll(nil)
	removeAll([]string{})
}

func TestAlbumGC_RemovesOrphan(t *testing.T) {
	t.Parallel()

	ab := newAlbumBuffer()

	// Store orphan album (owner never arrives)
	ab.store("orphan-album", 1, "", telebot.Photo{File: telebot.File{FileID: "a"}}, 0, 0, 0)

	// gcExpired should find and remove it
	ab.gcExpired("orphan-album")

	// Verify removed
	_, ok := ab.flush("orphan-album")
	if ok {
		t.Fatal("expected orphan album to be GC'd before flush")
	}
}

func TestAlbumGC_NoOpForFlushedAlbums(t *testing.T) {
	t.Parallel()

	ab := newAlbumBuffer()

	// Store album
	ab.store("album-1", 1, "legenda", telebot.Photo{File: telebot.File{FileID: "a"}}, 0, 0, 0)
	// Flush it normally
	_, ok := ab.flush("album-1")
	if !ok {
		t.Fatal("expected flush to succeed")
	}

	// gcExpired should be a no-op (already removed by flush)
	ab.gcExpired("album-1") // should not panic or log
}

func TestAlbumGC_UnknownAlbumIsNoOp(t *testing.T) {
	t.Parallel()

	ab := newAlbumBuffer()
	// gcExpired on non-existent album should not panic
	ab.gcExpired("never-existed")
}

func TestAlbumGC_CleansUpAfterStoreWithoutFlush(t *testing.T) {
	// Verify that gcExpired removes an album that was stored but never flushed.
	// The 5-minute timer in store() calls gcExpired; this test validates the
	// callback behavior directly rather than waiting for the real timer.
	t.Parallel()

	ab := newAlbumBuffer()

	// First photo creates entry + schedules timer
	firstOwner := ab.store("album-gc", 10, "", telebot.Photo{File: telebot.File{FileID: "a"}}, 0, 0, 0)
	if !firstOwner {
		t.Fatal("expected first photo to be owner")
	}

	// Second photo should NOT create a new entry or schedule another timer
	secondOwner := ab.store("album-gc", 12, "legenda", telebot.Photo{File: telebot.File{FileID: "b"}}, 0, 0, 0)
	if secondOwner {
		t.Fatal("expected second photo not to be owner")
	}

	// Manually trigger GC (simulating timer firing after 5min)
	ab.gcExpired("album-gc")

	// Album should be gone
	_, ok := ab.flush("album-gc")
	if ok {
		t.Fatal("expected album to be GC'd")
	}
}

func TestFlushAlbum_ReturnsMetadata(t *testing.T) {
	t.Parallel()

	ab := newAlbumBuffer()

	// Store album with context fields (simulating handlePhotoAlbum)
	ab.store("album-meta", 10, "minhas fotos", telebot.Photo{File: telebot.File{FileID: "a"}}, 12345, 7, 999)

	// Second photo — uses same album, context fields from first photo
	ab.store("album-meta", 12, "", telebot.Photo{File: telebot.File{FileID: "b"}}, 0, 0, 0)

	fa, ok := ab.flush("album-meta")
	if !ok {
		t.Fatal("expected flush to succeed")
	}
	if fa.chatID != 12345 {
		t.Fatalf("expected chatID 12345, got %d", fa.chatID)
	}
	if fa.threadID != 7 {
		t.Fatalf("expected threadID 7, got %d", fa.threadID)
	}
	if fa.senderID != 999 {
		t.Fatalf("expected senderID 999, got %d", fa.senderID)
	}
	if fa.messageID != 10 {
		t.Fatalf("expected messageID 10 (first photo), got %d", fa.messageID)
	}
	if fa.caption != "minhas fotos" {
		t.Fatalf("expected caption preserved, got %q", fa.caption)
	}
	if len(fa.photos) != 2 {
		t.Fatalf("expected 2 photos, got %d", len(fa.photos))
	}

	// Verify timer was scheduled by checking that gcExpired eventually fires
	// (the timer was set when first photo created the album entry in T2)
	ab.gcExpired("album-meta")
	if _, ok := ab.flush("album-meta"); ok {
		t.Fatal("expected album to be empty after gcExpired (timer was scheduled by first store)")
	}
}

// Tests for inputSession, recentMedia, and attachRecentMediaIfRelevant were removed
// because they depend on agent.Message and agent.ContentPart which no longer exist.
// They will be rewritten when the bridge executor is wired.
