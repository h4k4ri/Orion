module github.com/horizon/orion/plugins/manila

go 1.27.0

require (
	github.com/google/uuid v1.6.0
	github.com/horizon/orion/sdk/go v0.0.0
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/grpc v1.79.2 // indirect
)

replace github.com/horizon/orion/sdk/go => ../../sdk/go
