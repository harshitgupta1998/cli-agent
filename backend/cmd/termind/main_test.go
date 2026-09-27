package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioMimeTypeFromExtension(t *testing.T) {
	tests := map[string]string{
		"request.wav":  "audio/wav",
		"request.mp3":  "audio/mpeg",
		"request.m4a":  "audio/mp4",
		"request.webm": "audio/webm",
	}

	for path, want := range tests {
		got, err := audioMimeType(path)
		if err != nil {
			t.Fatalf("audioMimeType(%q) returned error: %v", path, err)
		}
		if got != want {
			t.Fatalf("audioMimeType(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestVoiceTranscriptPayloadReadsAudioFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.wav")
	audio := []byte("fake audio")
	if err := os.WriteFile(path, audio, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := voiceTranscriptPayload(path, "en")
	if err != nil {
		t.Fatal(err)
	}

	if payload.MimeType != "audio/wav" {
		t.Fatalf("MimeType = %q, want audio/wav", payload.MimeType)
	}
	if payload.Language != "en" {
		t.Fatalf("Language = %q, want en", payload.Language)
	}
	if payload.AudioBase64 != base64.StdEncoding.EncodeToString(audio) {
		t.Fatalf("AudioBase64 = %q, want encoded audio", payload.AudioBase64)
	}
}

func TestVoiceTranscriptPayloadRejectsUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "request.txt")
	if err := os.WriteFile(path, []byte("fake audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := voiceTranscriptPayload(path, "en"); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}
