# cmux: Connection Mux ![Travis Build Status](https://api.travis-ci.org/soheilhy/args.svg?branch=master "Travis Build Status") [![GoDoc](https://godoc.org/github.com/soheilhy/cmux?status.svg)](http://godoc.org/github.com/soheilhy/cmux)

cmux is a generic Go library to multiplex connections based on
their payload. Using cmux, you can serve gRPC, HTTP, and Go RPC
on the same TCP listener to avoid having to use one port per 
protocol.

## How-To
Simply create your main listener, create a cmux for that listener,
and then match connections:
```go
// Create the main listener.
l, err := net.Listen("tcp", ":23456")
if err != nil {
	log.Fatal(err)
}

// Create a cmux.
m := cmux.New(l)

// Match connections in order.
grpcL := m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
httpL := m.Match(cmux.Any()) // Any means anything that is not yet matched.

// Create your protocol servers.
grpcS := grpc.NewServer()
pb.RegisterGreeterServer(grpcs, &server{})

httpS := &http.Server{
	Handler: &testHTTP1Handler{},
}

// Use the muxed listeners for your servers.
go grpcS.Serve(grpcL)
go httpS.Serve(httpL)

// Start serving!
m.Serve()
```

Take a look at [other examples in the GoDoc](http://godoc.org/github.com/soheilhy/cmux/#pkg-examples).

## Docs
* [GoDocs](https://godoc.org/github.com/soheilhy/cmux)

## Performance
There is room for improvment but, since we are only matching
the very first bytes of a connection, the performance overheads on
long-lived connections (i.e., RPCs and pipelined HTTP streams)
is negligible.

*TODO(soheil)*: Add benchmarks.

## Limitations
*TLS*: Since `cmux` sits in between the actual listener and the mux'ed
listeners, TLS handshake is not handled inside the actual servers.
Because of that, when you handle HTTPS using cmux `http.Request.TLS`
would not be set.
