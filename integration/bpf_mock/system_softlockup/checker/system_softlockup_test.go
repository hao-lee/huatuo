// Copyright 2026 The HuaTuo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestVerifyPublishedEvents(t *testing.T) {
	const metadata = `"hostname":"node","region":"test","tracer_name":"softlockup","tracer_type":"event","tracer_id":"id","observed_timestamp":"2026-10-13T00:00:00Z","uploaded_timestamp":"2026-10-13T00:00:01Z"`
	var actualMainDocument, actualThreadDocument document
	if err := json.Unmarshal([]byte(`{`+metadata+`,"tracer_data":{"pid":42,"cpu":0,"comm":"mock","cpus_stack":""}}`), &actualMainDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{`+metadata+`,"tracer_data":{"pid":43,"cpu":0,"comm":"mock-thread-123","cpus_stack":""}}`), &actualThreadDocument); err != nil {
		t.Fatal(err)
	}
	expectedIdentities := map[string]fixtureIdentity{"main": {PID: 42, TID: 42, Comm: "mock"}, "thread": {PID: 43, TID: 44, Comm: "mock-thread-123"}, "other-main": {PID: 100, TID: 100, Comm: "mock"}, "other-thread": {PID: 200, TID: 201, Comm: "mock"}}
	if err := verifyFixtures([]document{actualMainDocument, actualThreadDocument}, expectedIdentities); err != nil {
		t.Fatal(err)
	}
	for _, actualDocuments := range [][]document{nil, {actualMainDocument}, {actualMainDocument, actualMainDocument}, {actualMainDocument, actualThreadDocument, actualThreadDocument}} {
		if err := verifyFixtures(actualDocuments, expectedIdentities); err == nil {
			t.Fatal("missing or duplicate ES documents accepted")
		}
	}
	for _, change := range []func(*document){
		func(actualDocument *document) { actualDocument.Data.Comm = "wrong" },
		func(actualDocument *document) { actualDocument.Data.Stack = nil },
		func(actualDocument *document) { actualDocument.TracerType = "metric" },
		func(actualDocument *document) { actualDocument.Data.CPU = nil },
		func(actualDocument *document) { actualCPU := uint32(1); actualDocument.Data.CPU = &actualCPU },
		func(actualDocument *document) { actualDocument.Data.PID = 44 },
	} {
		invalidDocument := actualThreadDocument
		change(&invalidDocument)
		if err := verifyFixtures([]document{actualMainDocument, invalidDocument}, expectedIdentities); err == nil {
			t.Fatal("invalid ES event accepted")
		}
	}
	expectedIdentities["thread"] = expectedIdentities["main"]
	if err := verifyFixtures([]document{actualMainDocument, actualThreadDocument}, expectedIdentities); err == nil {
		t.Fatal("worker fixture used main thread")
	}
}

func verifyFixtures(actualDocuments []document, expectedIdentities map[string]fixtureIdentity) error {
	matchedDocuments := make(map[int]bool)
	for _, scenario := range []string{"main", "thread", "other-main", "other-thread"} {
		if err := verifyScenario(actualDocuments, scenario, expectedIdentities[scenario], matchedDocuments); err != nil {
			return err
		}
	}
	if len(matchedDocuments) != len(actualDocuments) {
		return fmt.Errorf("unmatched ES documents")
	}
	return nil
}
