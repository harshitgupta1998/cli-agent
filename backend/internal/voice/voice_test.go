package voice

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

func TestCommandTranscriberRunsConfiguredCommand(t *testing.T) {
	transcriber := NewCommandTranscriber("printf 'show current directory'")
	audio := base64.StdEncoding.EncodeToString([]byte("fake audio"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := transcriber.Transcribe(ctx, Input{
		AudioBase64: audio,
		MimeType:    "audio/wav",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if result.Transcript != "show current directory" {
		t.Fatalf("Transcript = %q, want %q", result.Transcript, "show current directory")
	}
	if result.Confidence <= 0 {
		t.Fatalf("Confidence = %f, want positive", result.Confidence)
	}
}

func TestDisabledTranscriberReturnsUnavailable(t *testing.T) {
	_, err := DisabledTranscriber{}.Transcribe(context.Background(), Input{})
	if err != ErrUnavailable {
		t.Fatalf("err = %v, want %v", err, ErrUnavailable)
	}
}

func TestAcceptedMimeTypesAreDefensiveCopy(t *testing.T) {
	types := AcceptedMimeTypes()
	types[0] = "mutated"

	if !IsAcceptedMimeType("audio/wav") {
		t.Fatal("audio/wav should remain accepted")
	}
	if IsAcceptedMimeType("text/plain") {
		t.Fatal("text/plain should not be accepted")
	}
}
