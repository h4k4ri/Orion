module github.com/horizon/orion/connectors/identity-keycloak

go 1.27.0

require (
	github.com/coreos/go-oidc/v3 v3.11.0
	github.com/horizon/orion/sdk/go/identity v0.0.0
	golang.org/x/oauth2 v0.24.0
)

replace github.com/horizon/orion/sdk/go/identity => ../../sdk/go/identity

require (
	github.com/go-jose/go-jose/v4 v4.0.4 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	golang.org/x/crypto v0.32.0 // indirect
	golang.org/x/net v0.34.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.36.2 // indirect
)
