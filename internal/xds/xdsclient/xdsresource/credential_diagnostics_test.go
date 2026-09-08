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

package xdsresource

import (
	"fmt"
	typedpb "github.com/cncf/xds/go/xds/type/v3"
	clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	_ "google.golang.org/grpc/internal/xds/clusterspecifier/rls"
	_ "google.golang.org/grpc/internal/xds/httpfilter/fault"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"strings"
	"testing"

	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

func (s) TestCredentialPayloadOmittedFromResourceErrors(t *testing.T) {
	const secret = "synthetic-private-access-token"
	token, err := anypb.New(&tokenpb.AccessTokenCredentials{Token: secret})
	if err != nil {
		t.Fatal(err)
	}
	hcm, err := anypb.New(&httppb.HttpConnectionManager{XffNumTrustedHops: 1, HttpFilters: []*httppb.HttpFilter{{Name: "filter", ConfigType: &httppb.HttpFilter_TypedConfig{TypedConfig: token}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("listener", func(t *testing.T) {
		_, err := processClientSideListener(&listenerpb.Listener{ApiListener: &listenerpb.ApiListener{ApiListener: hcm}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "xff_num_trusted_hops") || strings.Contains(err.Error(), secret) {
			t.Fatalf("listener error = %v, want safe field-specific reason", err)
		}
	})
	t.Run("network filter", func(t *testing.T) {
		_, err := processNetworkFilters([]*listenerpb.Filter{{ConfigType: &listenerpb.Filter_TypedConfig{TypedConfig: hcm}}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "missing name") || strings.Contains(err.Error(), secret) {
			t.Fatalf("network filter error = %v, want safe field-specific reason", err)
		}
	})
	t.Run("route", func(t *testing.T) {
		_, _, err := routesProtoToSlice([]*routepb.Route{{Name: "route-name", TypedPerFilterConfig: map[string]*anypb.Any{"filter": token}}}, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "doesn't have a match") || !strings.Contains(err.Error(), "route-name") || strings.Contains(err.Error(), secret) {
			t.Fatalf("route error = %v, want safe named-route reason", err)
		}
	})
}

func (s) TestHTTPFilterPayloadOmittedFromErrors(t *testing.T) {
	const secret = "synthetic-private-access-token"
	for _, filterType := range []string{"router.v3.Router", "fault.v3.HTTPFault", "rbac.v3.RBAC", "rbac.v3.RBACPerRoute"} {
		typeURL := "type.googleapis.com/envoy.extensions.filters.http." + filterType
		config, err := anypb.New(&typedpb.TypedStruct{TypeUrl: typeURL, Value: &structpb.Struct{Fields: map[string]*structpb.Value{"token": structpb.NewStringValue(secret)}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, lds := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/lds=%v", filterType, lds), func(t *testing.T) {
				_, _, err := validateHTTPFilterConfig(config, lds, false, nil, nil)
				if err == nil || !strings.Contains(err.Error(), typeURL) || strings.Contains(err.Error(), secret) {
					t.Fatalf("filter error = %v, want safe filter type and parsing reason", err)
				}
			})
		}
	}
}

func (s) TestCredentialPayloadOmittedFromClusterErrors(t *testing.T) {
	const secret = "synthetic-private-access-token"
	token, err := anypb.New(&tokenpb.AccessTokenCredentials{Token: secret})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("cluster", func(t *testing.T) {
		_, err := validateClusterAndConstructClusterUpdate(&clusterpb.Cluster{Name: "cluster-name", LbPolicy: clusterpb.Cluster_RANDOM, TypedExtensionProtocolOptions: map[string]*anypb.Any{"extension": token}}, nil)
		if err == nil || !strings.Contains(err.Error(), "unexpected lbPolicy") || !strings.Contains(err.Error(), "cluster-name") || strings.Contains(err.Error(), secret) {
			t.Fatalf("cluster error = %v, want safe cluster-name and reason", err)
		}
	})
	t.Run("endpoint", func(t *testing.T) {
		_, err := parseEndpoints([]*endpointpb.LbEndpoint{{LoadBalancingWeight: wrapperspb.UInt32(0), Metadata: &corepb.Metadata{TypedFilterMetadata: map[string]*anypb.Any{"extension": token}}}}, map[string]bool{})
		if err == nil || !strings.Contains(err.Error(), "zero weight") || strings.Contains(err.Error(), secret) {
			t.Fatalf("endpoint error = %v, want safe weight reason", err)
		}
	})
	t.Run("cluster specifier", func(t *testing.T) {
		config := &anypb.Any{TypeUrl: "type.googleapis.com/grpc.lookup.v1.RouteLookupClusterSpecifier", Value: append(append([]byte(nil), token.Value...), 0xff)}
		_, err := processClusterSpecifierPlugins([]*routepb.ClusterSpecifierPlugin{{Extension: &corepb.TypedExtensionConfig{Name: "plugin-name", TypedConfig: config}}})
		if err == nil || !strings.Contains(err.Error(), "plugin-name") || strings.Contains(err.Error(), secret) {
			t.Fatalf("cluster specifier error = %v, want safe plugin-name and reason", err)
		}
	})
}
