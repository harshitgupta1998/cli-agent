import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request

import pytest


API_BASE_URL = os.environ.get("TERMIND_API_BASE_URL", "http://localhost:8000")


class APIClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    def get(self, path: str, query: dict[str, str] | None = None) -> tuple[int, dict]:
        url = f"{self.base_url}{path}"
        if query:
            url = f"{url}?{urllib.parse.urlencode(query)}"
        return self._request("GET", url)

    def post(self, path: str, payload: dict) -> tuple[int, dict]:
        return self._request("POST", f"{self.base_url}{path}", payload)

    def _request(self, method: str, url: str, payload: dict | None = None) -> tuple[int, dict]:
        body = None
        headers = {"Accept": "application/json"}
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"

        request = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return response.status, json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read().decode("utf-8"))


@pytest.fixture(scope="session")
def api() -> APIClient:
    client = APIClient(API_BASE_URL)
    deadline = time.monotonic() + 20
    last_error: Exception | None = None

    while time.monotonic() < deadline:
        try:
            status, payload = client.get("/health")
            if status == 200 and payload.get("status") == "ok":
                return client
        except Exception as error:
            last_error = error
        time.sleep(0.5)

    raise RuntimeError(f"Termind API did not become healthy at {API_BASE_URL}") from last_error

