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

package bpfmock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type Document[Event any] struct {
	ContainerID string `json:"container_id"`
	TracerName  string `json:"tracer_name"`
	TracerType  string `json:"tracer_type"`
	TracerID    string `json:"tracer_id"`
	Hostname    string `json:"hostname"`
	Region      string `json:"region"`
	Observed    string `json:"observed_timestamp"`
	Uploaded    string `json:"uploaded_timestamp"`
	Data        Event  `json:"tracer_data"`
}

func (d *Document[Event]) ValidateMetadata(tracer string) error {
	if d.TracerName != tracer || d.TracerType != "event" || d.TracerID == "" ||
		d.Hostname == "" || d.Region == "" {
		return errors.New("incomplete or incorrect ES event metadata")
	}
	for _, timestamp := range []string{d.Observed, d.Uploaded} {
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil || parsed.IsZero() {
			return fmt.Errorf("invalid ES event timestamp %q", timestamp)
		}
	}
	return nil
}

// ReadDocuments decodes the _source records retrieved by the ES shell helper.
func ReadDocuments[Event any](path string) ([]Document[Event], error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var documents []Document[Event]
	decoder := json.NewDecoder(file)
	for {
		var doc Document[Event]
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return documents, nil
			}
			return nil, fmt.Errorf("decode ES document: %w", err)
		}
		documents = append(documents, doc)
	}
}

func ReadJSON[Value any](path string) (Value, error) {
	var value Value
	body, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(body, &value)
	}
	if err != nil {
		return value, fmt.Errorf("read %s: %w", path, err)
	}
	return value, nil
}

// MatchScenario enforces cardinality and exclusive ownership of ES documents.
// Feature checkers retain their own identity selection and field assertions.
func MatchScenario[Event any](documents []Document[Event], scenario string,
	matched map[int]bool, producesEvent bool, selectDocument func(*Document[Event]) bool,
	verify func(*Document[Event]) error,
) error {
	want := 0
	if producesEvent {
		want = 1
	}
	count := 0
	for i := range documents {
		doc := &documents[i]
		if !selectDocument(doc) {
			continue
		}
		count++
		if count > want {
			return fmt.Errorf("%s: expected %d ES documents, received at least %d", scenario, want, count)
		}
		if matched[i] {
			return fmt.Errorf("%s: ES document already matched another scenario", scenario)
		}
		if err := verify(doc); err != nil {
			return fmt.Errorf("%s: %w", scenario, err)
		}
		matched[i] = true
	}
	if count != want {
		return fmt.Errorf("%s: expected %d ES documents, received %d", scenario, want, count)
	}
	return nil
}
