import json
import logging
import threading
import uuid
from concurrent import futures
from datetime import datetime, timezone
from typing import Any, Optional

import grpc
from google.protobuf import json_format
from google.protobuf.struct_pb2 import Struct
from google.protobuf.timestamp_pb2 import Timestamp

from .plugin import Plugin
from .proto.plugin.v1 import plugin_pb2 as pb
from .proto.plugin.v1 import plugin_pb2_grpc
from .relationship import RelationshipRequest
from .resource import ResourceRequest

logger = logging.getLogger(__name__)


def _now() -> Timestamp:
    value = Timestamp()
    value.FromDatetime(datetime.now(timezone.utc))
    return value


def _payload_bytes(payload: Optional[Struct]) -> bytes:
    if payload is None:
        return b"{}"
    return json.dumps(json_format.MessageToDict(payload), separators=(",", ":")).encode("utf-8")


def _struct_from_bytes(payload: Optional[bytes]) -> Struct:
    result = Struct()
    if not payload:
        return result
    try:
        value = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        value = {"value": payload.decode("utf-8", errors="replace")}
    if not isinstance(value, dict):
        value = {"value": value}
    json_format.ParseDict(value, result)
    return result


def _error(code: str, message: str) -> pb.OrionError:
    return pb.OrionError(code=code, message=message)


def _response(request_id: str, response: Any, error: Optional[Exception] = None) -> pb.InvokeResponse:
    if error is not None:
        return pb.InvokeResponse(request_id=request_id, error=_error("INTERNAL", str(error)))
    if response is None:
        return pb.InvokeResponse(request_id=request_id, error=_error("INVALID_RESPONSE", "handler returned None"))

    result = getattr(response, "result", None)
    message = pb.InvokeResponse(
        request_id=request_id,
        success=bool(response.success),
        result=_struct_from_bytes(result),
    )
    if response.error is not None:
        message.error.CopyFrom(_error(response.error.code, response.error.message))
    return message


class PluginExecutorServicer(plugin_pb2_grpc.PluginExecutorServicer):
    def __init__(self, plugin: Plugin):
        self._plugin = plugin
        self._lock = threading.RLock()
        self._operations: dict[str, pb.OperationStatus] = {}
        self._cancel_events: dict[str, threading.Event] = {}
        self._executor = futures.ThreadPoolExecutor(max_workers=10)

    def Invoke(self, request: pb.InvokeRequest, context: grpc.ServicerContext) -> pb.InvokeResponse:
        payload = _payload_bytes(request.payload)
        resource_handler = self._plugin.get_resource_handler(request.resource)
        if resource_handler is not None:
            operation = resource_handler.operations.get(request.operation)
            if operation is None:
                return pb.InvokeResponse(
                    request_id=request.request_id,
                    error=_error("NOT_SUPPORTED", f"Operation {request.operation} not supported for {request.resource}"),
                )
            try:
                return _response(
                    request.request_id,
                    operation(ResourceRequest(
                        request_id=request.request_id,
                        provider_id=request.provider_id,
                        operation=request.operation,
                        payload=payload,
                    )),
                )
            except Exception as exc:  # plugin exceptions are protocol errors
                return _response(request.request_id, None, exc)

        role, operation_name = _split_relationship_operation(request.operation)
        relationship_handler = self._plugin.get_relationship_handler(request.resource)
        if relationship_handler is not None and role:
            if relationship_handler.role != role:
                return pb.InvokeResponse(
                    request_id=request.request_id,
                    error=_error("ROLE_MISMATCH", "relationship role is not implemented by this plugin"),
                )
            operation = relationship_handler.operations.get(operation_name)
            if operation is None:
                return pb.InvokeResponse(
                    request_id=request.request_id,
                    error=_error("NOT_SUPPORTED", f"Operation {operation_name} not supported for {request.resource}"),
                )
            try:
                return _response(
                    request.request_id,
                    operation(RelationshipRequest(
                        request_id=request.request_id,
                        provider_id=request.provider_id,
                        operation=operation_name,
                        source_resource_id=request.metadata.get("source_resource_id", ""),
                        target_resource_id=request.metadata.get("target_resource_id", ""),
                        payload=payload,
                    )),
                )
            except Exception as exc:
                return _response(request.request_id, None, exc)

        return pb.InvokeResponse(
            request_id=request.request_id,
            error=_error("NOT_FOUND", f"Handler not found for {request.resource}"),
        )

    def InvokeAsync(self, request: pb.InvokeRequest, context: grpc.ServicerContext) -> pb.OperationStatus:
        operation_id = request.idempotency_key or request.request_id
        if not operation_id:
            operation_id = f"operation-{uuid.uuid4()}"
        with self._lock:
            existing = self._operations.get(operation_id)
            if existing is not None:
                return _copy_status(existing)
            status = pb.OperationStatus(
                operation_id=operation_id,
                state=pb.OPERATION_STATE_RUNNING,
                resource=request.resource,
                resource_version=request.resource_version,
                operation_name=request.operation,
                started_at=_now(),
                updated_at=_now(),
            )
            cancel_event = threading.Event()
            self._operations[operation_id] = status
            self._cancel_events[operation_id] = cancel_event
            self._executor.submit(self._run_async, request, operation_id, cancel_event)
            return _copy_status(status)

    def _run_async(self, request: pb.InvokeRequest, operation_id: str, cancel_event: threading.Event) -> None:
        response = self.Invoke(request, None)
        with self._lock:
            status = self._operations[operation_id]
            self._cancel_events.pop(operation_id, None)
            status.updated_at.CopyFrom(_now())
            status.completed_at.CopyFrom(status.updated_at)
            status.progress_percent = 100
            if cancel_event.is_set() or status.state == pb.OPERATION_STATE_CANCELLING:
                status.state = pb.OPERATION_STATE_CANCELLED
                status.error.CopyFrom(_error("CANCELLED", "operation cancelled"))
            elif not response.success:
                status.state = pb.OPERATION_STATE_FAILED
                if response.HasField("error"):
                    status.error.CopyFrom(response.error)
            else:
                status.state = pb.OPERATION_STATE_SUCCEEDED
                status.result.CopyFrom(response.result)

    def GetOperationStatus(self, request: pb.GetOperationStatusRequest, context: grpc.ServicerContext) -> pb.OperationStatus:
        with self._lock:
            status = self._operations.get(request.operation_id)
            if status is None:
                return pb.OperationStatus(
                    operation_id=request.operation_id,
                    error=_error("NOT_FOUND", "operation not found"),
                )
            return _copy_status(status)

    def CancelOperation(self, request: pb.CancelOperationRequest, context: grpc.ServicerContext) -> pb.OperationStatus:
        with self._lock:
            status = self._operations.get(request.operation_id)
            if status is None:
                return pb.OperationStatus(
                    operation_id=request.operation_id,
                    error=_error("NOT_FOUND", "operation not found"),
                )
            if status.state in (pb.OPERATION_STATE_PENDING, pb.OPERATION_STATE_RUNNING):
                status.state = pb.OPERATION_STATE_CANCELLING
                status.updated_at.CopyFrom(_now())
                event = self._cancel_events.get(request.operation_id)
                if event is not None:
                    event.set()
            return _copy_status(status)

    def Reconcile(self, request: pb.ReconcileRequest, context: grpc.ServicerContext) -> pb.ReconcileResponse:
        return pb.ReconcileResponse(
            success=False,
            error=_error("NOT_SUPPORTED", "reconcile is owned by Orion core"),
        )

    def HealthCheck(self, request: pb.HealthCheckRequest, context: grpc.ServicerContext) -> pb.HealthCheckResponse:
        return pb.HealthCheckResponse(healthy=True, message="ok")


def _copy_status(status: pb.OperationStatus) -> pb.OperationStatus:
    result = pb.OperationStatus()
    result.CopyFrom(status)
    return result


def _split_relationship_operation(operation: str) -> tuple[str, str]:
    if ":" not in operation:
        return "", operation
    return operation.split(":", 1)


def serve(plugin: Plugin, address: str = "[::]:50051") -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    plugin_pb2_grpc.add_PluginExecutorServicer_to_server(
        PluginExecutorServicer(plugin), server
    )
    bound_port = server.add_insecure_port(address)
    if bound_port == 0:
        raise RuntimeError(f"could not bind plugin server to {address}")
    server.start()
    logger.info("Orion plugin server starting on %s", address)
    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        server.stop(grace=5)
