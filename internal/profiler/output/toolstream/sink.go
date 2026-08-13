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
	"fmt"
	"time"

	"huatuo-bamai/core/autotracing"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/profiler"
	"huatuo-bamai/internal/profiler/output"
	"huatuo-bamai/pkg/tracing"
)

const profilerTracerName = "profiler"

type eventSender interface {
	Send(any) error
}

// Sink exports aggregated profiles through the toolstream client.
type Sink struct {
	sender      eventSender
	containerID string
}

// New creates a Toolstream-backed profile sink.
func New(sender eventSender, containerID string) *Sink {
	return &Sink{sender: sender, containerID: containerID}
}

// Export sends one aggregated profile as a profiler event.
func (s *Sink) Export(_ context.Context, snapshot output.ProfileSnapshot) error {
	if s.sender == nil {
		return fmt.Errorf("export profiling snapshot: toolstream sender is nil")
	}
	if snapshot.Profile == nil {
		return fmt.Errorf("export profiling snapshot: profile is nil")
	}

	event := &autotracing.ProfilerEvent{
		TracerID:      snapshot.TracerID,
		ContainerID:   s.containerID,
		TracerName:    profilerTracerName,
		TracerRunType: tracing.TracerRunTypeTask,
		TracerTime:    time.Now().Format("2006-01-02 15:04:05.000 -0700"),
		TracerData: &tracerData{
			MetricData: newMetrics(snapshot.OverflowCount),
			FlameData:  snapshot.Profile,
		},
	}

	if err := s.sender.Send(event); err != nil {
		log.WithField("tracer_id", snapshot.TracerID).
			Errorf("failed to send profiling event: %v", err)
		return err
	}

	log.WithField("tracer_id", snapshot.TracerID).
		Infof("profiling event sent via toolstream")
	return nil
}

type tracerData struct {
	MetricData any                   `json:"metric_data,omitempty"`
	FlameData  *profiler.ProfileData `json:"flamedata"`
}

type metrics struct {
	StartTime         time.Time `json:"start_time"`
	AggrOverflowCount int       `json:"aggr_overflow_count"`
}

func newMetrics(count int) *metrics {
	return &metrics{
		StartTime:         time.Now(),
		AggrOverflowCount: count,
	}
}
