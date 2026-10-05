from .plugin import Plugin, Config
from .error import OrionError
from .resource import ResourceRequest, ResourceResponse
from .relationship import RelationshipRequest, RelationshipResponse


def serve(plugin, address="[::]:50051"):
    """Start a blocking PluginExecutor gRPC server."""
    from .server import serve as _serve

    return _serve(plugin, address)

__all__ = [
    "Plugin",
    "Config",
    "OrionError",
    "ResourceRequest",
    "ResourceResponse",
    "RelationshipRequest",
    "RelationshipResponse",
    "serve",
]
