package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// errNoReflection means the server does not describe the method over gRPC server reflection,
// so the caller falls back to the google.protobuf.Struct convention.
var errNoReflection = errors.New("method not available over server reflection")

// resolveMethod asks the server (gRPC server reflection v1) for the file that defines the
// service, and its dependencies, and returns the method's descriptor.
func resolveMethod(ctx context.Context, conn *grpc.ClientConn, fullMethod string) (protoreflect.MethodDescriptor, error) {
	service, method, ok := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")
	if !ok || service == "" || method == "" {
		return nil, fmt.Errorf("gRPC method %q is not /package.Service/Method", fullMethod)
	}
	stream, err := rpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, reflectionErr(err)
	}
	defer stream.CloseSend()

	ask := func(req *rpb.ServerReflectionRequest) ([][]byte, error) {
		if err := stream.Send(req); err != nil {
			return nil, reflectionErr(err)
		}
		resp, err := stream.Recv()
		if err != nil {
			return nil, reflectionErr(err)
		}
		if e := resp.GetErrorResponse(); e != nil {
			return nil, fmt.Errorf("%w: %s", errNoReflection, e.GetErrorMessage())
		}
		return resp.GetFileDescriptorResponse().GetFileDescriptorProto(), nil
	}

	files := map[string]*descriptorpb.FileDescriptorProto{}
	add := func(raw [][]byte) error {
		for _, b := range raw {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(b, fd); err != nil {
				return err
			}
			files[fd.GetName()] = fd
		}
		return nil
	}
	raw, err := ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: service}})
	if err != nil {
		return nil, err
	}
	if err := add(raw); err != nil {
		return nil, err
	}
	// Fetch missing dependencies; well-known types come from the local registry.
	for pending := true; pending; {
		pending = false
		for _, fd := range files {
			for _, dep := range fd.GetDependency() {
				if _, ok := files[dep]; ok {
					continue
				}
				if local, err := protoregistry.GlobalFiles.FindFileByPath(dep); err == nil {
					files[dep] = protodesc.ToFileDescriptorProto(local)
					pending = true
					continue
				}
				raw, err := ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileByFilename{FileByFilename: dep}})
				if err != nil {
					return nil, err
				}
				if err := add(raw); err != nil {
					return nil, err
				}
				pending = true
			}
		}
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, fd := range files {
		set.File = append(set.File, fd)
	}
	registry, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("gRPC reflection: %w", err)
	}
	desc, err := registry.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errNoReflection, err)
	}
	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a service", service)
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, fmt.Errorf("service %s has no method %s", service, method)
	}
	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, fmt.Errorf("gRPC %s is streaming; a pipeline step calls unary methods only", fullMethod)
	}
	return md, nil
}

func reflectionErr(err error) error {
	if status.Code(err) == codes.Unimplemented {
		return fmt.Errorf("%w: %s", errNoReflection, err)
	}
	return err
}

// invokeTyped calls a method described over reflection: AOS's string parameters become the
// request message's real field types, and the response comes back as JSON.
func invokeTyped(ctx context.Context, conn *grpc.ClientConn, fullMethod string, md protoreflect.MethodDescriptor, params map[string]string) (map[string]any, error) {
	body, err := json.Marshal(typedFields(params, md.Input()))
	if err != nil {
		return nil, err
	}
	in := dynamicpb.NewMessage(md.Input())
	if err := (protojson.UnmarshalOptions{}).Unmarshal(body, in); err != nil {
		return nil, fmt.Errorf("gRPC %s request: %w", fullMethod, err)
	}
	out := dynamicpb.NewMessage(md.Output())
	if err := conn.Invoke(ctx, fullMethod, in, out); err != nil {
		return nil, fmt.Errorf("gRPC %s: %w", fullMethod, err)
	}
	js, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(out)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(js, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// typedFields builds the request's JSON from string parameters: booleans and numbers as JSON
// literals, messages, lists and maps parsed from their JSON text; field names may be the
// proto or the JSON name. Unknown parameters are left for protojson to reject.
func typedFields(params map[string]string, msg protoreflect.MessageDescriptor) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
		fd := msg.Fields().ByName(protoreflect.Name(k))
		if fd == nil {
			fd = msg.Fields().ByJSONName(k)
		}
		if fd == nil {
			continue
		}
		if fd.IsList() || fd.IsMap() || fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
			var j any
			if err := json.Unmarshal([]byte(v), &j); err == nil {
				out[k] = j
			}
			continue
		}
		switch fd.Kind() {
		case protoreflect.BoolKind:
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				out[k] = b
			}
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.FloatKind, protoreflect.DoubleKind:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				out[k] = f
			}
			// 64-bit integers stay strings: that is their canonical proto3 JSON form.
		}
	}
	return out
}
