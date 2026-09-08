/*
 *
 * Copyright 2026 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package extproc

import (
	"strings"
	"testing"

	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	filterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func (s) TestCredentialPayloadOmittedFromConfigErrors(t *testing.T) {
	const secret = "synthetic-private-access-token"
	token, err := anypb.New(&tokenpb.AccessTokenCredentials{Token: secret})
	if err != nil {
		t.Fatal(err)
	}
	config, err := anypb.New(&filterpb.ExternalProcessor{GrpcService: &corepb.GrpcService{
		TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
			TargetUri: "processor.example:443", CallCredentialsPlugin: []*anypb.Any{token},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	original := proto.Clone(config)
	for _, test := range []struct {
		name   string
		config proto.Message
		reason string
	}{
		{"missing mode", config, "missing processing_mode"},
		{"wrong message type", &tokenpb.AccessTokenCredentials{Token: secret}, "unknown type"},
		{"wrong Any type", token, "failed to unmarshal config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := (builder{}).ParseFilterConfig(test.config, httpfilter.ParseOptions{})
			if err == nil || !strings.Contains(err.Error(), test.reason) || strings.Contains(err.Error(), secret) {
				t.Fatalf("ParseFilterConfig() error = %v, want safe reason %q", err, test.reason)
			}
		})
	}
	if !proto.Equal(config, original) {
		t.Fatal("diagnostics changed the credential-bearing configuration")
	}
}
