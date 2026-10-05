from dataclasses import dataclass
from typing import Any, Callable, Dict, Optional, AnyStr
import json

from .error import OrionError


@dataclass
class ResourceRequest:
    request_id: str
    provider_id: str
    operation: str
    payload: bytes

    def payload_json(self) -> Any:
        return json.loads(self.payload.decode("utf-8"))


@dataclass
class ResourceResponse:
    success: bool
    result: Optional[bytes] = None
    error: Optional[OrionError] = None

    @classmethod
    def success_response(cls, data: Any) -> "ResourceResponse":
        payload = json.dumps(data).encode("utf-8")
        return cls(success=True, result=payload)

    @classmethod
    def error_response(cls, code: str, message: str) -> "ResourceResponse":
        return cls(
            success=False,
            error=OrionError(code=code, message=message)
        )


ResourceOperation = Callable[[ResourceRequest], ResourceResponse]


@dataclass
class ResourceHandler:
    kind: str
    version: str
    operations: Dict[str, ResourceOperation]
    capabilities: Dict[str, Any]
