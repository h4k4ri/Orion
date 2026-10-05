import json
import logging
from orion_sdk import (
    Config,
    Plugin,
    ResourceRequest,
    ResourceResponse,
    OrionError,
    serve,
)

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def create_volume(req: ResourceRequest) -> ResourceResponse:
    try:
        spec = req.payload_json()
        volume_id = f"{spec['pool']}/{spec['name']}"

        logger.info(f"Creating volume: {volume_id}")

        return ResourceResponse.success_response({
            "id": volume_id,
            "name": spec["name"],
            "size": spec["size"],
        })
    except json.JSONDecodeError as e:
        return ResourceResponse.error_response("INVALID_INPUT", str(e))
    except KeyError as e:
        return ResourceResponse.error_response("INVALID_INPUT", f"Missing field: {e}")


def delete_volume(req: ResourceRequest) -> ResourceResponse:
    try:
        spec = req.payload_json()
        logger.info(f"Deleting volume: {spec.get('id', 'unknown')}")
        return ResourceResponse.success_response({})
    except Exception as e:
        return ResourceResponse.error_response("INTERNAL", str(e))


def resize_volume(req: ResourceRequest) -> ResourceResponse:
    try:
        spec = req.payload_json()
        logger.info(f"Resizing volume: {spec.get('id', 'unknown')}")
        return ResourceResponse.success_response({})
    except Exception as e:
        return ResourceResponse.error_response("INTERNAL", str(e))


def main():
    plugin = Plugin(Config(
        id="zfs-plugin",
        name="ZFS",
        version="1.0.0",
        vendor="Orion",
    ))

    plugin.resource("orion.io/storage.volume", "v1").handle(
        "create", create_volume
    ).handle(
        "delete", delete_volume
    ).handle(
        "resize", resize_volume
    ).add_capability(
        "snapshots", True
    ).add_capability(
        "encryption", False
    ).register()

    logger.info("ZFS plugin starting on :50051")
    serve(plugin, ":50051")


if __name__ == "__main__":
    main()
