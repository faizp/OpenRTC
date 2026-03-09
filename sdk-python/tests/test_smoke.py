from __future__ import annotations

import json
from pathlib import Path
from unittest.mock import Mock, patch
from urllib.error import HTTPError

from openrtc_publisher import AdminClient, OpenRTCError, PublishRequest

FIXTURE_ROOT = Path(__file__).resolve().parents[2] / "testdata" / "contracts"

def test_publish_request_fields() -> None:
    request = PublishRequest(room="tenant:room", event="evt", payload={"ok": True})

    assert request.room == "tenant:room"
    assert request.event == "evt"
    assert request.payload == {"ok": True}


def test_admin_client_publish_success() -> None:
    fixture = json.loads((FIXTURE_ROOT / "publish-request.json").read_text())
    client = AdminClient("https://openrtc.example.com", "token")

    response = Mock()
    response.read.return_value = b""
    response.__enter__ = Mock(return_value=response)
    response.__exit__ = Mock(return_value=False)

    with patch("openrtc_publisher.publisher.urlopen", return_value=response) as mocked_urlopen:
        client.publish(PublishRequest(**fixture))

    assert mocked_urlopen.called


def test_admin_client_maps_http_error() -> None:
    fixture = (FIXTURE_ROOT / "error-response.json").read_text().encode("utf-8")
    client = AdminClient("https://openrtc.example.com", "token")

    error = HTTPError(
        url="https://openrtc.example.com/v1/publish",
        code=403,
        msg="Forbidden",
        hdrs=None,
        fp=Mock(read=Mock(return_value=fixture)),
    )

    with patch("openrtc_publisher.publisher.urlopen", side_effect=error):
        try:
            client.publish(
                PublishRequest(room="tenant-a:room-1", event="evt", payload={"ok": True})
            )
        except OpenRTCError as exc:
            assert exc.code == "ROOM_FORBIDDEN"
            assert exc.request_id == "req-123"
            assert exc.status_code == 403
        else:
            raise AssertionError("expected OpenRTCError")
