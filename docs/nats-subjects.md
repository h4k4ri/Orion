# Orion NATS subject catalog

Subjects are versioned and use the form `orion.<kind>.<domain>.<resource>.<action>.v1`.

| Kind | Subject | Owner |
|---|---|---|
| command | `orion.command.compute.instance.create.v1` | orion-compute |
| command | `orion.command.compute.instance.destroy.v1` | orion-compute |
| command | `orion.command.network.port.create.v1` | orion-network |
| command | `orion.command.volume.volume.create.v1` | orion-volume |
| event | `orion.event.compute.instance.created.v1` | orion-compute |
| event | `orion.event.compute.instance.failed.v1` | orion-compute |
| event | `orion.event.network.port.created.v1` | orion-network |
| event | `orion.event.volume.volume.created.v1` | orion-volume |
| desired | `orion.desired.compute.<host>.v1` | orion-compute |
| desired | `orion.desired.volume.<cell>.v1` | orion-volume |
| dead letter | `orion.dlq.<domain>.<resource>.<action>.v1` | owning consumer |

Breaking changes create a `v2` subject; tags are never reused.
