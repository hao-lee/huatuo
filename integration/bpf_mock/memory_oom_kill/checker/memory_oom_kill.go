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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bpfmock "github.com/ccfos/huatuo/integration/bpf_mock/common"
	"github.com/ccfos/huatuo/internal/utils/kernaddr"
)

// fixtureIdentity combines userspace process IDs with API or kernel CSS observations.
type fixtureIdentity struct {
	Comm       string `json:"comm"`
	TriggerPID uint32 `json:"trigger_pid"`
	VictimPID  uint32 `json:"victim_pid"`
	TriggerCSS uint64 `json:"trigger_css"`
	VictimCSS  uint64 `json:"victim_css"`
}

type actor struct {
	PID  uint32 `json:"pid"`
	Comm string `json:"comm"`
	CSS  string `json:"memory_cgroup_css_addr"`
}

type oomEvent struct {
	Trigger actor `json:"trigger"`
	Victim  actor `json:"victim"`
}

type cgroupSnapshot struct {
	Path    string            `json:"path"`
	Max     uint64            `json:"max"`
	Current uint64            `json:"current"`
	Stat    map[string]uint64 `json:"stat"`
}

type containerActor struct {
	PID         *uint32         `json:"pid"`
	Comm        *string         `json:"comm"`
	CSS         *string         `json:"memory_cgroup_css_addr"`
	ContainerID string          `json:"container_id"`
	Hostname    string          `json:"container_hostname"`
	Cgroup      *cgroupSnapshot `json:"cgroup"`
}

type containerMetadata struct {
	Data struct {
		ID       string            `json:"id"`
		Hostname string            `json:"hostname"`
		CSS      map[string]string `json:"cgroup_css"`
	} `json:"data"`
}

type oomData struct {
	Trigger  *containerActor `json:"trigger"`
	Victim   *containerActor `json:"victim"`
	Snapshot struct {
		Host map[string]uint64 `json:"host_meminfo"`
	} `json:"memory_snapshot"`
}

type document = bpfmock.Document[oomData]

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mock OOM ES verification:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	expectedContainerPath := flags.String("container", "", "expected container identity and memory CSS JSON")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	args := flags.Args()
	if len(args) < 3 {
		return fmt.Errorf("usage: %s [-container METADATA_JSON] ES_DOCUMENTS_JSON TRIGGER_DIR SCENARIO...", os.Args[0])
	}
	var expectedContainer *containerMetadata
	if *expectedContainerPath != "" {
		expectedMetadata, err := bpfmock.ReadJSON[containerMetadata](*expectedContainerPath)
		if err != nil {
			return err
		}
		if expectedMetadata.Data.ID == "" || expectedMetadata.Data.Hostname == "" || expectedMetadata.Data.CSS["memory"] == "" {
			return errors.New("incomplete discovered container metadata")
		}
		expectedContainer = &expectedMetadata
	}
	actualDocuments, err := bpfmock.ReadDocuments[oomData](args[0])
	if err != nil {
		return err
	}
	matchedDocuments := make(map[int]bool)
	for _, scenario := range args[2:] {
		observedIdentity, err := bpfmock.ReadJSON[fixtureIdentity](filepath.Join(args[1], scenario+".json"))
		if err != nil {
			return err
		}
		if err := verifyScenario(actualDocuments, scenario, observedIdentity, matchedDocuments, expectedContainer); err != nil {
			return err
		}
	}
	if len(matchedDocuments) != len(actualDocuments) {
		return fmt.Errorf("unexpected ES documents: matched %d of %d", len(matchedDocuments), len(actualDocuments))
	}
	fmt.Printf("PASS OOM: %d scenarios verified against %d ES documents\n", len(args[2:]), len(actualDocuments))
	return nil
}

func verifyScenario(
	actualDocuments []document,
	scenario string,
	observedIdentity fixtureIdentity,
	matchedDocuments map[int]bool,
	expectedContainer *containerMetadata,
) error {
	expectedEvent, err := expected(scenario, observedIdentity)
	if err != nil {
		return fmt.Errorf("%s: %w", scenario, err)
	}
	return bpfmock.MatchScenario(actualDocuments, scenario, matchedDocuments, expectedEvent != nil,
		func(actualDocument *document) bool {
			return actualDocument.Data.Trigger != nil && actualDocument.Data.Trigger.PID != nil && *actualDocument.Data.Trigger.PID == observedIdentity.TriggerPID
		},
		func(actualDocument *document) error {
			actualTrigger, err := actualDocument.Data.Trigger.identity()
			if err != nil {
				return fmt.Errorf("trigger: %w", err)
			}
			actualVictim, err := actualDocument.Data.Victim.identity()
			if err != nil {
				return fmt.Errorf("victim: %w", err)
			}
			if actualTrigger != expectedEvent.Trigger || actualVictim != expectedEvent.Victim {
				return fmt.Errorf("actor mismatch: got trigger %+v, victim %+v; want %+v", actualTrigger, actualVictim, expectedEvent)
			}
			if err := actualDocument.ValidateMetadata("memory_oom_kill"); err != nil {
				return err
			}
			if actualDocument.Data.Snapshot.Host["MemTotal"] == 0 {
				return errors.New("missing ES host memory snapshot")
			}
			if err := verifyContainer(actualDocument, expectedContainer); err != nil {
				return err
			}
			return nil
		})
}

func expected(scenario string, observedIdentity fixtureIdentity) (*oomEvent, error) {
	if observedIdentity.TriggerPID == 0 || observedIdentity.TriggerCSS == 0 || observedIdentity.Comm == "" {
		return nil, errors.New("fixture preparation reported an invalid trigger identity")
	}
	if scenario == "null-victim" {
		if observedIdentity.VictimPID != 0 || observedIdentity.VictimCSS != 0 {
			return nil, errors.New("fixture did not clear the victim")
		}
	} else if observedIdentity.VictimPID == 0 || observedIdentity.VictimCSS == 0 {
		return nil, errors.New("fixture preparation reported an invalid victim identity")
	}
	if scenario == "null-control" {
		return nil, nil
	}
	expectedVictimComm := observedIdentity.Comm
	switch scenario {
	case "other-task", "other-task-memcg":
		expectedVictimComm = "oom-mock-victim"
		if observedIdentity.TriggerPID == observedIdentity.VictimPID {
			return nil, errors.New("fixture did not create a distinct victim")
		}
	case "pointer-chain":
		expectedVictimComm = "oom-mock-victim"
		if observedIdentity.VictimPID != 4242 ||
			observedIdentity.TriggerPID == observedIdentity.VictimPID ||
			observedIdentity.TriggerCSS == observedIdentity.VictimCSS {
			return nil, errors.New("invalid synthetic victim identity or cgroup pointer chain")
		}
	case "null-victim":
		expectedVictimComm = ""
	case "same-task", "memcg":
		if observedIdentity.TriggerPID != observedIdentity.VictimPID ||
			observedIdentity.TriggerCSS != observedIdentity.VictimCSS {
			return nil, errors.New("fixture did not use the trigger as victim")
		}
	default:
		return nil, fmt.Errorf("unknown scenario %q", scenario)
	}
	return &oomEvent{
		Trigger: actor{
			PID:  observedIdentity.TriggerPID,
			Comm: observedIdentity.Comm,
			CSS:  kernaddr.Format(observedIdentity.TriggerCSS),
		},
		Victim: actor{
			PID:  observedIdentity.VictimPID,
			Comm: expectedVictimComm,
			CSS:  kernaddr.Format(observedIdentity.VictimCSS),
		},
	}, nil
}

func verifyContainer(actualDocument *document, expectedContainer *containerMetadata) error {
	if err := verifyContainerActor(actualDocument.Data.Trigger, expectedContainer); err != nil {
		return fmt.Errorf("trigger: %w", err)
	}
	expectedVictimContainer := expectedContainer
	if *actualDocument.Data.Victim.PID == 0 {
		expectedVictimContainer = nil
	}
	if err := verifyContainerActor(actualDocument.Data.Victim, expectedVictimContainer); err != nil {
		return fmt.Errorf("victim: %w", err)
	}
	expectedContainerID := ""
	if expectedVictimContainer != nil {
		expectedContainerID = expectedVictimContainer.Data.ID
	}
	if actualDocument.ContainerID != expectedContainerID {
		return fmt.Errorf("document container ID: got %q, want %q", actualDocument.ContainerID, expectedContainerID)
	}
	return nil
}

func verifyContainerActor(actualActor *containerActor, expectedContainer *containerMetadata) error {
	if expectedContainer == nil {
		if actualActor.ContainerID != "" || actualActor.Hostname != "" || actualActor.Cgroup != nil {
			return errors.New("unexpected container metadata")
		}
		return nil
	}
	if actualActor.ContainerID != expectedContainer.Data.ID {
		return fmt.Errorf("container ID: got %q, want %q", actualActor.ContainerID, expectedContainer.Data.ID)
	}
	if actualActor.Hostname != expectedContainer.Data.Hostname {
		return fmt.Errorf("container hostname: got %q, want %q", actualActor.Hostname, expectedContainer.Data.Hostname)
	}
	if *actualActor.CSS != expectedContainer.Data.CSS["memory"] {
		return fmt.Errorf("memory CSS: got %q, want %q", *actualActor.CSS, expectedContainer.Data.CSS["memory"])
	}
	if actualActor.Cgroup == nil {
		return errors.New("missing cgroup snapshot")
	}
	if !strings.Contains(actualActor.Cgroup.Path, expectedContainer.Data.ID) {
		return fmt.Errorf("cgroup path %q does not contain container ID %q", actualActor.Cgroup.Path, expectedContainer.Data.ID)
	}
	const podMemoryLimit = 64 * 1024 * 1024
	if actualActor.Cgroup.Max != podMemoryLimit {
		return fmt.Errorf("cgroup memory limit: got %d, want %d", actualActor.Cgroup.Max, podMemoryLimit)
	}
	if actualActor.Cgroup.Current == 0 || len(actualActor.Cgroup.Stat) == 0 {
		return errors.New("missing cgroup memory usage or statistics")
	}
	return nil
}

func (actualActor *containerActor) identity() (actor, error) {
	if actualActor == nil {
		return actor{}, errors.New("missing actor object")
	}
	if actualActor.PID == nil || actualActor.Comm == nil || actualActor.CSS == nil {
		return actor{}, errors.New("missing actor pid, comm, or memory_cgroup_css_addr")
	}
	return actor{PID: *actualActor.PID, Comm: *actualActor.Comm, CSS: *actualActor.CSS}, nil
}
