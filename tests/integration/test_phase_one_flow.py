from uuid import uuid4


DEFAULT_CWD = "/Users/example/projects/termind"
BACKEND_CWD = "/app"


def test_phase_one_review_run_remember_flow(api):
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
        },
    )
    assert session_status == 200
    assert session["session_id"].startswith("ses_")
    assert session["project_id"].startswith("prj_")

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

    messages_status, messages = api.get(f"/v1/sessions/{session['session_id']}/messages")
    assert messages_status == 200
    assert len(messages["messages"]) >= 2
    assert messages["messages"][0]["role"] == "user"
    assert messages["messages"][0]["content"] == "what is using port 8000?"
    assert messages["messages"][1]["role"] == "assistant"
    assert "lsof -i :8000" in messages["messages"][1]["content"]

    execute_status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "request_id": plan["request_id"],
            "user_request": "phase3 execute pwd",
            "command": "pwd",
            "cwd": BACKEND_CWD,
            "shell": "sh",
            "risk_level": "safe",
            "confirmation": {
                "status": "approved",
                "approved_at": "2026-08-15T17:45:00Z",
            },
        },
    )
    assert execute_status == 200
    assert result["status"] == "completed"
    assert result["exit_code"] == 0
    assert result["stdout"].strip() == BACKEND_CWD
    assert result["duration_ms"] > 0

    memory_status, memory = api.post(
        "/v1/memory/search",
        {
            "query": "phase3 execute pwd",
            "project_id": session["project_id"],
            "cwd": BACKEND_CWD,
            "limit": 5,
        },
    )
    assert memory_status == 200
    assert memory["results"]
    assert memory["results"][0]["command"] == "pwd"
    assert "successful command" in memory["results"][0]["matched_reasons"]


def test_rejected_command_returns_rejected_status(api):
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": BACKEND_CWD,
            "shell": "sh",
        },
    )
    assert session_status == 200

    status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "request_id": "req_demo",
            "command": "pwd",
            "cwd": BACKEND_CWD,
            "confirmation": {
                "status": "rejected",
            },
        },
    )

    assert status == 200
    assert result["status"] == "rejected"
    assert result["exit_code"] is None
    assert result["stderr"] == "Command was rejected by the user."


def test_destructive_backend_execution_is_blocked(api):
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": BACKEND_CWD,
            "shell": "sh",
        },
    )
    assert session_status == 200

    status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "request_id": "req_demo",
            "user_request": "try destructive command",
            "command": "rm -rf /tmp/termind-test",
            "cwd": BACKEND_CWD,
            "shell": "sh",
            "risk_level": "destructive",
            "confirmation": {
                "status": "approved",
            },
        },
    )

    assert status == 200
    assert result["status"] == "blocked"
    assert result["exit_code"] is None
    assert "blocked" in result["stderr"].lower()


def test_successful_execution_learns_project_command(api):
    unique_token = uuid4().hex
    command = f"printf {unique_token}"
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": BACKEND_CWD,
            "shell": "sh",
        },
    )
    assert session_status == 200

    execute_status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "request_id": "req_demo",
            "user_request": "learn this project command",
            "command": command,
            "cwd": BACKEND_CWD,
            "shell": "sh",
            "risk_level": "safe",
            "confirmation": {
                "status": "approved",
            },
        },
    )
    assert execute_status == 200
    assert result["status"] == "completed"

    context_status, project = api.get("/v1/context/project", {"cwd": BACKEND_CWD})
    assert context_status == 200
    assert project["project_id"] == session["project_id"]
    assert project["detected_stack"]["language"] == "go"
    assert project["detected_stack"]["package_manager"] == "go modules"
    learned = {item["command"]: item for item in project["common_commands"]}
    assert command in learned
    assert learned[command]["success_count"] >= 1


def test_cli_can_record_locally_executed_command_event(api):
    unique_query = f"pytest persisted command {uuid4().hex}"
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
        },
    )
    assert session_status == 200

    status, payload = api.post(
        "/v1/commands/record",
        {
            "session_id": session["session_id"],
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
    assert payload["embedding_status"] in {"stored", "failed", "skipped"}

    search_status, search = api.post(
        "/v1/memory/search",
        {
            "query": unique_query,
            "project_id": session["project_id"],
            "cwd": DEFAULT_CWD,
            "limit": 5,
        },
    )

    assert search_status == 200
    assert search["results"][0]["command_event_id"] == payload["command_event_id"]
    assert search["results"][0]["user_request"] == unique_query
    assert "same directory" in search["results"][0]["matched_reasons"]


def test_memory_search_can_fall_back_to_semantic_recall(api):
    session_status, session = api.post(
        "/v1/sessions",
        {
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
        },
    )
    assert session_status == 200

    status, payload = api.post(
        "/v1/commands/record",
        {
            "session_id": session["session_id"],
            "request_id": "req_semantic_demo",
            "user_request": "print working directory",
            "proposed_command": "pwd",
            "final_command": "pwd",
            "cwd": DEFAULT_CWD,
            "shell": "zsh",
            "risk_level": "safe",
            "confirmation": "approved",
            "exit_code": 0,
            "stdout": DEFAULT_CWD,
            "stderr": "",
            "duration_ms": 8,
        },
    )

    assert status == 200
    if payload["embedding_status"] != "stored":
        return

    search_status, search = api.post(
        "/v1/memory/search",
        {
            "query": "find earlier shell action for location",
            "project_id": session["project_id"],
            "cwd": DEFAULT_CWD,
            "limit": 5,
        },
    )

    assert search_status == 200
    assert search["results"]
    assert any(result["command"] == "pwd" for result in search["results"])
    assert any("semantic match" in result["matched_reasons"] for result in search["results"])


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


def test_irrelevant_request_does_not_search_or_plan_commands(api):
    status, plan = api.post(
        "/v1/requests",
        {
            "session_id": "ses_demo",
            "input": "how to slap Ruth?",
            "cwd": DEFAULT_CWD,
        },
    )

    assert status == 200
    assert plan["intent"] == "unknown"
    assert plan["plan"]["command"] == ""
    assert plan["policy"]["requires_confirmation"] is False


def test_irrelevant_memory_query_returns_no_seeded_fallback(api):
    status, memory = api.post(
        "/v1/memory/search",
        {
            "query": "how to slap Ruth?",
            "project_id": "prj_demo",
            "cwd": DEFAULT_CWD,
            "limit": 5,
        },
    )

    assert status == 200
    assert memory["results"] == []
