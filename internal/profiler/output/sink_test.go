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
	"testing"

	"huatuo-bamai/internal/profiler"
)

func TestSinkFunc(t *testing.T) {
	want := ProfileSnapshot{
		Profile:       &profiler.ProfileData{ProfileType: profiler.ProfileTypeMemSample},
		OverflowCount: 7,
		TracerID:      "trace-123",
	}

	var got ProfileSnapshot
	sink := SinkFunc(func(_ context.Context, snapshot ProfileSnapshot) error {
		got = snapshot
		return nil
	})

	if err := sink.Export(context.Background(), want); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if got.Profile != want.Profile ||
		got.OverflowCount != want.OverflowCount ||
		got.TracerID != want.TracerID {
		t.Fatalf("Export() snapshot = %#v, want %#v", got, want)
	}
}
