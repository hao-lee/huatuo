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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	bpfmock "github.com/ccfos/huatuo/integration/bpf_mock/common"
)

type fixtureIdentity struct {
	PID       uint32 `json:"pid"`
	TID       uint32 `json:"tid"`
	Comm      string `json:"comm"`
	ElapsedNS uint64 `json:"elapsed_ns"`
}

type event struct {
	PID        uint32 `json:"pid"`
	TID        uint32 `json:"tid"`
	Comm       string `json:"comm"`
	DurationNS uint64 `json:"reclaim_duration_ns"`
}

type document = bpfmock.Document[event]

type containerMetadata struct {
	Data struct {
		ID  string            `json:"id"`
		CSS map[string]string `json:"cgroup_css"`
	} `json:"data"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mock memory reclaim ES verification:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 5 {
		return fmt.Errorf("usage: %s ES_DOCUMENTS_JSON TRIGGER_DIR CONTAINER_JSON FILTER_THRESHOLD_NS [SCENARIO...]", os.Args[0])
	}
	actualDocuments, err := bpfmock.ReadDocuments[event](os.Args[1])
	if err != nil {
		return err
	}
	expectedContainer, err := bpfmock.ReadJSON[containerMetadata](os.Args[3])
	if err != nil {
		return err
	}
	if expectedContainer.Data.ID == "" || expectedContainer.Data.CSS["cpu"] == "" {
		return errors.New("incomplete discovered container identity or CPU CSS")
	}
	filteredThreshold, err := strconv.ParseUint(os.Args[4], 10, 64)
	if err != nil || filteredThreshold == 0 {
		return fmt.Errorf("invalid filter threshold %q", os.Args[4])
	}
	scenarios := os.Args[5:]
	if len(scenarios) == 0 {
		scenarios = []string{"host-main", "report-main", "report-thread", "filtered-main", "filtered-thread"}
	}
	matchedDocuments := make(map[int]bool)
	for _, scenario := range scenarios {
		expectedIdentity, err := bpfmock.ReadJSON[fixtureIdentity](filepath.Join(os.Args[2], scenario+".json"))
		if err != nil {
			return err
		}
		if err := verifyScenario(actualDocuments, scenario, expectedIdentity, expectedContainer.Data.ID, filteredThreshold, matchedDocuments); err != nil {
			return err
		}
	}
	if len(matchedDocuments) != len(actualDocuments) {
		return fmt.Errorf("unexpected ES documents: matched %d of %d", len(matchedDocuments), len(actualDocuments))
	}
	fmt.Println("PASS memory reclaim: entry/return timing, task identity, container attribution, host exclusion, and threshold filtering verified")
	return nil
}

func verifyScenario(actualDocuments []document, scenario string, expectedIdentity fixtureIdentity,
	expectedContainerID string, filteredThreshold uint64, matchedDocuments map[int]bool,
) error {
	var threaded, producesEvent, filtered bool
	switch scenario {
	case "host-main":
	case "report-main":
		producesEvent = true
	case "report-thread":
		producesEvent, threaded = true, true
	case "filtered-main":
		filtered = true
	case "filtered-thread":
		filtered, threaded = true, true
	default:
		return fmt.Errorf("unknown scenario %q", scenario)
	}
	const minimumDurationNS = 20 * 1000 * 1000
	if expectedIdentity.PID == 0 || expectedIdentity.TID == 0 || expectedIdentity.Comm == "" ||
		threaded != (expectedIdentity.PID != expectedIdentity.TID) || expectedIdentity.ElapsedNS < minimumDurationNS {
		return fmt.Errorf("%s: invalid userspace task identity or elapsed time", scenario)
	}
	if filtered && expectedIdentity.ElapsedNS >= filteredThreshold {
		return fmt.Errorf("%s: trigger exceeded the configured filtering threshold", scenario)
	}
	return bpfmock.MatchScenario(actualDocuments, scenario, matchedDocuments, producesEvent,
		func(actualDocument *document) bool {
			return actualDocument.Data.PID == expectedIdentity.PID && actualDocument.Data.TID == expectedIdentity.TID
		},
		func(actualDocument *document) error {
			if err := actualDocument.ValidateMetadata("memory_reclaim"); err != nil {
				return err
			}
			if actualDocument.ContainerID != expectedContainerID || actualDocument.Data.Comm != expectedIdentity.Comm {
				return errors.New("incorrect container attribution or command name")
			}
			if actualDocument.Data.DurationNS < minimumDurationNS || actualDocument.Data.DurationNS > expectedIdentity.ElapsedNS {
				return fmt.Errorf("BPF duration %d outside userspace bounds [%d, %d]",
					actualDocument.Data.DurationNS, minimumDurationNS, expectedIdentity.ElapsedNS)
			}
			return nil
		})
}
