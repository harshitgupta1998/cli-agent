package services

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
	"github.com/harsgupta/termind/backend/internal/voice"
)

func TestCreateVoiceTranscriptUsesCommandTranscriber(t *testing.T) {
	agent := NewAgentService(nil, nil, time.Second).
		WithVoiceConfig(true, "command", 5).
		WithVoiceTranscriber(voice.NewCommandTranscriber("printf 'show current directory'"))

	response := agent.CreateVoiceTranscript(models.VoiceTranscriptRequest{
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("fake audio")),
		MimeType:    "audio/wav",
		Language:    "en",
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Transcript != "show current directory" {
		t.Fatalf("Transcript = %q, want show current directory", response.Transcript)
	}
	if response.NextEndpoint != "/v1/requests" {
		t.Fatalf("NextEndpoint = %q, want /v1/requests", response.NextEndpoint)
	}
	if !response.RequiresEdit {
		t.Fatal("RequiresEdit = false, want true")
	}
}

func TestCreateVoiceTranscriptRejectsInvalidAudio(t *testing.T) {
	agent := NewAgentService(nil, nil, time.Second).
		WithVoiceConfig(true, "command", 5).
		WithVoiceTranscriber(voice.NewCommandTranscriber("printf 'unused'"))

	response := agent.CreateVoiceTranscript(models.VoiceTranscriptRequest{
		AudioBase64: "not-base64",
		MimeType:    "audio/wav",
	})

	if response.Status != "invalid_audio" {
		t.Fatalf("Status = %q, want invalid_audio", response.Status)
	}
}

func TestCreateVoiceTranscriptRejectsOversizedAudio(t *testing.T) {
	agent := NewAgentService(nil, nil, time.Second).
		WithVoiceConfig(true, "command", 5).
		WithVoiceMaxBytes(2).
		WithVoiceTranscriber(voice.NewCommandTranscriber("printf 'unused'"))

	response := agent.CreateVoiceTranscript(models.VoiceTranscriptRequest{
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("fake audio")),
		MimeType:    "audio/wav",
	})

	if response.Status != "payload_too_large" {
		t.Fatalf("Status = %q, want payload_too_large", response.Status)
	}
}

func TestCreateVoiceTranscriptRejectsUnsupportedMimeType(t *testing.T) {
	agent := NewAgentService(nil, nil, time.Second).
		WithVoiceConfig(true, "command", 5).
		WithVoiceTranscriber(voice.NewCommandTranscriber("printf 'unused'"))

	response := agent.CreateVoiceTranscript(models.VoiceTranscriptRequest{
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("fake audio")),
		MimeType:    "text/plain",
	})

	if response.Status != "unsupported_mime_type" {
		t.Fatalf("Status = %q, want unsupported_mime_type", response.Status)
	}
}
