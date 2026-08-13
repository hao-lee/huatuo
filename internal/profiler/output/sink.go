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

package output

import (
	"context"

	"huatuo-bamai/internal/profiler"
)

// ProfileSnapshot is the transport-neutral result of one aggregation window.
type ProfileSnapshot struct {
	Profile       *profiler.ProfileData
	OverflowCount int
	TracerID      string
}

// Sink exports aggregated profiles to a repository-specific destination.
type Sink interface {
	Export(context.Context, ProfileSnapshot) error
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(context.Context, ProfileSnapshot) error

func (f SinkFunc) Export(ctx context.Context, snapshot ProfileSnapshot) error {
	return f(ctx, snapshot)
}
