from openrtc_publisher import PublishRequest


def test_publish_request_fields() -> None:
    request = PublishRequest(room="tenant:room", event="evt", payload={"ok": True})

    assert request.room == "tenant:room"
    assert request.event == "evt"
    assert request.payload == {"ok": True}
