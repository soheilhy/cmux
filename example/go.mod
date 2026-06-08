module github.com/soheilhy/cmux/example

go 1.25.0

require (
	github.com/soheilhy/cmux v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.53.0
	google.golang.org/grpc v1.80.0
	google.golang.org/grpc/examples v0.0.0-20260605180800-0f3086db7a75
)

require (
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/soheilhy/cmux => ../
