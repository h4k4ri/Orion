# Orion Share Control

`manila-plugin` is the control-plane facade for `orion.io/storage.share`.
It selects a real storage provider per request and forwards the operation over
the Orion plugin protocol; it does not embed CephFS, NFS, SMB or GlusterFS.

Environment:

- `ORION_MANILA_DEFAULT_BACKEND` (default: `cephfs`)
- `ORION_MANILA_CEPHFS_ENDPOINT` (default: `:50056`)
- `ORION_MANILA_NFS_ENDPOINT` (default: `:50054`)
- `ORION_MANILA_SMB_ENDPOINT` (default: `:50058`)
- `ORION_MANILA_GLUSTERFS_ENDPOINT` (default: `:50053`)
- `ORION_MANILA_STATE_FILE` for an optional atomic share inventory snapshot

The selected provider must expose the corresponding Orion share resource. The
controller supports create/get/list/delete, attach/detach, resize and snapshot
operations without coupling callers to a backend protocol.
