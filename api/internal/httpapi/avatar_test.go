package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivvanare/ecosystem_complete/api/internal/avatar"
)

type fakeAvatars struct {
	previous *string
	err      error
	url      string
	cleared  bool
}

func (f *fakeAvatars) Set(_ context.Context, _, url string) (*string, error) {
	f.url = url
	return f.previous, f.err
}

func (f *fakeAvatars) Clear(context.Context, string) (*string, error) {
	f.cleared = true
	return f.previous, f.err
}

func avatarRouter(avatars AvatarWriter, storage Storage) http.Handler {
	return New(Options{
		Avatars:      avatars,
		MediaStorage: storage,
		TokenParser:  authedParser("u1"),
		Logger:       slog.New(slog.DiscardHandler),
	})
}

// pngUpload is a minimal multipart body whose bytes sniff as image/png.
func pngUpload(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\nnot-really-a-png")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func TestUploadingAnAvatarIs401WithoutABearer(t *testing.T) {
	// The route exists (a token parser is wired) but the request carries no bearer.
	handler := avatarRouter(&fakeAvatars{}, &recordingStorage{})
	body, contentType := pngUpload(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// The object is written first and the column after: a URL pointing at nothing would be a broken
// image on every screen, which is worse than an orphan file.
func TestUploadingAnAvatarStoresThenRecords(t *testing.T) {
	storage := &recordingStorage{url: "/media/avatars/u1/1.png"}
	avatars := &fakeAvatars{}
	handler := avatarRouter(avatars, storage)

	body, contentType := pngUpload(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"avatarUrl":"/media/avatars/u1/1.png"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if storage.saved == "" || !strings.HasPrefix(storage.saved, "avatars/u1/") {
		t.Fatalf("stored under %q, want a key under avatars/<user>/", storage.saved)
	}
	if !strings.HasSuffix(storage.saved, ".png") {
		t.Fatalf("key = %q, want the sniffed extension", storage.saved)
	}
}

// Replacing an avatar removes the object it replaced, so storage does not fill with orphans from
// the one upload a user repeats.
func TestReplacingAnAvatarDeletesThePreviousObject(t *testing.T) {
	previous := "/media/avatars/u1/old.png"
	storage := &recordingStorage{url: "/media/avatars/u1/new.png"}
	avatars := &fakeAvatars{previous: &previous}

	body, contentType := pngUpload(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer token")
	handler := avatarRouter(avatars, storage)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != "avatars/u1/old.png" {
		t.Fatalf("deleted = %v, want the replaced object key", storage.deleted)
	}
}

func TestDeletingAnAvatarIs204(t *testing.T) {
	avatars := &fakeAvatars{}
	handler := avatarRouter(avatars, &recordingStorage{})
	req := httptest.NewRequest(http.MethodDelete, "/v1/me/avatar", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !avatars.cleared {
		t.Fatal("the avatar was not cleared")
	}
}

// A non-image is rejected before anything is written.
func TestUploadingANonImageIs422(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = part.Write([]byte("#!/bin/sh\necho hola\n"))
	_ = writer.Close()

	storage := &recordingStorage{}
	handler := avatarRouter(&fakeAvatars{}, storage)
	req := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if storage.saves != 0 {
		t.Fatal("a non-image was stored")
	}
}

func TestAFailingAvatarWriteIsA500(t *testing.T) {
	handler := avatarRouter(&fakeAvatars{err: errors.New("db down")}, &recordingStorage{url: "/media/x.png"})
	body, contentType := pngUpload(t)
	req := httptest.NewRequest(http.MethodPut, "/v1/me/avatar", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// A valid token for an account that is gone is an authentication problem, not a server error.
func TestAGoneAccountIs401(t *testing.T) {
	handler := avatarRouter(&fakeAvatars{err: avatar.ErrNotFound}, &recordingStorage{})
	req := httptest.NewRequest(http.MethodDelete, "/v1/me/avatar", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// recordingStorage is the storage seam with a ledger of what happened.
type recordingStorage struct {
	url       string
	saved     string
	saves     int
	deleted   []string
	deleteErr error
}

func (s *recordingStorage) Save(_ context.Context, key string, body io.Reader) (string, error) {
	s.saves++
	s.saved = key
	_, _ = io.Copy(io.Discard, body)
	return s.url, nil
}

func (s *recordingStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErr
}
