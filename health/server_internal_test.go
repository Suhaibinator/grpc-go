/*
 *
 * Copyright 2018 gRPC authors.
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

package health

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestShutdown(t *testing.T) {
	const testService = "tteesstt"
	s := NewServer()
	s.SetServingStatus(testService, healthpb.HealthCheckResponse_SERVING)

	status := s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_SERVING)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Run SetServingStatus and Shutdown in parallel.
	go func() {
		for i := 0; i < 1000; i++ {
			s.SetServingStatus(testService, healthpb.HealthCheckResponse_SERVING)
			time.Sleep(time.Microsecond)
		}
		wg.Done()
	}()
	go func() {
		time.Sleep(300 * time.Microsecond)
		s.Shutdown()
		wg.Done()
	}()
	wg.Wait()

	s.mu.Lock()
	status = s.statusMap[testService]
	s.mu.Unlock()
	if status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_NOT_SERVING)
	}

	s.Resume()
	status = s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_SERVING)
	}

	s.SetServingStatus(testService, healthpb.HealthCheckResponse_NOT_SERVING)
	status = s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_NOT_SERVING)
	}
}

// TestList verifies that List() returns the health status of all the services if no. of services are within
// maxAllowedLimits.
func (s) TestList(t *testing.T) {
	s := NewServer()

	// Fill out status map with information
	const length = 3
	for i := 0; i < length; i++ {
		s.SetServingStatus(fmt.Sprintf("%d", i),
			healthpb.HealthCheckResponse_ServingStatus(i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var in healthpb.HealthListRequest
	got, err := s.List(ctx, &in)

	if err != nil {
		t.Fatalf("s.List(ctx, &in) returned err %v, want nil", err)
	}
	if len(got.GetStatuses()) != length+1 {
		t.Fatalf("len(out.GetStatuses()) = %d, want %d",
			len(got.GetStatuses()), length+1)
	}
	want := &healthpb.HealthListResponse{
		Statuses: map[string]*healthpb.HealthCheckResponse{
			"":  {Status: healthpb.HealthCheckResponse_SERVING},
			"0": {Status: healthpb.HealthCheckResponse_UNKNOWN},
			"1": {Status: healthpb.HealthCheckResponse_SERVING},
			"2": {Status: healthpb.HealthCheckResponse_NOT_SERVING},
		},
	}
	if diff := cmp.Diff(got, want, protocmp.Transform()); diff != "" {
		t.Fatalf("Health response did not match expectation.  Diff (-got, +want): %s", diff)
	}
}

// TestListResourceExhausted verifies that List()
// returns a ResourceExhausted error if no. of services are more than
// maxAllowedServices.
func (s) TestListResourceExhausted(t *testing.T) {
	s := NewServer()

	// Fill out status map with service information,
	// 101 (100 + 1 existing) elements will trigger an error.
	for i := 1; i <= maxAllowedServices; i++ {
		s.SetServingStatus(fmt.Sprintf("%d", i),
			healthpb.HealthCheckResponse_SERVING)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var in healthpb.HealthListRequest
	_, err := s.List(ctx, &in)

	want := status.Errorf(codes.ResourceExhausted,
		"server health list exceeds maximum capacity: %d", maxAllowedServices)
	if !errors.Is(err, want) {
		t.Fatalf("s.List(ctx, &in) returned %v, want %v", err, want)
	}
}

// watchServer provides bounded in-process status delivery for lifecycle tests.
type watchServer struct {
	healthpb.Health_WatchServer
	ctx       context.Context
	responses chan healthpb.HealthCheckResponse_ServingStatus
	sendErr   error
}

func (w *watchServer) Context() context.Context { return w.ctx }

func (w *watchServer) Send(r *healthpb.HealthCheckResponse) error {
	if w.sendErr != nil {
		return w.sendErr
	}
	select {
	case w.responses <- r.Status:
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}

func (s) TestWatchSubscriberCleanup(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprintf("known_%v", known), func(t *testing.T) {
			server := NewServer()
			const service = "service"
			initial := healthpb.HealthCheckResponse_SERVICE_UNKNOWN
			if known {
				initial = healthpb.HealthCheckResponse_SERVING
				server.SetServingStatus(service, initial)
			}
			start := func() (*watchServer, context.CancelFunc, <-chan error) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				t.Cleanup(cancel)
				w := &watchServer{ctx: ctx, responses: make(chan healthpb.HealthCheckResponse_ServingStatus, 1)}
				done := make(chan error, 1)
				go func() { done <- server.Watch(&healthpb.HealthCheckRequest{Service: service}, w) }()
				return w, cancel, done
			}
			receive := func(w *watchServer, want healthpb.HealthCheckResponse_ServingStatus) {
				t.Helper()
				select {
				case got := <-w.responses:
					if got != want {
						t.Fatalf("status = %v, want %v", got, want)
					}
				case <-w.ctx.Done():
					t.Fatal("timed out waiting for status")
				}
			}
			stop := func(cancel context.CancelFunc, done <-chan error) {
				t.Helper()
				cancel()
				select {
				case err := <-done:
					if status.Code(err) != codes.Canceled {
						t.Fatalf("Watch returned %v, want Canceled", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for Watch to exit")
				}
			}
			first, cancelFirst, doneFirst := start()
			second, cancelSecond, doneSecond := start()
			receive(first, initial)
			receive(second, initial)
			stop(cancelFirst, doneFirst)
			server.mu.RLock()
			remaining := len(server.updates[service])
			server.mu.RUnlock()
			if remaining != 1 {
				t.Fatalf("remaining subscribers = %d, want 1", remaining)
			}
			server.SetServingStatus(service, healthpb.HealthCheckResponse_NOT_SERVING)
			receive(second, healthpb.HealthCheckResponse_NOT_SERVING)
			stop(cancelSecond, doneSecond)
			if _, ok := server.updates[service]; ok {
				t.Fatal("last subscriber left an updates entry")
			}
			// A new watch still receives the configured status after cleanup.
			third, cancelThird, doneThird := start()
			receive(third, healthpb.HealthCheckResponse_NOT_SERVING)
			stop(cancelThird, doneThird)
			if len(server.updates) != 0 {
				t.Fatalf("updates contains %d entries, want none", len(server.updates))
			}
		})
	}
}

func (s) TestWatchSendFailureCleanup(t *testing.T) {
	server := NewServer()
	w := &watchServer{ctx: context.Background(), sendErr: errors.New("send failed")}
	if err := server.Watch(&healthpb.HealthCheckRequest{Service: "service"}, w); status.Code(err) != codes.Canceled {
		t.Fatalf("Watch returned %v, want Canceled", err)
	}
	if len(server.updates) != 0 {
		t.Fatalf("updates contains %d entries, want none", len(server.updates))
	}
}
