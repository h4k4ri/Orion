from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional
import json

from .resource import ResourceHandler, ResourceOperation, ResourceRequest, ResourceResponse
from .relationship import RelationshipHandler, RelationshipOperation, RelationshipRequest, RelationshipResponse
from .error import OrionError


@dataclass
class Config:
    id: str
    name: str
    version: str
    vendor: str


class Plugin:
    def __init__(self, config: Config):
        self.config = config
        self._resources: Dict[str, ResourceHandler] = {}
        self._relationships: Dict[str, RelationshipHandler] = {}

    def resource(self, kind: str, version: str) -> "ResourceBuilder":
        return ResourceBuilder(self, kind, version)

    def relationship(self, kind: str, role: str, version: str = "v1") -> "RelationshipBuilder":
        return RelationshipBuilder(self, kind, role, version)

    def get_resource_handler(self, kind: str) -> Optional[ResourceHandler]:
        return self._resources.get(kind)

    def get_relationship_handler(self, kind: str) -> Optional[RelationshipHandler]:
        return self._relationships.get(kind)

    def list_resources(self) -> List[ResourceHandler]:
        return list(self._resources.values())

    def list_relationships(self) -> List[RelationshipHandler]:
        return list(self._relationships.values())

    def get_config(self) -> Config:
        return self.config

    def _register_resource(self, handler: ResourceHandler) -> None:
        self._resources[handler.kind] = handler

    def _register_relationship(self, handler: RelationshipHandler) -> None:
        self._relationships[handler.kind] = handler

    def build_manifest(self) -> Dict[str, Any]:
        return {
            "apiVersion": "orion.io/v1",
            "pluginId": self.config.id,
            "name": self.config.name,
            "version": self.config.version,
            "vendor": self.config.vendor,
            "runtime": {
                "protocol": "grpc",
                "protocolVersion": "v1",
            },
            "implements": [
                {
                    "kind": r.kind,
                    "version": r.version,
                    "operations": list(r.operations.keys()),
                    "capabilities": r.capabilities,
                }
                for r in self._resources.values()
            ],
            "implementsRelationships": [
                {
                    "kind": rel.kind,
                    "version": rel.version,
                    "role": rel.role,
                    "operations": list(rel.operations.keys()),
                }
                for rel in self._relationships.values()
            ],
        }


class ResourceBuilder:
    def __init__(self, plugin: Plugin, kind: str, version: str):
        self.plugin = plugin
        self.kind = kind
        self.version = version
        self.operations: Dict[str, ResourceOperation] = {}
        self.capabilities: Dict[str, Any] = {}

    def handle(self, operation: str, handler: ResourceOperation) -> "ResourceBuilder":
        self.operations[operation] = handler
        return self

    def add_capability(self, name: str, value: Any) -> "ResourceBuilder":
        self.capabilities[name] = value
        return self

    def register(self) -> None:
        handler = ResourceHandler(
            kind=self.kind,
            version=self.version,
            operations=self.operations,
            capabilities=self.capabilities,
        )
        self.plugin._register_resource(handler)


class RelationshipBuilder:
    def __init__(self, plugin: Plugin, kind: str, role: str, version: str):
        self.plugin = plugin
        self.kind = kind
        self.role = role
        self.version = version
        self.operations: Dict[str, RelationshipOperation] = {}

    def handle(self, operation: str, handler: RelationshipOperation) -> "RelationshipBuilder":
        self.operations[operation] = handler
        return self

    def register(self) -> None:
        handler = RelationshipHandler(
            kind=self.kind,
            role=self.role,
            operations=self.operations,
            version=self.version,
        )
        self.plugin._register_relationship(handler)


def new_plugin(config: Config) -> Plugin:
    return Plugin(config)
