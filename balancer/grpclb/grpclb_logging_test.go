/*
 *
 * Copyright 2016 gRPC authors.
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

package grpclb

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/balancer"
	lbpb "google.golang.org/grpc/balancer/grpclb/grpc_lb_v1"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

type serverListLogger struct {
	grpclog.DepthLoggerV2
	messages []string
}

func (l *serverListLogger) V(int) bool { return true }
func (l *serverListLogger) InfoDepth(_ int, args ...any) {
	l.messages = append(l.messages, fmt.Sprint(args...))
}

func (s) TestServerListLogging(t *testing.T) {
	const backendToken = "backend-private-token"
	const dropToken = "drop-private-token"
	cc := testutils.NewBalancerClientConn(t)
	log := &serverListLogger{}
	lb := &lbBalancer{
		cc:           newLBCacheClientConn(cc),
		logger:       igrpclog.NewPrefixLogger(log, ""),
		usePickFirst: true,
		subConns:     make(map[resolver.Address]balancer.SubConn),
		scStates:     make(map[balancer.SubConn]connectivity.State),
	}
	list := &lbpb.ServerList{Servers: []*lbpb.Server{
		{IpAddress: []byte{127, 0, 0, 1}, Port: 1234, LoadBalanceToken: backendToken},
		{Drop: true, LoadBalanceToken: dropToken},
	}}
	lb.processServerList(list)
	lb.processServerList(list)
	logs := strings.Join(log.messages, "\n")
	for _, token := range []string{backendToken, dropToken} {
		if strings.Contains(logs, token) {
			t.Errorf("server list logging contains token %q: %s", token, logs)
		}
	}
	for _, want := range []string{"2 entries", "127.0.0.1", "1234", "same as the previous one"} {
		if !strings.Contains(logs, want) {
			t.Errorf("server list logging missing %q: %s", want, logs)
		}
	}
	if got := imetadata.Get(lb.backendAddrs[0])[lbTokenKey]; len(got) != 1 || got[0] != backendToken {
		t.Errorf("backend token metadata = %v, want %q", got, backendToken)
	}
	if list.Servers[0].LoadBalanceToken != backendToken || list.Servers[1].LoadBalanceToken != dropToken {
		t.Fatal("processing changed server list tokens")
	}
}
