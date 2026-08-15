from uuid import uuid4


DEFAULT_CWD = "/Users/example/projects/termind"


def test_phase_one_review_run_remember_flow(api):
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
        },
    )
    assert session_status == 200
    assert session["session_id"] == "ses_demo"
    assert session["project_id"] == "prj_demo"

    request_status, plan = api.post(
        "/v1/requests",
        {
            "session_id": session["session_id"],
            "input": "what is using port 8000?",
            "cwd": DEFAULT_CWD,
        },
    )
    assert request_status == 200
    assert plan["intent"] == "execute_command"
    assert plan["plan"]["command"] == "lsof -i :8000"
    assert plan["plan"]["risk"] == "safe"
    assert plan["policy"]["requires_confirmation"] is True

    execute_status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "request_id": plan["request_id"],
            "command": plan["plan"]["command"],
            "cwd": DEFAULT_CWD,
            "confirmation": {
                "status": "approved",
                "approved_at": "2026-08-15T17:45:00Z",
            },
        },
    )
    assert execute_status == 200
    assert result["status"] == "completed"
    assert result["exit_code"] == 0
    assert "LISTEN" in result["stdout"]
    assert result["duration_ms"] > 0

    memory_status, memory = api.post(
        "/v1/memory/search",
        {
            "query": "port 8000",
            "project_id": "prj_demo",
            "cwd": DEFAULT_CWD,
            "limit": 5,
        },
    )
    assert memory_status == 200
    assert memory["results"]
    assert memory["results"][0]["command"] == "lsof -i :8000"
    assert "successful command" in memory["results"][0]["matched_reasons"]


def test_rejected_command_returns_rejected_status(api):
    status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": "ses_demo",
            "request_id": "req_demo",
            "command": "lsof -i :8000",
            "cwd": DEFAULT_CWD,
            "confirmation": {
                "status": "rejected",
            },
        },
    )

    assert status == 200
    assert result["status"] == "rejected"
    assert result["exit_code"] is None
    assert result["stderr"] == "Command was rejected by the user."


def test_cli_can_record_locally_executed_command_event(api):
    unique_query = f"pytest persisted command {uuid4().hex}"
    status, payload = api.post(
        "/v1/commands/record",
        {
            "session_id": "ses_demo",
            "request_id": "req_demo",
            "user_request": unique_query,
            "proposed_command": "pwd",
            "final_command": "pwd",
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
            "risk_level": "safe",
            "confirmation": "approved",
            "exit_code": 0,
            "stdout": DEFAULT_CWD,
            "stderr": "",
            "duration_ms": 12,
        },
    )

    assert status == 200
    assert payload["status"] == "completed"
    assert payload["command_event_id"].startswith("cmd_")
    assert payload["message"] == "Command event persisted to Postgres."

    search_status, search = api.post(
        "/v1/memory/search",
        {
            "query": unique_query,
            "project_id": "prj_demo",
            "cwd": DEFAULT_CWD,
            "limit": 5,
        },
    )

    assert search_status == 200
    assert search["results"][0]["command_event_id"] == payload["command_event_id"]
    assert search["results"][0]["user_request"] == unique_query
    assert "same directory" in search["results"][0]["matched_reasons"]


def test_validation_errors_are_explicit(api):
    status, payload = api.post(
        "/v1/requests",
        {
            "session_id": "ses_demo",
            "input": "",
            "cwd": DEFAULT_CWD,
        },
    )

    assert status == 400
    assert payload == {"error": "input_required"}
