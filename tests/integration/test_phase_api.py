def test_health_reports_go_runtime(api):
    status, payload = api.get("/health")

    assert status == 200
    assert payload == {"runtime": "go", "status": "ok"}


def test_config_reports_mock_go_backend(api):
    status, payload = api.get("/v1/config")

    assert status == 200
    assert payload["product"] == "Termind"
    assert payload["backend_runtime"] == "go"
    assert payload["mode"] == "phase_4_persistent_memory"
    assert payload["planner_mode"] in {"ollama", "rules"}
    assert payload["ollama_model"]
    assert payload["command_timeout_seconds"] > 0
    assert payload["local_only_mode"] is True


def test_phase_four_is_current_and_later_phases_are_mocked(api):
    status, payload = api.get("/v1/phases")

    assert status == 200
    assert payload["current_phase"] == "phase_4"

    phases = {phase["id"]: phase for phase in payload["phases"]}
    assert phases["phase_1"]["status"] == "ready"
    assert phases["phase_1"]["name"] == "Command Workbench"
    assert "POST /v1/requests" in phases["phase_1"]["mock_apis"]

    assert phases["phase_2"]["status"] == "ready"
    assert phases["phase_3"]["status"] == "ready"
    assert phases["phase_4"]["status"] == "ready"
    assert phases["phase_5"]["status"] == "mocked"
    assert phases["phase_6"]["status"] == "planned"


def test_later_phase_capabilities_are_exposed_as_mocks(api):
    status, payload = api.get("/v1/mocks/capabilities")

    assert status == 200
    capabilities = {capability["id"]: capability for capability in payload["capabilities"]}

    assert capabilities["ollama_planner"]["phase"] == "phase_2"
    assert capabilities["command_executor"]["phase"] == "phase_3"
    assert capabilities["memory_store"]["phase"] == "phase_4"
    assert capabilities["semantic_recall"]["phase"] == "phase_5"
    assert capabilities["voice_input"]["phase"] == "phase_6"
