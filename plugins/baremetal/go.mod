module github.com/horizon/orion/plugins/baremetal

go 1.27.0

require (
	github.com/bougou/go-ipmi v0.5.12
	github.com/go-chi/chi/v5 v5.3.2
	github.com/horizon/orion/sdk/go v0.0.0
	github.com/horizon/orion/sdk/go/httpapi v0.0.0
	github.com/horizon/orion/sdk/go/schema v0.0.0
	github.com/stmcginnis/gofish v0.12.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/pretty v0.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/rogpeppe/go-internal v1.6.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/otel/sdk v1.42.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/grpc v1.79.2 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/horizon/orion/sdk/go => ../../sdk/go

replace github.com/horizon/orion/sdk/go/httpapi => ../../sdk/go/httpapi

replace github.com/horizon/orion/sdk/go/schema => ../../sdk/go/schema
