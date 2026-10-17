from uuid import uuid4


def test_session_creates_owner_workspace_and_workspace_endpoints_are_scoped(api):
    cwd = "/tmp/termind-workspace-" + uuid4().hex
    session_status, session = api.post("/v1/sessions", {"cwd": cwd, "shell": "sh"})
    assert session_status == 200
    assert session["workspace_id"]

    list_status, listed = api.get("/v1/workspaces")
    assert list_status == 200
    workspace = next(item for item in listed["workspaces"] if item["id"] == session["workspace_id"])
    assert workspace["owner_user_id"] == "usr_owner"
    assert workspace["root_path"] == cwd

    get_status, fetched = api.get(f"/v1/workspaces/{workspace['id']}")
    assert get_status == 200
    assert fetched["id"] == workspace["id"]

    update_status, updated = api.patch(f"/v1/workspaces/{workspace['id']}", {"name": "Renamed test workspace"})
    assert update_status == 200
    assert updated["name"] == "Renamed test workspace"


def test_client_cannot_choose_workspace_owner_or_access_unknown_workspace(api):
    status, payload = api.post(
        "/v1/workspaces",
        {"name": "spoofed", "root_path": "/tmp", "owner_user_id": "usr_other"},
    )
    assert status == 400
    assert payload == {"error": "invalid_json"}

    status, payload = api.get("/v1/workspaces/wsp_not_owned_by_current_user")
    assert status == 404
    assert payload == {"error": "workspace_not_found"}


def test_session_messages_and_execution_cannot_escape_workspace_scope(api):
    cwd = "/tmp/termind-scope-" + uuid4().hex
    session_status, session = api.post("/v1/sessions", {"cwd": cwd, "shell": "sh"})
    assert session_status == 200

    messages_status, messages = api.get("/v1/sessions/ses_not_owned/messages")
    assert messages_status == 404
    assert messages == {"error": "session_not_found"}

    execute_status, result = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "command": "pwd",
            "cwd": "/tmp",
            "confirmation": {"status": "approved"},
        },
    )
    assert execute_status == 404
    assert result == {"error": "workspace_not_found"}


def test_workspace_policy_is_validated_and_denied_before_execution(api):
    cwd = "/tmp/termind-policy-" + uuid4().hex
    session_status, session = api.post("/v1/sessions", {"cwd": cwd, "shell": "sh"})
    assert session_status == 200
    workspace_id = session["workspace_id"]
    permissions_path = f"/v1/workspaces/{workspace_id}/permissions"

    status, defaults = api.get(permissions_path)
    assert status == 200
    assert defaults["execution_mode"] == "confirmation_required"

    status, error = api.put(
        permissions_path,
        {
            "execution_mode": "unknown-mode",
            "writable_roots": [],
            "allowed_commands": [],
            "denied_commands": [],
            "max_duration_seconds": 30,
            "max_output_bytes": 1048576,
        },
    )
    assert status == 400
    assert error == {"error": "execution_mode_invalid"}

    status, updated = api.put(
        permissions_path,
        {
            "execution_mode": "disabled",
            "writable_roots": [],
            "allowed_commands": [],
            "denied_commands": [],
            "max_duration_seconds": 30,
            "max_output_bytes": 1048576,
        },
    )
    assert status == 200
    assert updated["policy_version"] > defaults["policy_version"]

    status, error = api.post(
        "/v1/commands/execute",
        {
            "session_id": session["session_id"],
            "command": "pwd",
            "cwd": cwd,
            "confirmation": {"status": "approved"},
        },
    )
    assert status == 403
    assert error["error"] == "execution_disabled"
