// Package grpcrisk is a gRPC risk-scoring mock for the protocols demo: risk.RiskService/Score
// takes and returns google.protobuf.Struct (AOS's convention for services it cannot describe),
// with server reflection on, so a client sees reflection without a descriptor for this service.
package grpcrisk

import (
	"context"
	"hash/fnv"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/structpb"
)

var service = grpc.ServiceDesc{
	ServiceName: "risk.RiskService",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Score",
		Handler: func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			in := &structpb.Struct{}
			if err := dec(in); err != nil {
				return nil, err
			}
			customer, _ := in.AsMap()["customerId"].(string)
			h := fnv.New32a()
			_, _ = h.Write([]byte(customer))
			score := float64(h.Sum32()%100) / 100
			band := "low"
			if score >= 0.7 {
				band = "high"
			} else if score >= 0.4 {
				band = "medium"
			}
			return structpb.NewStruct(map[string]any{"customerId": customer, "score": score, "band": band})
		},
	}},
	Metadata: "risk.proto",
}

// Serve listens on addr (plaintext, local demo only) until the listener fails.
func Serve(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s := grpc.NewServer()
	s.RegisterService(&service, nil)
	reflection.Register(s)
	return s.Serve(lis)
}
