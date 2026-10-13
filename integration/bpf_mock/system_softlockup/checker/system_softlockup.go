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
	PID   uint32  `json:"pid"`
	Comm  string  `json:"comm"`
	CPU   *uint32 `json:"cpu"`
	Stack *string `json:"cpus_stack"`
}

type document = bpfmock.Document[event]

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mock softlockup ES verification:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: %s ES_DOCUMENTS_JSON TRIGGER_DIR", os.Args[0])
	}
	actualDocuments, err := bpfmock.ReadDocuments[event](os.Args[1])
	if err != nil {
		return err
	}
	matchedDocuments := make(map[int]bool)
	for _, scenario := range []string{"main", "thread", "other-main", "other-thread"} {
		expectedIdentity, err := bpfmock.ReadJSON[fixtureIdentity](filepath.Join(os.Args[2], scenario+".json"))
		if err != nil {
			return err
		}
		if err := verifyScenario(actualDocuments, scenario, expectedIdentity, matchedDocuments); err != nil {
			return err
		}
	}
	if len(matchedDocuments) != len(actualDocuments) {
		return fmt.Errorf("unexpected ES documents: matched %d of %d", len(matchedDocuments), len(actualDocuments))
	}
	fmt.Printf("PASS softlockup: main, worker, and rejected taint flags verified against ES documents\n")
	return nil
}

func verifyScenario(
	actualDocuments []document,
	scenario string,
	expectedIdentity fixtureIdentity,
	matchedDocuments map[int]bool,
) error {
	rejectedScenario := scenario == "other-main" || scenario == "other-thread"
	if expectedIdentity.PID == 0 || expectedIdentity.TID == 0 || expectedIdentity.Comm == "" ||
		((scenario == "thread" || scenario == "other-thread") && expectedIdentity.PID == expectedIdentity.TID) ||
		((scenario == "main" || scenario == "other-main") && expectedIdentity.PID != expectedIdentity.TID) {
		return fmt.Errorf("%s: invalid trigger task identity", scenario)
	}
	return bpfmock.MatchScenario(actualDocuments, scenario, matchedDocuments, !rejectedScenario,
		func(actualDocument *document) bool { return actualDocument.Data.PID == expectedIdentity.PID },
		func(actualDocument *document) error {
			if err := actualDocument.ValidateMetadata("softlockup"); err != nil {
				return err
			}
			if actualDocument.Data.Comm != expectedIdentity.Comm {
				return fmt.Errorf("command name: got %q, want %q", actualDocument.Data.Comm, expectedIdentity.Comm)
			}
			if actualDocument.Data.Stack == nil {
				return errors.New("missing CPU stack string")
			}
			if actualDocument.Data.CPU == nil {
				return errors.New("missing CPU")
			}
			if *actualDocument.Data.CPU != expectedIdentity.CPU {
				return fmt.Errorf("CPU: got %d, want %d", *actualDocument.Data.CPU, expectedIdentity.CPU)
			}
			return nil
		})
}
