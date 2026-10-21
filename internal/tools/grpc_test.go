package tools

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/structpb"
)

// serve starts a real gRPC server on loopback and returns its address.
func serve(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	register(s)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

type testService struct {
	testpb.UnimplementedTestServiceServer
}

func (testService) UnaryCall(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	out := &testpb.SimpleResponse{Payload: &testpb.Payload{Body: make([]byte, in.GetResponseSize())}}
	if in.GetFillUsername() {
		out.Username = "aos"
	}
	return out, nil
}

// A service with no descriptor the server can describe, following AOS's Struct convention.
var riskService = grpc.ServiceDesc{
	ServiceName: "risk.RiskService",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Score",
		Handler: func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			in := &structpb.Struct{}
			if err := dec(in); err != nil {
				return nil, err
			}
			return structpb.NewStruct(map[string]any{"case": in.AsMap()["case_id"], "score": 0.42})
		},
	}},
}

func grpcTool(addr, method string) config.Tool {
	return config.Tool{Type: "grpc", Target: addr, Method: method, Insecure: true, TimeoutMs: 5000}
}

func TestGRPCCallsATypedServiceThroughReflection(t *testing.T) {
	addr := serve(t, func(s *grpc.Server) {
		hs := health.NewServer()
		healthpb.RegisterHealthServer(s, hs)
		reflection.Register(s)
	})
	out, err := ExecuteProtocolTool(context.Background(), grpcTool(addr, "/grpc.health.v1.Health/Check"), map[string]string{"service": ""})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["result"].(map[string]any)["status"]; got != "SERVING" {
		t.Fatalf("status=%v", got)
	}
}

func TestGRPCSendsRealFieldTypes(t *testing.T) {
	addr := serve(t, func(s *grpc.Server) {
		testpb.RegisterTestServiceServer(s, testService{})
		reflection.Register(s)
	})
	out, err := ExecuteProtocolTool(context.Background(), grpcTool(addr, "UnaryCall"),
		map[string]string{"response_size": "4", "fillUsername": "true"})
	if err == nil {
		t.Fatal("a method without its service must be rejected")
	}
	tool := grpcTool(addr, "UnaryCall")
	tool.Service = "grpc.testing.TestService"
	out, err = ExecuteProtocolTool(context.Background(), tool, map[string]string{"response_size": "4", "fillUsername": "true"})
	if err != nil {
		t.Fatal(err)
	}
	result := out["result"].(map[string]any)
	if result["username"] != "aos" {
		t.Fatalf("the bool did not arrive as a bool: %v", result)
	}
	if body, _ := result["payload"].(map[string]any)["body"].(string); body != "AAAAAA==" {
		t.Fatalf("the int32 did not arrive as a number: %v", result)
	}
}

func TestGRPCFallsBackToStructWithoutADescriptor(t *testing.T) {
	for _, withReflection := range []bool{true, false} {
		addr := serve(t, func(s *grpc.Server) {
			s.RegisterService(&riskService, nil)
			if withReflection {
				reflection.Register(s) // knows nothing about risk.RiskService
			}
		})
		tool := grpcTool(addr, "Score")
		tool.Service = "risk.RiskService"
		out, err := ExecuteProtocolTool(context.Background(), tool, map[string]string{"case_id": "CASE-9"})
		if err != nil {
			t.Fatalf("reflection=%v: %v", withReflection, err)
		}
		if out["result"].(map[string]any)["case"] != "CASE-9" {
			t.Fatalf("reflection=%v: %v", withReflection, out)
		}
	}
}

func TestGRPCUsesTLSUnlessToldOtherwise(t *testing.T) {
	addr := serve(t, func(s *grpc.Server) { healthpb.RegisterHealthServer(s, health.NewServer()) })
	tool := grpcTool(addr, "/grpc.health.v1.Health/Check")
	tool.Insecure = false
	tool.TimeoutMs = 2000
	_, err := ExecuteProtocolTool(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("a plaintext server must not be reachable over the default TLS")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "tls") && !strings.Contains(err.Error(), "handshake") &&
		!strings.Contains(err.Error(), "Unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}
