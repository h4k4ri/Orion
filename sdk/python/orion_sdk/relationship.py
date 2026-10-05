from dataclasses import dataclass
from typing import Any, Callable, Dict, Optional
import json

from .error import OrionError


@dataclass
class RelationshipRequest:
    request_id: str
    provider_id: str
    operation: str
    source_resource_id: str
    target_resource_id: str
    payload: bytes

    def payload_json(self) -> Any:
        return json.loads(self.payload.decode("utf-8"))


@dataclass
class RelationshipResponse:
    success: bool
    result: Optional[bytes] = None
    error: Optional[OrionError] = None

    @classmethod
    def success_response(cls, data: Any) -> "RelationshipResponse":
        payload = json.dumps(data).encode("utf-8")
        return cls(success=True, result=payload)

    @classmethod
    def error_response(cls, code: str, message: str) -> "RelationshipResponse":
        return cls(
            success=False,
            error=OrionError(code=code, message=message)
        )


RelationshipOperation = Callable[[RelationshipRequest], RelationshipResponse]


@dataclass
class RelationshipHandler:
    kind: str
    role: str
    operations: Dict[str, RelationshipOperation]
    version: str = "v1"
