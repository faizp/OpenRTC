from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, cast
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


@dataclass(slots=True)
class PublishRequest:
    room: str
    event: str
    payload: Any
    exclude_sender_conn_id: str | None = None
    trace_id: str | None = None


@dataclass(slots=True)
class Stats:
    active_connections: int = 0
    active_rooms: int = 0
    joins_total: int = 0
    leaves_total: int = 0
    events_total: int = 0
    presence_updates_total: int = 0
    queue_overflows_total: int = 0
    admin_publishes_total: int = 0


class OpenRTCError(Exception):
    def __init__(self, code: str, message: str, request_id: str | None, status_code: int) -> None:
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message
        self.request_id = request_id
        self.status_code = status_code


class AdminClient:
    def __init__(
        self, base_url: str, token: str, *, timeout: float = 5.0, retries: int = 1
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._token = token
        self._timeout = timeout
        self._retries = retries

    def publish(self, request: PublishRequest) -> None:
        payload = {
            "room": request.room,
            "event": request.event,
            "payload": request.payload,
        }
        if request.exclude_sender_conn_id is not None:
            payload["exclude_sender_conn_id"] = request.exclude_sender_conn_id
        if request.trace_id is not None:
            payload["trace_id"] = request.trace_id

        self._request("POST", "/v1/publish", payload)

    def stats(self) -> Stats:
        payload = self._request("GET", "/v1/stats", None)
        return Stats(**payload)

    def _request(self, method: str, path: str, payload: dict[str, Any] | None) -> dict[str, Any]:
        body = None if payload is None else json.dumps(payload).encode("utf-8")
        request = Request(
            f"{self._base_url}{path}",
            data=body,
            method=method,
            headers={
                "Authorization": f"Bearer {self._token}",
                "Content-Type": "application/json",
            },
        )

        attempts = self._retries + 1
        for attempt in range(attempts):
            try:
                with urlopen(request, timeout=self._timeout) as response:
                    content = response.read()
                    if not content:
                        return {}
                    return cast(dict[str, Any], json.loads(content.decode("utf-8")))
            except HTTPError as error:
                raw = error.read().decode("utf-8")
                payload = json.loads(raw) if raw else {}
                raise OpenRTCError(
                    payload.get("code", "INTERNAL"),
                    payload.get("message", "request failed"),
                    payload.get("request_id"),
                    error.code,
                ) from error
            except URLError:
                if attempt == attempts - 1:
                    raise

        return {}
