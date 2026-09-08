/*
 *
 * Copyright 2024 gRPC authors.
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

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func TestDescriptorPathEncoding(t *testing.T) {
	previous := requireUnimplemented
	requireUnimplemented = proto.Bool(true)
	t.Cleanup(func() { requireUnimplemented = previous })
	for _, path := range []string{"ordinary/service.proto", "quote\".proto", "back\\slash.proto", "line\nfeed.proto", "carriage\rreturn.proto", "semi;colon.proto", "unicode_日本語.proto"} {
		for _, deprecated := range []bool{false, true} {
			t.Run(path+"/deprecated="+strconv.FormatBool(deprecated), func(t *testing.T) {
				gen, err := (protogen.Options{}).New(&pluginpb.CodeGeneratorRequest{
					FileToGenerate: []string{path},
					ProtoFile: []*descriptorpb.FileDescriptorProto{{
						Name: proto.String(path), Package: proto.String("test"), Syntax: proto.String("proto3"),
						Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/test;test"), Deprecated: proto.Bool(deprecated)},
						Service: []*descriptorpb.ServiceDescriptorProto{{Name: proto.String("TestService")}},
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				// Use a normal output filename so this checks source encoding independently.
				gen.Files[0].GeneratedFilenamePrefix = "test"
				content, err := generateFile(gen, gen.Files[0]).Content()
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), "test_grpc.pb.go", content, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				wantPath := strconv.Quote(path)
				wantPath = wantPath[1 : len(wantPath)-1]
				wantComment := "// source: " + wantPath
				if deprecated {
					wantComment = "// " + wantPath + " is a deprecated file."
				}
				if !strings.Contains(string(content), wantComment+"\n") {
					t.Errorf("missing source comment %q", wantComment)
				}
				count := 0
				ast.Inspect(parsed, func(n ast.Node) bool {
					kv, ok := n.(*ast.KeyValueExpr)
					if !ok {
						return true
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Metadata" {
						return true
					}
					count++
					literal, ok := kv.Value.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Errorf("Metadata is not a string literal")
						return false
					}
					got, err := strconv.Unquote(literal.Value)
					if err != nil || got != path {
						t.Errorf("Metadata = %q, %v; want %q", got, err, path)
					}
					return true
				})
				if count != 1 {
					t.Errorf("Metadata fields = %d, want 1", count)
				}
			})
		}
	}
}
