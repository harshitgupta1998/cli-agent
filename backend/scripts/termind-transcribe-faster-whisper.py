#!/usr/bin/env python3
"""Local speech-to-text adapter for Termind.

The Go backend calls this script with the audio file path as the first
argument. Print only the transcript to stdout; diagnostics belong on stderr.
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

from faster_whisper import WhisperModel


def env(name: str, fallback: str) -> str:
    value = os.environ.get(name, "").strip()
    return value or fallback


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: termind-transcribe-faster-whisper.py AUDIO_PATH", file=sys.stderr)
        return 2

    audio_path = Path(sys.argv[1])
    if not audio_path.exists():
        print(f"audio file not found: {audio_path}", file=sys.stderr)
        return 2
    if audio_path.stat().st_size == 0:
        print(f"audio file is empty: {audio_path}", file=sys.stderr)
        return 2

    model_name = env("TERMIND_STT_MODEL", "tiny.en")
    model_dir = env("TERMIND_STT_MODEL_DIR", "/models/whisper")
    device = env("TERMIND_STT_DEVICE", "cpu")
    compute_type = env("TERMIND_STT_COMPUTE_TYPE", "int8")
    language = os.environ.get("TERMIND_STT_LANGUAGE", "").strip() or None

    print(
        f"loading STT model={model_name} device={device} compute_type={compute_type}",
        file=sys.stderr,
    )
    model = WhisperModel(
        model_name,
        device=device,
        compute_type=compute_type,
        download_root=model_dir,
    )
    segments, _info = model.transcribe(str(audio_path), language=language, vad_filter=True)
    transcript = " ".join(segment.text.strip() for segment in segments).strip()
    if not transcript:
        print("no speech detected", file=sys.stderr)
        return 1

    print(transcript)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
