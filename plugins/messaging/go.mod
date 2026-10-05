module github.com/horizon/orion/plugins/messaging

go 1.27.0

require (
	github.com/google/uuid v1.6.0
	github.com/horizon/orion/sdk/go v0.0.0
	github.com/nats-io/nats-server/v2 v2.10.20
	github.com/nats-io/nats.go v1.38.0
	github.com/segmentio/kafka-go v0.4.47
)

replace github.com/horizon/orion/sdk/go => ../../sdk/go

require (
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/minio/highwayhash v1.0.3 // indirect
	github.com/nats-io/jwt/v2 v2.5.8 // indirect
	github.com/nats-io/nkeys v0.4.9 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/grpc v1.79.2 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
