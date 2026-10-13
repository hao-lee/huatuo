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

// fixtureIdentity is recorded in userspace before the mock hook runs.
type fixtureIdentity struct {
	PID  uint32 `json:"pid"`
	TID  uint32 `json:"tid"`
	CPU  uint32 `json:"cpu"`
	Comm string `json:"comm"`
}

type event struct {
	TID     uint32  `json:"tid"`
	Comm    string  `json:"comm"`
	Timeout *int    `json:"hung_task_timeout_secs"`
	Stack   *string `json:"cpus_stack"`
	Blocked *string `json:"blocked_processes_stack"`
}

type document = bpfmock.Document[event]

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mock hungtask ES verification:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: %s ES_DOCUMENTS_JSON TRIGGER_DIR TIMEOUT_SECONDS", os.Args[0])
	}
	actualDocuments, err := bpfmock.ReadDocuments[event](os.Args[1])
	if err != nil {
		return err
	}
	expectedTimeout, err := strconv.Atoi(os.Args[3])
	if err != nil || expectedTimeout < 0 {
		return fmt.Errorf("invalid hung task timeout %q", os.Args[3])
	}
	matchedDocuments := make(map[int]bool)
	for _, scenario := range []string{"main", "thread"} {
		expectedIdentity, err := bpfmock.ReadJSON[fixtureIdentity](filepath.Join(os.Args[2], scenario+".json"))
		if err != nil {
			return err
		}
		if err := verifyScenario(actualDocuments, scenario, expectedIdentity, matchedDocuments, expectedTimeout); err != nil {
			return err
		}
	}
	if len(matchedDocuments) != len(actualDocuments) {
		return fmt.Errorf("unexpected ES documents: matched %d of %d", len(matchedDocuments), len(actualDocuments))
	}
	fmt.Printf("PASS hungtask: main and worker events verified against ES documents\n")
	return nil
}

func verifyScenario(
	actualDocuments []document,
	scenario string,
	expectedIdentity fixtureIdentity,
	matchedDocuments map[int]bool,
	expectedTimeout int,
) error {
	if expectedIdentity.PID == 0 || expectedIdentity.TID == 0 || expectedIdentity.Comm == "" ||
		(scenario == "thread" && expectedIdentity.PID == expectedIdentity.TID) || (scenario == "main" && expectedIdentity.PID != expectedIdentity.TID) {
		return fmt.Errorf("%s: invalid trigger task identity", scenario)
	}
	return bpfmock.MatchScenario(actualDocuments, scenario, matchedDocuments, true,
		func(actualDocument *document) bool { return actualDocument.Data.TID == expectedIdentity.TID },
		func(actualDocument *document) error {
			if err := actualDocument.ValidateMetadata("hungtask"); err != nil {
				return err
			}
			if actualDocument.Data.Comm != expectedIdentity.Comm {
				return fmt.Errorf("command name: got %q, want %q", actualDocument.Data.Comm, expectedIdentity.Comm)
			}
			if actualDocument.Data.Stack == nil {
				return errors.New("missing CPU stack string")
			}
			if actualDocument.Data.Blocked == nil {
				return errors.New("missing blocked-task stack string")
			}
			if actualDocument.Data.Timeout == nil {
				return errors.New("missing hung task timeout")
			}
			if *actualDocument.Data.Timeout != expectedTimeout {
				return fmt.Errorf("hung task timeout: got %d, want %d", *actualDocument.Data.Timeout, expectedTimeout)
			}
			return nil
		})
}
