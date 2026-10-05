# Orion object storage plugin

Este plugin usa `mc` para operar backends S3, MinIO e gateways S3 do Ceph. O
recurso `orion.io/storage.object.swift` fornece o contrato de objetos usado
pelos consumidores Swift do Orion, sem duplicar um segundo daemon.

Configure `ORION_S3_ENDPOINT` e `ORION_S3_ALIAS` para um alias persistente, ou
envie `endpoint`/`alias` na operação. O plugin implementa buckets, objetos,
metadata, copy, versionamento, lifecycle, CORS e encryption. Multipart upload
é delegado ao streaming multipart automático do `mc`; por isso não é anunciado
como uma API de sessões multipart independentes.
