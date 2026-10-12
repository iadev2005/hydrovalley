module github.com/iadev2005/hydrovalley/src/hub

go 1.26

require (
	github.com/coder/websocket v1.8.15
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/iadev2005/hydrovalley/gen/go v0.0.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)

replace github.com/iadev2005/hydrovalley/gen/go => ../../gen/go
