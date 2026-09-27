def test_health_reports_go_runtime(api):
    status, payload = api.get("/health")

    assert status == 200
    assert payload == {"runtime": "go", "status": "ok"}


def test_config_reports_mock_go_backend(api):
    status, payload = api.get("/v1/config")

    assert status == 200
    assert payload["product"] == "Termind"
    assert payload["backend_runtime"] == "go"
    assert payload["mode"] == "phase_6_voice_input"
    assert payload["planner_mode"] in {"ollama", "rules"}
    assert payload["ollama_model"]
    assert payload["ollama_embed_model"]
    assert payload["command_timeout_seconds"] > 0
    assert payload["local_only_mode"] is True
    assert payload["voice_input_enabled"] is False
    assert payload["voice_stt_provider"] == "disabled"
    assert payload["voice_stt_command"] is False
    assert payload["voice_max_audio_seconds"] > 0
    assert payload["voice_max_audio_bytes"] > 0


def test_phase_six_is_current_and_in_progress(api):
    status, payload = api.get("/v1/phases")

    assert status == 200
    assert payload["current_phase"] == "phase_6"

    phases = {phase["id"]: phase for phase in payload["phases"]}
    assert phases["phase_1"]["status"] == "ready"
    assert phases["phase_1"]["name"] == "Command Workbench"
    assert phases["phase_1"]["mock_apis"] == []

    assert phases["phase_2"]["status"] == "ready"
    assert phases["phase_3"]["status"] == "ready"
    assert phases["phase_4"]["status"] == "ready"
    assert phases["phase_5"]["status"] == "ready"
    assert phases["phase_5"]["mock_apis"] == []
    assert "Command-event embedding backfill" in phases["phase_5"]["scope"]
    assert "Semantic query embeddings" in phases["phase_5"]["scope"]
    assert "Hybrid keyword/semantic ranking" in phases["phase_5"]["scope"]
    assert "Frontend semantic match reasons" in phases["phase_5"]["scope"]
    assert phases["phase_6"]["status"] == "ready"
    assert "6.1 voice capability metadata and config - implemented" in phases["phase_6"]["scope"]
    assert "6.2 command-based local speech-to-text adapter - implemented" in phases["phase_6"]["scope"]
    assert "6.3 transcript endpoint hardening - implemented" in phases["phase_6"]["scope"]
    assert "6.4 frontend microphone and transcript confirmation - implemented" in phases["phase_6"]["scope"]
    assert "6.5 route confirmed transcript through planning and policy - implemented" in phases["phase_6"]["scope"]
    assert "6.6 CLI voice command wrapper - implemented" in phases["phase_6"]["scope"]
    assert phases["phase_6"]["mock_apis"] == []


def test_capability_metadata_matches_completed_and_planned_phases(api):
    status, payload = api.get("/v1/mocks/capabilities")

    assert status == 200
    capabilities = {capability["id"]: capability for capability in payload["capabilities"]}

    assert capabilities["ollama_planner"]["phase"] == "phase_2"
    assert capabilities["command_executor"]["phase"] == "phase_3"
    assert capabilities["memory_store"]["phase"] == "phase_4"
    assert capabilities["semantic_recall"]["phase"] == "phase_5"
    assert capabilities["semantic_recall"]["status"] == "ready"
    assert capabilities["semantic_recall"]["endpoints"] == ["POST /v1/memory/search"]
    assert capabilities["voice_input"]["phase"] == "phase_6"
    assert capabilities["voice_input"]["status"] == "ready"
    assert capabilities["voice_input"]["endpoints"] == ["GET /v1/voice/config", "POST /v1/voice/transcripts"]
    assert capabilities["voice_input"]["next_steps"]
    assert capabilities["voice_input"]["next_steps"][0].startswith("Configure")


def test_embedding_backfill_endpoint(api):
    status, payload = api.post("/v1/memory/embeddings/backfill", {"limit": 2})

    assert status == 200
    assert payload["status"] in {"completed", "partial", "skipped"}
    assert payload["scanned"] >= 0
    assert payload["stored"] >= 0
    assert payload["failed"] >= 0
    assert payload["skipped"] >= 0
    if payload["status"] != "skipped":
        assert payload["embedding_model"]


def test_embedding_backfill_accepts_default_limit(api):
    status, payload = api.post("/v1/memory/embeddings/backfill", {})

    assert status == 200
    assert payload["status"] in {"completed", "partial", "skipped"}
    assert set(payload).issuperset({"scanned", "stored", "failed", "skipped"})


def test_voice_config_contract_is_available(api):
    status, payload = api.get("/v1/voice/config")

    assert status == 200
    assert payload["enabled"] is False
    assert payload["stt_provider"] == "disabled"
    assert payload["max_audio_seconds"] > 0
    assert payload["max_audio_bytes"] > 0
    assert "audio/wav" in payload["accepted_mime_types"]
    assert payload["transcript_endpoint"] == "/v1/voice/transcripts"
    assert payload["status"] == "planned"


def test_voice_transcript_placeholder_keeps_pipeline_contract(api):
    status, payload = api.post(
        "/v1/voice/transcripts",
        {
            "audio_base64": "ZmFrZSBhdWRpbw==",
            "mime_type": "audio/wav",
            "language": "en",
        },
    )

    assert status == 501
    assert payload["status"] == "not_implemented"
    assert payload["transcript"] == ""
    assert payload["requires_edit"] is True
    assert payload["next_endpoint"] == "/v1/requests"


def test_voice_transcript_requires_mime_type(api):
    status, payload = api.post("/v1/voice/transcripts", {"audio_base64": ""})

    assert status == 400
    assert payload == {"error": "mime_type_required"}


def test_voice_transcript_requires_audio_payload(api):
    status, payload = api.post(
        "/v1/voice/transcripts",
        {
            "audio_base64": "",
            "mime_type": "audio/wav",
        },
    )

    assert status == 400
    assert payload["status"] == "audio_required"


def test_voice_transcript_rejects_unsupported_mime_type(api):
    status, payload = api.post(
        "/v1/voice/transcripts",
        {
            "audio_base64": "ZmFrZSBhdWRpbw==",
            "mime_type": "text/plain",
        },
    )

    assert status == 400
    assert payload["status"] == "unsupported_mime_type"


def test_voice_transcript_rejects_invalid_base64(api):
    status, payload = api.post(
        "/v1/voice/transcripts",
        {
            "audio_base64": "not-base64",
            "mime_type": "audio/wav",
        },
    )

    assert status == 400
    assert payload["status"] == "invalid_audio"
