package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrUnavailable = errors.New("voice transcriber unavailable")

var acceptedMimeTypes = []string{"audio/wav", "audio/mpeg", "audio/mp4", "audio/webm"}

type Input struct {
	AudioBase64 string
	MimeType    string
	Language    string
}

type Result struct {
	Transcript string
	Confidence float64
}

type Transcriber interface {
	Provider() string
	Transcribe(ctx context.Context, input Input) (Result, error)
}

type DisabledTranscriber struct{}

func (DisabledTranscriber) Provider() string {
	return "disabled"
}

func (DisabledTranscriber) Transcribe(context.Context, Input) (Result, error) {
	return Result{}, ErrUnavailable
}

type CommandTranscriber struct {
	command string
}

func NewCommandTranscriber(command string) CommandTranscriber {
	return CommandTranscriber{command: strings.TrimSpace(command)}
}

func (t CommandTranscriber) Provider() string {
	return "command"
}

func (t CommandTranscriber) Transcribe(ctx context.Context, input Input) (Result, error) {
	if t.command == "" {
		return Result{}, ErrUnavailable
	}
	audio, err := DecodeAudio(input.AudioBase64)
	if err != nil {
		return Result{}, fmt.Errorf("decode audio: %w", err)
	}
	if len(audio) == 0 {
		return Result{}, errors.New("audio is empty")
	}

	extension := extensionForMime(input.MimeType)
	tempDir, err := os.MkdirTemp("", "termind-voice-*")
	if err != nil {
		return Result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	audioPath := filepath.Join(tempDir, "input"+extension)
	if err := os.WriteFile(audioPath, audio, 0o600); err != nil {
		return Result{}, fmt.Errorf("write audio: %w", err)
	}

	command := t.command
	quotedPath := shellQuote(audioPath)
	if strings.Contains(command, "{audio}") {
		command = strings.ReplaceAll(command, "{audio}", quotedPath)
	} else {
		command = command + " " + quotedPath
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return Result{}, fmt.Errorf("run stt command: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	transcript := strings.TrimSpace(stdout.String())
	if transcript == "" {
		return Result{}, errors.New("stt command returned empty transcript")
	}
	return Result{Transcript: transcript, Confidence: 0.70}, nil
}

func extensionForMime(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/webm":
		return ".webm"
	default:
		return ".wav"
	}
}

func AcceptedMimeTypes() []string {
	return append([]string{}, acceptedMimeTypes...)
}

func IsAcceptedMimeType(mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	for _, accepted := range acceptedMimeTypes {
		if normalized == accepted {
			return true
		}
	}
	return false
}

func DecodeAudio(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(value))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
