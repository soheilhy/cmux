package cmux_test

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	grpchello "github.com/grpc/grpc-common/go/helloworld"
	"github.com/soheilhy/cmux"
)

type exampleHTTPHandler struct{}

func (h *exampleHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "example http response")
}

func serveHTTP(l net.Listener) {
	s := &http.Server{
		Handler: &exampleHTTPHandler{},
	}
	s.Serve(l)
}

type ExampleRPCRcvr struct{}

func (r *ExampleRPCRcvr) Cube(i int, j *int) error {
	*j = i * i
	return nil
}

func serveRPC(l net.Listener) {
	s := rpc.NewServer()
	s.Register(&ExampleRPCRcvr{})
	s.Accept(l)
}

type grpcServer struct{}

func (s *grpcServer) SayHello(ctx context.Context, in *grpchello.HelloRequest) (
	*grpchello.HelloReply, error) {

	return &grpchello.HelloReply{Message: "Hello " + in.Name + " from cmux"}, nil
}

func serveGRPC(l net.Listener) {
	grpcs := grpc.NewServer()
	grpchello.RegisterGreeterServer(grpcs, &grpcServer{})
	grpcs.Serve(l)
}

func Example() {
	l, err := net.Listen("tcp", "127.0.0.1:50051")
	if err != nil {
		log.Fatal(err)
	}

	m := cmux.New(l)

	// We first match the connection against HTTP2 fields. If matched, the
	// connection will be sent through the "grpcl" listener.
	grpcl := m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
	// Otherwise, we match it againts HTTP1 methods and HTTP2. If matched by
	// any of them, it is sent through the "httpl" listener.
	httpl := m.Match(cmux.HTTP1Fast(), cmux.HTTP2())
	// If not matched by HTTP, we assume it is an RPC connection.
	rpcl := m.Match(cmux.Any())

	// Then we used the muxed listeners.
	go serveGRPC(grpcl)
	go serveHTTP(httpl)
	go serveRPC(rpcl)

	m.Serve()
}
