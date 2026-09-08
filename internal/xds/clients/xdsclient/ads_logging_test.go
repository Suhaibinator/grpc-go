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

package xdsclient

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/grpclog"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type responseLog struct {
	grpclog.DepthLoggerV2
	verbosity int
	messages  []string
}

func (l *responseLog) V(v int) bool { return v <= l.verbosity }
func (l *responseLog) InfoDepth(_ int, args ...any) {
	l.messages = append(l.messages, fmt.Sprint(args...))
}

type responseStream []byte

func (s responseStream) Recv() ([]byte, error) { return s, nil }
func (responseStream) Send([]byte) error       { return nil }

func (s) TestADSResponseLogging(t *testing.T) {
	const secret = "synthetic-private-access-token"
	token, err := anypb.New(&tokenpb.AccessTokenCredentials{Token: secret})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []*anypb.Any{token, {TypeUrl: "unknown.example/resource", Value: []byte(secret)}} {
		for _, verbosity := range []int{0, 2, 9} {
			t.Run(fmt.Sprintf("%s/verbosity%d", resource.TypeUrl, verbosity), func(t *testing.T) {
				want := &discoverypb.DiscoveryResponse{Resources: []*anypb.Any{resource}, TypeUrl: "resource-type", VersionInfo: "version", Nonce: "nonce"}
				data, err := proto.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				log := &responseLog{verbosity: verbosity}
				stream := &adsStreamImpl{logger: igrpclog.NewPrefixLogger(log, "")}
				resources, typ, version, nonce, err := stream.recvMessage(responseStream(data))
				if err != nil {
					t.Fatal(err)
				}
				if len(resources) != 1 || !proto.Equal(resources[0], resource) || typ != want.TypeUrl || version != want.VersionInfo || nonce != want.Nonce {
					t.Fatal("receiving changed resource or response metadata")
				}
				logs := strings.Join(log.messages, "\n")
				for _, value := range []string{secret, base64.StdEncoding.EncodeToString(resource.Value)} {
					if strings.Contains(logs, value) {
						t.Fatalf("response log contains credential payload: %s", logs)
					}
				}
				if verbosity == 0 && logs != "" {
					t.Fatalf("verbosity 0 logged response: %s", logs)
				}
				if verbosity >= 2 {
					for _, value := range []string{typ, version, nonce} {
						if !strings.Contains(logs, value) {
							t.Errorf("response summary missing %q: %s", value, logs)
						}
					}
				}
			})
		}
	}
}
