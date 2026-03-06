from dataclasses import dataclass
from typing import Any, Protocol


@dataclass(slots=True)
class PublishRequest:
    room: str
    event: str
    payload: Any


class Publisher(Protocol):
    def publish(self, request: PublishRequest) -> None:
        ...
