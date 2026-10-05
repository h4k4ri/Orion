"""gRPC bindings for the Orion PluginExecutor service.

This small generated-equivalent module is kept in source control so the SDK
does not require grpcio-tools at runtime.
"""

import grpc

from . import plugin_pb2 as plugin__v1__plugin__pb2


class PluginExecutorStub:
    def __init__(self, channel):
        self.Invoke = channel.unary_unary(
            "/plugin.v1.PluginExecutor/Invoke",
            request_serializer=plugin__v1__plugin__pb2.InvokeRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.InvokeResponse.FromString,
        )
        self.InvokeAsync = channel.unary_unary(
            "/plugin.v1.PluginExecutor/InvokeAsync",
            request_serializer=plugin__v1__plugin__pb2.InvokeRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.OperationStatus.FromString,
        )
        self.GetOperationStatus = channel.unary_unary(
            "/plugin.v1.PluginExecutor/GetOperationStatus",
            request_serializer=plugin__v1__plugin__pb2.GetOperationStatusRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.OperationStatus.FromString,
        )
        self.CancelOperation = channel.unary_unary(
            "/plugin.v1.PluginExecutor/CancelOperation",
            request_serializer=plugin__v1__plugin__pb2.CancelOperationRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.OperationStatus.FromString,
        )
        self.Reconcile = channel.unary_unary(
            "/plugin.v1.PluginExecutor/Reconcile",
            request_serializer=plugin__v1__plugin__pb2.ReconcileRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.ReconcileResponse.FromString,
        )
        self.HealthCheck = channel.unary_unary(
            "/plugin.v1.PluginExecutor/HealthCheck",
            request_serializer=plugin__v1__plugin__pb2.HealthCheckRequest.SerializeToString,
            response_deserializer=plugin__v1__plugin__pb2.HealthCheckResponse.FromString,
        )


class PluginExecutorServicer:
    def Invoke(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")

    def InvokeAsync(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")

    def GetOperationStatus(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")

    def CancelOperation(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")

    def Reconcile(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")

    def HealthCheck(self, request, context):
        context.set_code(grpc.StatusCode.UNIMPLEMENTED)
        context.set_details("Method not implemented!")
        raise NotImplementedError("Method not implemented!")


def add_PluginExecutorServicer_to_server(servicer, server):
    rpc_method_handlers = {
        "Invoke": grpc.unary_unary_rpc_method_handler(
            servicer.Invoke,
            request_deserializer=plugin__v1__plugin__pb2.InvokeRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.InvokeResponse.SerializeToString,
        ),
        "InvokeAsync": grpc.unary_unary_rpc_method_handler(
            servicer.InvokeAsync,
            request_deserializer=plugin__v1__plugin__pb2.InvokeRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.OperationStatus.SerializeToString,
        ),
        "GetOperationStatus": grpc.unary_unary_rpc_method_handler(
            servicer.GetOperationStatus,
            request_deserializer=plugin__v1__plugin__pb2.GetOperationStatusRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.OperationStatus.SerializeToString,
        ),
        "CancelOperation": grpc.unary_unary_rpc_method_handler(
            servicer.CancelOperation,
            request_deserializer=plugin__v1__plugin__pb2.CancelOperationRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.OperationStatus.SerializeToString,
        ),
        "Reconcile": grpc.unary_unary_rpc_method_handler(
            servicer.Reconcile,
            request_deserializer=plugin__v1__plugin__pb2.ReconcileRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.ReconcileResponse.SerializeToString,
        ),
        "HealthCheck": grpc.unary_unary_rpc_method_handler(
            servicer.HealthCheck,
            request_deserializer=plugin__v1__plugin__pb2.HealthCheckRequest.FromString,
            response_serializer=plugin__v1__plugin__pb2.HealthCheckResponse.SerializeToString,
        ),
    }
    generic_handler = grpc.method_handlers_generic_handler(
        "plugin.v1.PluginExecutor", rpc_method_handlers
    )
    server.add_generic_rpc_handlers((generic_handler,))

