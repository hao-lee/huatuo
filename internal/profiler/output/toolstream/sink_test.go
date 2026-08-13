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

package toolstream

import (
	"context"
	"errors"
	"testing"

	"huatuo-bamai/core/autotracing"
	"huatuo-bamai/internal/profiler"
	"huatuo-bamai/internal/profiler/output"
)

type fakeEventSender struct {
	event any
	err   error
}

func (s *fakeEventSender) Send(event any) error {
	s.event = event
	return s.err
}

func TestSinkExport(t *testing.T) {
	sender := &fakeEventSender{}
	sink := New(sender, "container-123")
	profile := &profiler.ProfileData{ProfileType: profiler.ProfileTypeMemSample}

	err := sink.Export(context.Background(), output.ProfileSnapshot{
		Profile:       profile,
		OverflowCount: 11,
		TracerID:      "trace-123",
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	event, ok := sender.event.(*autotracing.ProfilerEvent)
	if !ok {
		t.Fatalf("event type = %T, want *autotracing.ProfilerEvent", sender.event)
	}
	if event.TracerID != "trace-123" || event.ContainerID != "container-123" {
		t.Fatalf(
			"event identity = (%q, %q), want (trace-123, container-123)",
			event.TracerID,
			event.ContainerID,
		)
	}

	got, ok := event.TracerData.(*tracerData)
	if !ok {
		t.Fatalf("TracerData type = %T, want *tracerData", event.TracerData)
	}
	if got.FlameData != profile {
		t.Fatalf("FlameData = %p, want %p", got.FlameData, profile)
	}
	metric, ok := got.MetricData.(*metrics)
	if !ok {
		t.Fatalf("MetricData type = %T, want *metrics", got.MetricData)
	}
	if metric.AggrOverflowCount != 11 {
		t.Fatalf("AggrOverflowCount = %d, want 11", metric.AggrOverflowCount)
	}
}

func TestSinkExportReturnsSendError(t *testing.T) {
	want := errors.New("toolstream unavailable")
	sink := New(&fakeEventSender{err: want}, "")

	err := sink.Export(context.Background(), output.ProfileSnapshot{
		Profile:  &profiler.ProfileData{},
		TracerID: "trace-123",
	})
	if !errors.Is(err, want) {
		t.Fatalf("Export() error = %v, want %v", err, want)
	}
}
