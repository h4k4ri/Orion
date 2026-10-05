from dataclasses import dataclass, field
from typing import Dict, Optional


@dataclass
class OrionError:
    code: str
    message: str
    details: Dict[str, str] = field(default_factory=dict)

    def __str__(self) -> str:
        return f"{self.code}: {self.message}"

    @classmethod
    def not_found(cls, message: str) -> "OrionError":
        return cls(code="NOT_FOUND", message=message)

    @classmethod
    def invalid_input(cls, message: str) -> "OrionError":
        return cls(code="INVALID_INPUT", message=message)

    @classmethod
    def not_supported(cls, message: str) -> "OrionError":
        return cls(code="NOT_SUPPORTED", message=message)

    @classmethod
    def internal(cls, message: str) -> "OrionError":
        return cls(code="INTERNAL", message=message)
