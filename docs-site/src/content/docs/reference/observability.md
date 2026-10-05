---
title: Observabilidade
description: Estado atual de metricas e tracing do Orion com OpenTelemetry.
---

O Orion agora usa `OpenTelemetry` no control plane em Go.

## O que existe hoje

- todos os servicos HTTP em Go expõem `GET /metrics`;
- todas as rotas HTTP passam por instrumentacao `otelhttp`;
- `compute`, `network` e `volume` criam spans de aplicacao nas operacoes centrais;
- `request_id` entra como atributo `orion.request_id` no span atual.

## Exportacao

Cada servico HTTP inicializa:

- metricas Prometheus por processo;
- tracing `OTLP/HTTP` quando `OTEL_EXPORTER_OTLP_ENDPOINT` ou `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` estiver definido.

Sem endpoint OTLP configurado, o servico continua expondo metricas e cria spans locais sem exporter remoto.

## Endpoints

Exemplos:

```bash
curl -s http://127.0.0.1:8080/metrics
curl -s http://127.0.0.1:8081/metrics
curl -s http://127.0.0.1:8083/metrics
curl -s http://127.0.0.1:8086/metrics
curl -s http://127.0.0.1:8087/metrics
```

## Escopo atual

Hoje a instrumentacao cobre:

- `orion-api`
- `orion-identity`
- `orion-placement`
- `orion-compute`
- `orion-image`
- `orion-network`
- `orion-volume`

Os agentes em Rust ainda nao estao emitindo OpenTelemetry nativo. Esse e um passo posterior.
