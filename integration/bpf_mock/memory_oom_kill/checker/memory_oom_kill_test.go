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
	"testing"
)

func TestVerifyESDocuments(t *testing.T) {
	// This document has only production JSON fields, without raw page counts.
	const body = `{
 "hostname":"node", "region":"test", "tracer_name":"memory_oom_kill",
 "tracer_type":"event", "tracer_id":"event-id",
 "observed_timestamp":"2026-10-12T00:00:00Z", "uploaded_timestamp":"2026-10-12T00:00:01Z",
 "tracer_data":{
  "trigger":{"pid":42,"comm":"oom-mock","memory_cgroup_css_addr":"0xffff000000000001"},
  "victim":{"pid":42,"comm":"oom-mock","memory_cgroup_css_addr":"0xffff000000000001"},
  "memory_snapshot":{"host_meminfo":{"MemTotal":4096}}
 }}`
	var validDocument document
	if err := json.Unmarshal([]byte(body), &validDocument); err != nil {
		t.Fatal(err)
	}
	observedIdentity := fixtureIdentity{
		Comm: "oom-mock", TriggerPID: 42, VictimPID: 42,
		TriggerCSS: 0xffff000000000001, VictimCSS: 0xffff000000000001,
	}
	for _, tc := range []struct {
		name        string
		change      func(*document)
		count       int
		expectError bool
	}{
		{name: "published fields", count: 1},
		{name: "missing", count: 0, expectError: true},
		{name: "duplicate", count: 2, expectError: true},
		{name: "wrong victim", count: 1, change: func(actualDocument *document) { (*actualDocument.Data.Victim.PID)++ }, expectError: true},
		{name: "wrong CSS", count: 1, change: func(actualDocument *document) { *actualDocument.Data.Victim.CSS = "0xffff000000000002" }, expectError: true},
		{name: "wrong command", count: 1, change: func(actualDocument *document) { *actualDocument.Data.Trigger.Comm = "wrong" }, expectError: true},
		{name: "unexpected task", count: 1, change: func(actualDocument *document) { (*actualDocument.Data.Trigger.PID)++ }, expectError: true},
		{name: "wrong tracer", count: 1, change: func(actualDocument *document) { actualDocument.TracerName = "softlockup" }, expectError: true},
		{name: "missing timestamp", count: 1, change: func(actualDocument *document) { actualDocument.Observed = "" }, expectError: true},
		{name: "missing snapshot", count: 1, change: func(actualDocument *document) { actualDocument.Data.Snapshot.Host = nil }, expectError: true},
		{name: "host document container ID", count: 1, change: func(actualDocument *document) { actualDocument.ContainerID = "unexpected" }, expectError: true},
		{name: "host trigger container ID", count: 1, change: func(actualDocument *document) { actualDocument.Data.Trigger.ContainerID = "unexpected" }, expectError: true},
		{name: "host victim container ID", count: 1, change: func(actualDocument *document) { actualDocument.Data.Victim.ContainerID = "unexpected" }, expectError: true},
		{name: "host trigger container hostname", count: 1, change: func(actualDocument *document) { actualDocument.Data.Trigger.Hostname = "unexpected" }, expectError: true},
		{name: "host victim container hostname", count: 1, change: func(actualDocument *document) { actualDocument.Data.Victim.Hostname = "unexpected" }, expectError: true},
		{name: "host trigger cgroup snapshot", count: 1, change: func(actualDocument *document) { actualDocument.Data.Trigger.Cgroup = &cgroupSnapshot{} }, expectError: true},
		{name: "host victim cgroup snapshot", count: 1, change: func(actualDocument *document) { actualDocument.Data.Victim.Cgroup = &cgroupSnapshot{} }, expectError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actualDocument document
			if err := json.Unmarshal([]byte(body), &actualDocument); err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(&actualDocument)
			}
			actualDocuments := make([]document, tc.count)
			for i := range actualDocuments {
				actualDocuments[i] = actualDocument
			}
			err := verifyScenario(actualDocuments, "same-task", observedIdentity, make(map[int]bool), nil)
			if (err != nil) != tc.expectError {
				t.Fatalf("verify error=%v, want error=%v", err, tc.expectError)
			}
		})
	}
	matchedDocuments := make(map[int]bool)
	if err := verifyScenario([]document{validDocument}, "same-task", observedIdentity, matchedDocuments, nil); err != nil {
		t.Fatal(err)
	}
	if !matchedDocuments[0] {
		t.Fatal("verified document was not marked as matched")
	}
	if err := verifyScenario([]document{validDocument}, "memcg", observedIdentity, matchedDocuments, nil); err == nil {
		t.Fatal("same document matched two scenarios")
	}
	if err := verifyScenario(nil, "null-control", observedIdentity, make(map[int]bool), nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyScenario([]document{validDocument}, "null-control", observedIdentity, make(map[int]bool), nil); err == nil {
		t.Fatal("null control emitted a document")
	}
}

func TestExpectedPublishedActors(t *testing.T) {
	for _, name := range []string{"same-task", "other-task", "memcg", "other-task-memcg", "pointer-chain", "null-victim"} {
		t.Run(name, func(t *testing.T) {
			observedIdentity := fixtureIdentity{Comm: "oom-mock", TriggerPID: 42, VictimPID: 42, TriggerCSS: 0xffff000000000001, VictimCSS: 0xffff000000000001}
			expectedVictim := actor{PID: 42, Comm: "oom-mock", CSS: "0xffff000000000001"}
			switch name {
			case "other-task", "other-task-memcg", "pointer-chain":
				observedIdentity.VictimPID = 43
				expectedVictim.PID = 43
				expectedVictim.Comm = "oom-mock-victim"
				if name == "pointer-chain" {
					observedIdentity.VictimPID, expectedVictim.PID = 4242, 4242
					observedIdentity.VictimCSS = 0xffff000000000002
					expectedVictim.CSS = "0xffff000000000002"
				}
			case "null-victim":
				observedIdentity.VictimPID = 0
				observedIdentity.VictimCSS = 0
				expectedVictim = actor{}
			}
			builtEvent, err := expected(name, observedIdentity)
			if err != nil {
				t.Fatal(err)
			}
			if builtEvent.Victim != expectedVictim || builtEvent.Trigger != (actor{PID: 42, Comm: "oom-mock", CSS: "0xffff000000000001"}) {
				t.Fatalf("unexpected actors: %+v", builtEvent)
			}
		})
	}
}

func TestContainerEvents(t *testing.T) {
	const body = `{
 "hostname":"node", "region":"test", "tracer_name":"memory_oom_kill",
 "tracer_type":"event", "tracer_id":"id", "container_id":"container-id",
 "observed_timestamp":"2026-10-13T00:00:00Z", "uploaded_timestamp":"2026-10-13T00:00:01Z",
 "tracer_data":{
  "trigger":{"pid":42,"comm":"oom-mock","memory_cgroup_css_addr":"0xffff000000000001",
   "container_id":"container-id","container_hostname":"oom-trigger",
   "cgroup":{"path":"/pods/container-id","max":67108864,"current":4096,"stat":{"anon":4096}}},
  "victim":{"pid":42,"comm":"oom-mock","memory_cgroup_css_addr":"0xffff000000000001",
   "container_id":"container-id","container_hostname":"oom-trigger",
   "cgroup":{"path":"/pods/container-id","max":67108864,"current":4096,"stat":{"anon":4096}}},
  "memory_snapshot":{"host_meminfo":{"MemTotal":4096}}
 }}`
	var expectedContainer containerMetadata
	expectedContainer.Data.ID = "container-id"
	expectedContainer.Data.Hostname = "oom-trigger"
	expectedContainer.Data.CSS = map[string]string{"memory": "0xffff000000000001"}
	observedIdentity := fixtureIdentity{
		Comm: "oom-mock", TriggerPID: 42, VictimPID: 42,
		TriggerCSS: 0xffff000000000001, VictimCSS: 0xffff000000000001,
	}
	for _, tc := range []struct {
		name   string
		change func(*document)
	}{
		{name: "valid"},
		{name: "document ID", change: func(actualDocument *document) { actualDocument.ContainerID = "wrong" }},
		{name: "trigger ID", change: func(actualDocument *document) { actualDocument.Data.Trigger.ContainerID = "wrong" }},
		{name: "victim ID", change: func(actualDocument *document) { actualDocument.Data.Victim.ContainerID = "wrong" }},
		{name: "hostname", change: func(actualDocument *document) { actualDocument.Data.Victim.Hostname = "wrong" }},
		{name: "CSS", change: func(actualDocument *document) { *actualDocument.Data.Trigger.CSS = "wrong" }},
		{name: "missing snapshot", change: func(actualDocument *document) { actualDocument.Data.Trigger.Cgroup = nil }},
		{name: "path", change: func(actualDocument *document) { actualDocument.Data.Victim.Cgroup.Path = "/other" }},
		{name: "limit", change: func(actualDocument *document) { actualDocument.Data.Victim.Cgroup.Max++ }},
		{name: "usage", change: func(actualDocument *document) { actualDocument.Data.Trigger.Cgroup.Current = 0 }},
		{name: "statistics", change: func(actualDocument *document) { actualDocument.Data.Trigger.Cgroup.Stat = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actualDocument document
			if err := json.Unmarshal([]byte(body), &actualDocument); err != nil {
				t.Fatal(err)
			}
			if tc.change != nil {
				tc.change(&actualDocument)
			}
			err := verifyScenario([]document{actualDocument}, "same-task", observedIdentity, make(map[int]bool), &expectedContainer)
			if (err != nil) != (tc.change != nil) {
				t.Fatalf("unexpected verification result: %v", err)
			}
		})
	}
	var actualDocument document
	if err := json.Unmarshal([]byte(body), &actualDocument); err != nil {
		t.Fatal(err)
	}
	actualDocument.ContainerID = ""
	zero := uint32(0)
	empty := ""
	actualDocument.Data.Victim = &containerActor{PID: &zero, Comm: &empty, CSS: &empty}
	observedIdentity.VictimPID, observedIdentity.VictimCSS = 0, 0
	if err := verifyScenario([]document{actualDocument}, "null-victim", observedIdentity, make(map[int]bool), &expectedContainer); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*document){
		func(actualDocument *document) { actualDocument.ContainerID = "container-id" },
		func(actualDocument *document) { actualDocument.Data.Victim.ContainerID = "container-id" },
		func(actualDocument *document) { actualDocument.Data.Victim.Hostname = "oom-trigger" },
		func(actualDocument *document) { actualDocument.Data.Victim.Cgroup = &cgroupSnapshot{} },
	} {
		invalidDocument := actualDocument
		victimCopy := *actualDocument.Data.Victim
		invalidDocument.Data.Victim = &victimCopy
		change(&invalidDocument)
		if err := verifyScenario([]document{invalidDocument}, "null-victim", observedIdentity, make(map[int]bool), &expectedContainer); err == nil {
			t.Fatal("null victim accepted container metadata")
		}
	}
}

func TestRequiredActorFields(t *testing.T) {
	const body = `{
 "hostname":"node", "region":"test", "tracer_name":"memory_oom_kill",
 "tracer_type":"event", "tracer_id":"id",
 "observed_timestamp":"2026-10-13T00:00:00Z", "uploaded_timestamp":"2026-10-13T00:00:01Z",
 "tracer_data":{
  "trigger":{"pid":42,"comm":"oom-mock","memory_cgroup_css_addr":"0xffff000000000001"},
  "victim":{"pid":0,"comm":"","memory_cgroup_css_addr":""},
  "memory_snapshot":{"host_meminfo":{"MemTotal":4096}}
 }}`
	observedIdentity := fixtureIdentity{Comm: "oom-mock", TriggerPID: 42, TriggerCSS: 0xffff000000000001}
	var validDocument document
	if err := json.Unmarshal([]byte(body), &validDocument); err != nil {
		t.Fatal(err)
	}
	if err := verifyScenario([]document{validDocument}, "null-victim", observedIdentity, make(map[int]bool), nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"trigger", "victim"} {
		for _, field := range []string{"object", "pid", "comm", "memory_cgroup_css_addr"} {
			for _, mode := range []string{"missing", "null"} {
				t.Run(name+"/"+field+"/"+mode, func(t *testing.T) {
					var input map[string]any
					if err := json.Unmarshal([]byte(body), &input); err != nil {
						t.Fatal(err)
					}
					data := input["tracer_data"].(map[string]any)
					target, key := data, name
					if field != "object" {
						target, key = data[name].(map[string]any), field
					}
					if mode == "null" {
						target[key] = nil
					} else {
						delete(target, key)
					}
					encoded, err := json.Marshal(input)
					if err != nil {
						t.Fatal(err)
					}
					var actualDocument document
					if err := json.Unmarshal(encoded, &actualDocument); err != nil {
						t.Fatal(err)
					}
					if err := verifyScenario([]document{actualDocument}, "null-victim", observedIdentity, make(map[int]bool), nil); err == nil {
						t.Fatal("missing or null actor data accepted")
					}
				})
			}
		}
	}
}

func TestNullControlPreparedIdentity(t *testing.T) {
	// Preparation observes chosen=current; running the scenario passes NULL to the hook.
	observedIdentity := fixtureIdentity{
		Comm: "oom-mock", TriggerPID: 42, VictimPID: 42,
		TriggerCSS: 0xffff000000000001, VictimCSS: 0xffff000000000001,
	}
	builtEvent, err := expected("null-control", observedIdentity)
	if err != nil || builtEvent != nil {
		t.Fatalf("null control: event=%+v, error=%v", builtEvent, err)
	}
}
