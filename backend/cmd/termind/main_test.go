package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/harsgupta/termind/backend/internal/models"
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

func TestVoiceAudioWrapperPlansExecutesAndRecordsTranscript(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "request.wav")
	audio := []byte("fake audio")
	if err := os.WriteFile(audioPath, audio, 0o600); err != nil {
		t.Fatal(err)
	}

	const transcript = "print voice e2e marker"
	recorded := make(chan models.CommandRecordRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1/voice/transcripts":
			if r.Method != http.MethodPost {
				t.Fatalf("voice method = %s, want POST", r.Method)
			}
			var payload models.VoiceTranscriptRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.MimeType != "audio/wav" {
				t.Fatalf("MimeType = %q, want audio/wav", payload.MimeType)
			}
			if payload.Language != "en" {
				t.Fatalf("Language = %q, want en", payload.Language)
			}
			if payload.AudioBase64 != base64.StdEncoding.EncodeToString(audio) {
				t.Fatal("voice endpoint received unexpected audio payload")
			}
			_ = json.NewEncoder(w).Encode(models.VoiceTranscriptResponse{
				Status:       "completed",
				Transcript:   transcript,
				Confidence:   0.94,
				STTProvider:  "test",
				RequiresEdit: true,
				NextEndpoint: "/v1/requests",
				Message:      "Transcript created locally.",
			})

		case "/v1/requests":
			if r.Method != http.MethodPost {
				t.Fatalf("request method = %s, want POST", r.Method)
			}
			var payload models.UserRequestCreate
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Input != transcript {
				t.Fatalf("request input = %q, want transcript %q", payload.Input, transcript)
			}
			_ = json.NewEncoder(w).Encode(models.UserRequestResponse{
				RequestID: "req_voice_e2e",
				Intent:    models.IntentExecuteCommand,
				Plan: models.CommandPlan{
					Command:              "printf voice-e2e",
					CWD:                  payload.CWD,
					Risk:                 models.RiskSafe,
					RequiresConfirmation: false,
					Reason:               "safe test command",
					Provenance:           []string{"voice transcript"},
				},
				Policy: models.PolicyDecision{
					Risk:                 models.RiskSafe,
					RequiresConfirmation: false,
				},
			})

		case "/v1/commands/record":
			if r.Method != http.MethodPost {
				t.Fatalf("record method = %s, want POST", r.Method)
			}
			var payload models.CommandRecordRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			recorded <- payload
			_ = json.NewEncoder(w).Encode(models.CommandRecordResponse{
				CommandEventID: "cmd_voice_e2e",
				Status:         models.CommandStatusCompleted,
				Message:        "recorded",
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	restoreStdin := replaceStdin(t, "y\n")
	defer restoreStdin()

	client := APIClient{
		baseURL: server.URL,
		http:    server.Client(),
	}

	confirmedTranscript, err := handleVoiceAudio(client, audioPath, "en")
	if err != nil {
		t.Fatal(err)
	}
	if confirmedTranscript != transcript {
		t.Fatalf("confirmed transcript = %q, want %q", confirmedTranscript, transcript)
	}

	if err := handleRequest(client, "ses_voice_e2e", dir, confirmedTranscript, true, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	select {
	case record := <-recorded:
		if record.UserRequest != transcript {
			t.Fatalf("recorded UserRequest = %q, want %q", record.UserRequest, transcript)
		}
		if record.FinalCommand != "printf voice-e2e" {
			t.Fatalf("recorded FinalCommand = %q, want printf voice-e2e", record.FinalCommand)
		}
		if strings.TrimSpace(record.Stdout) != "voice-e2e" {
			t.Fatalf("recorded Stdout = %q, want voice-e2e", record.Stdout)
		}
		if record.Confirmation != models.ConfirmationApproved {
			t.Fatalf("recorded Confirmation = %q, want approved", record.Confirmation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command record")
	}
}

func replaceStdin(t *testing.T, input string) func() {
	t.Helper()

	original := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader

	return func() {
		os.Stdin = original
		_ = reader.Close()
	}
}
