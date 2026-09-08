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

package interop

import (
	"strings"
	"testing"
)

func (s) TestServiceAccountUsername(t *testing.T) {
	const key = `{"client_email":"account@example.com","private_key":"PRIVATE_KEY_MARKER"}`
	for _, test := range []struct {
		name, key, user string
		wantErr         bool
	}{
		{"exact identity", key, "account@example.com", false},
		{"different identity", key, "other@example.com", true},
		{"empty identity", key, "", true},
		{"partial identity", key, "account", true},
		{"unrelated field", key, "PRIVATE_KEY_MARKER", true},
		{"escaped identity", `{"client_email":"account\u0040example.com"}`, "account@example.com", false},
		{"missing identity", `{"private_key":"PRIVATE_KEY_MARKER"}`, "", true},
		{"empty account", `{"client_email":""}`, "", true},
		{"invalid type", `{"client_email":{"PRIVATE_KEY_MARKER":1}}`, "", true},
		{"invalid JSON", `{"private_key":"PRIVATE_KEY_MARKER"`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkServiceAccountUsername([]byte(test.key), test.user)
			if (err != nil) != test.wantErr {
				t.Fatalf("checkServiceAccountUsername() error = %v, want error %v", err, test.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_KEY_MARKER") {
				t.Fatalf("error includes private key: %v", err)
			}
		})
	}
}
