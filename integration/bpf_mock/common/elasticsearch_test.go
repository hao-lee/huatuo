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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadMockElasticsearchEvents(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required for the Elasticsearch helper")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		expected int
		total    int
		returned int
		wantExit int
	}{
		{name: "exact count", expected: 2, total: 2, returned: 2},
		{name: "one document", expected: 1, total: 1, returned: 1},
		{name: "three documents", expected: 3, total: 3, returned: 3},
		{name: "truncated search", expected: 2, total: 2, returned: 1, wantExit: 1},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			hits := strings.TrimSuffix(strings.Repeat(`{"_source":{"scenario":"mock"}},`, tc.returned), ",")
			response := fmt.Sprintf(`{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"relation":"eq","value":%d},"hits":[%s]}}`, tc.total, hits)
			cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/bpf_mock/common/elasticsearch.sh"
STORAGE_ADDR=https://127.0.0.1:9200
STORAGE_INDEX=mock-events
storage_curl() {
 if [[ ${*: -1} == */_refresh ]]; then
  printf '%s' '{"_shards":{"failed":0}}'
 else
  printf '%s' "$SEARCH_RESPONSE"
 fi
}
read_bpf_mock_es_events "$HUATUO_BAMAI_TEST_TMPDIR/events.json"
`, "test", fmt.Sprint(tc.expected))
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root,
				"HUATUO_BAMAI_TEST_TMPDIR="+workspace, "SEARCH_RESPONSE="+response)
			output, err := cmd.CombinedOutput()
			if tc.wantExit != 0 {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != tc.wantExit {
					t.Fatalf("want exit %d: %v: %s", tc.wantExit, err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("read ES documents: %v: %s", err, output)
			}
			actualDocuments, err := os.ReadFile(filepath.Join(workspace, "events.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(actualDocuments) != strings.Repeat("{\"scenario\":\"mock\"}\n", tc.expected) {
				t.Fatalf("unexpected ES documents: %s", actualDocuments)
			}
		})
	}
}

func TestDefaultElasticsearchLifecycle(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		wantExit, starts int
	}{
		{"shared", 0, 1},
		{"private", 0, 2},
		{"failure", 42, 1},
		{"skip", 77, 1},
		{"compatibility", 0, 4},
		{"stop_failure", 1, 1},
		{"state_failure", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
source "$ROOT_DIR/integration/lib_storage.sh"
if [[ $TEST_MODE != private ]]; then export STORAGE_SHARED_DIR="$HUATUO_BAMAI_TEST_TMPDIR/shared"; fi
docker() {
 case $1 in
 run) printf 'start\n' >> "$TEST_LOG"; printf 'container-%s\n' "${HUATUO_BAMAI_TEST_TMPDIR##*/}";;
 port) printf '127.0.0.1:19200\n';;
 rm) printf 'stop\n' >> "$TEST_LOG"; [[ $TEST_MODE != stop_failure ]];;
 logs) printf 'shared storage diagnostics\n';;
 image|pull) return 0;;
 *) return 1;;
 esac
}
printf() {
 if [[ $TEST_MODE == state_failure && $1 == STORAGE_CONTAINER_ID=* ]]; then
  builtin printf "STORAGE_CONTAINER_ID='"
  return 1
 fi
 builtin printf "$@"
}
storage_ready() { return 0; }
timeout() { shift; "$@"; }
finish() {
 local status=$?
 [[ -z ${STORAGE_SHARED_DIR:-} ]] || stop_shared_storage "$status" || exit 1
}
trap finish EXIT
run_case() (
 export HUATUO_BAMAI_TEST_TMPDIR="$HUATUO_BAMAI_TEST_TMPDIR/$1"
 mkdir -p "$HUATUO_BAMAI_TEST_TMPDIR"
 trap storage_stop EXIT
 storage_start "${2:-default}"
 if [[ ${2:-default} == default ]]; then
  [[ $STORAGE_ADDR == http://* && -z $STORAGE_USERNAME && -z $STORAGE_PASSWORD ]]
 else
  [[ $STORAGE_SHARED == false && -n $STORAGE_USERNAME && -n $STORAGE_PASSWORD ]]
 fi
 storage_test_index profiles >> "$HUATUO_BAMAI_TEST_TMPDIR/index"
 [[ $TEST_MODE != failure ]] || exit 42
 [[ $TEST_MODE != skip ]] || exit 77
)
run_case case-one.Abc
run_case case-two.Xyz
if [[ $TEST_MODE == compatibility ]]; then
 for profile in elasticsearch_v7 elasticsearch_v8 opensearch_v2; do run_case "$profile" "$profile"; done
fi
`)
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "HUATUO_BAMAI_TEST_TMPDIR="+workspace,
				"STORAGE_SHARED_DIR=", "TEST_LOG="+filepath.Join(workspace, "lifecycle.log"), "TEST_MODE="+tc.name)
			output, err := cmd.CombinedOutput()
			status := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				status = exit.ExitCode()
			}
			if status != tc.wantExit {
				t.Fatalf("storage lifecycle: exit=%d, want %d: %s", status, tc.wantExit, output)
			}
			log, err := os.ReadFile(filepath.Join(workspace, "lifecycle.log"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(log), "start\n") != tc.starts || strings.Count(string(log), "stop\n") != tc.starts {
				t.Fatalf("unexpected lifecycle: %s", log)
			}
			if tc.name == "shared" || tc.name == "private" {
				for _, name := range []string{"case-one.Abc", "case-two.Xyz"} {
					index, err := os.ReadFile(filepath.Join(workspace, name, "index"))
					if err != nil || string(index) != "profiles_"+strings.ToLower(name) {
						t.Fatalf("index isolation: %q: %v", index, err)
					}
				}
			}
			_, stateErr := os.Stat(filepath.Join(workspace, "shared", "es.env"))
			if tc.name == "failure" || tc.name == "stop_failure" {
				if stateErr != nil {
					t.Fatalf("failure artifacts missing: %v", stateErr)
				}
			} else if !errors.Is(stateErr, os.ErrNotExist) {
				t.Fatalf("unexpected shared state: %v", stateErr)
			}
		})
	}
}

func TestMockElasticsearchLifecycle(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required for the Elasticsearch helper")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		wantExit  int
		wantStart int
		wantIndex int
	}{
		{name: "shared", wantStart: 1, wantIndex: 2},
		{name: "private", wantStart: 2, wantIndex: 2},
		{name: "shared_failure", wantExit: 1, wantStart: 1, wantIndex: 2},
		{name: "shared_index_failure", wantExit: 1, wantStart: 1},
		{name: "shared_state_failure", wantExit: 1, wantStart: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
if [[ $TEST_MODE != private ]]; then
 export BPF_MOCK_ES_DIR="$HUATUO_BAMAI_TEST_TMPDIR/shared-es"
fi
stub_storage() {
 printf() {
  if [[ $TEST_MODE == shared_state_failure && $1 == STORAGE_CONTAINER_ID=* ]]; then
   builtin printf "STORAGE_CONTAINER_ID='"
   return 1
  fi
  builtin printf "$@"
 }
 require_commands() { :; }
 docker() { [[ $1 != logs ]] || printf 'shared ES diagnostics\n'; }
 storage_start() {
  [[ $1 == elasticsearch_v8 ]]
  printf 'start\n' >> "$TEST_LOG"
  STORAGE_CONTAINER_ID=mock-container
  STORAGE_ADDR=https://127.0.0.1:9200
  STORAGE_USERNAME=elastic
  STORAGE_PASSWORD=123456
 }
 storage_stop() {
  [[ -n $STORAGE_CONTAINER_ID ]] || return 0
  printf 'stop\n' >> "$TEST_LOG"
  STORAGE_CONTAINER_ID=""
 }
 storage_curl() {
  [[ $TEST_MODE != shared_index_failure ]] || return 1
  printf 'index %s\n' "$STORAGE_INDEX" >> "$TEST_LOG"
  printf '{"acknowledged":true}'
 }
}
finish_shared() {
 local status=$?
 [[ -n $BPF_MOCK_ES_DIR ]] || return 0
 source "$ROOT_DIR/integration/bpf_mock/common/elasticsearch.sh"
 stub_storage
 stop_shared_bpf_mock_es "$status"
}
trap finish_shared EXIT
run_case() (
 export HUATUO_BAMAI_TEST_TMPDIR="$HUATUO_BAMAI_TEST_TMPDIR/$1"
 mkdir -p "$HUATUO_BAMAI_TEST_TMPDIR"
 source "$ROOT_DIR/integration/config.sh"
 source "$ROOT_DIR/integration/bpf_mock/common/elasticsearch.sh"
 stub_storage
 trap stop_bpf_mock_es EXIT
 start_bpf_mock_es
 write_bpf_mock_es_config memory_oom_kill
)
run_case case-one.Abc
run_case case-two.Xyz
[[ $TEST_MODE != shared_failure ]] || exit 1
`)
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root,
				"HUATUO_BAMAI_TEST_TMPDIR="+workspace, "BPF_MOCK_ES_DIR=",
				"TEST_LOG="+filepath.Join(workspace, "lifecycle.log"), "TEST_MODE="+tc.name)
			output, err := cmd.CombinedOutput()
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("helper lifecycle: %v: %s", err, output)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != tc.wantExit {
					t.Fatalf("want exit %d: %v: %s", tc.wantExit, err, output)
				}
			}
			log, err := os.ReadFile(filepath.Join(workspace, "lifecycle.log"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(log), "start\n") != tc.wantStart ||
				strings.Count(string(log), "stop\n") != tc.wantStart ||
				strings.Count(string(log), "index ") != tc.wantIndex {
				t.Fatalf("unexpected lifecycle: %s", log)
			}
			if tc.name == "shared_state_failure" {
				if _, err := os.Stat(filepath.Join(workspace, "shared-es", "es.env")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed state write transferred container ownership: %v", err)
				}
			} else if tc.wantExit != 0 {
				diagnostics, err := os.ReadFile(filepath.Join(workspace, "shared-es", "elasticsearch.log"))
				if err != nil || !strings.Contains(string(diagnostics), "shared ES diagnostics") {
					t.Fatalf("missing failure diagnostics: %v: %s", err, diagnostics)
				}
			} else if _, err := os.Stat(filepath.Join(workspace, "shared-es")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("shared workspace remains after success: %v", err)
			}
			if tc.wantIndex == 2 {
				for _, name := range []string{"case-one.Abc", "case-two.Xyz"} {
					cfg, err := os.ReadFile(filepath.Join(workspace, name, "bamai.conf"))
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(cfg), `Index = "huatuo_bpf_mock_`+strings.ToLower(name)+`"`) ||
						!strings.Contains(string(cfg), `Username = "elastic"`) ||
						!strings.Contains(string(cfg), `Password = "123456"`) {
						t.Fatalf("incorrect isolated ES config: %s", cfg)
					}
				}
			}
		})
	}
}

func TestMockElasticsearchCleanupPreservesStatus(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	const services = "daemon\n"
	const unload = "unload memory_oom_kill_mock\n"
	for _, tc := range []struct {
		name     string
		status   int
		failStep string
		unloaded bool
		wantExit int
		wantLog  string
	}{
		{name: "success", wantLog: services + unload + "es\n"},
		{name: "test failure", status: 42, wantExit: 42, wantLog: services + unload + "logs\nes\n"},
		{name: "skip", status: 77, wantExit: 77, wantLog: services + unload + "es\n"},
		{name: "daemon failure", failStep: "daemon", wantExit: 1, wantLog: services + unload + "logs\nes\n"},
		{name: "ES failure", failStep: "es", wantExit: 1, wantLog: services + unload + "es\n"},
		{name: "unload failure", failStep: "unload", wantExit: 1, wantLog: services + unload + "logs\nes\n"},
		{name: "diagnostics failure", status: 42, failStep: "logs", wantExit: 42, wantLog: services + unload + "logs\nes\n"},
		{name: "module not owned", unloaded: true, wantLog: services + "es\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			module := "memory_oom_kill_mock"
			if tc.unloaded {
				module = ""
			}
			cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/bpf_mock/common/lib.sh"
huatuo_bamai_stop() { echo daemon >> "$TEST_LOG"; [[ $FAIL_STEP != daemon ]]; }
storage_dump_logs() { echo logs >> "$TEST_LOG"; [[ $FAIL_STEP != logs ]]; }
stop_bpf_mock_es() { echo es >> "$TEST_LOG"; [[ $FAIL_STEP != es ]]; }
rmmod() { echo "unload $1" >> "$TEST_LOG"; [[ $FAIL_STEP != unload ]]; }
trap 'cleanup_bpf_mock_es "$?" "$LOADED_MODULE" || exit $?' EXIT
exit "$TEST_STATUS"
`)
			logPath := filepath.Join(workspace, "cleanup.log")
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "TEST_LOG="+logPath,
				"TEST_STATUS="+strconv.Itoa(tc.status), "FAIL_STEP="+tc.failStep, "LOADED_MODULE="+module)
			output, err := cmd.CombinedOutput()
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("cleanup: %v: %s", err, output)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != tc.wantExit {
					t.Fatalf("want exit %d: %v: %s", tc.wantExit, err, output)
				}
			}
			log, err := os.ReadFile(logPath)
			if err != nil || string(log) != tc.wantLog {
				t.Fatalf("cleanup order: %v: got %q, want %q", err, log, tc.wantLog)
			}
		})
	}
}

func TestMockElasticsearchWaitsForChecker(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	checker := filepath.Join(workspace, "checker")
	if err := os.WriteFile(checker, []byte(`#!/usr/bin/env bash
[[ $1 == --container && $2 == expected.json && $4 == scenario ]] || exit 2
jq -se 'length == 1 and .[0].value == 42' "$3"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(checker, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/bpf_mock/common/lib.sh"
STORAGE_ADDR=https://127.0.0.1:9200
STORAGE_INDEX=events
storage_curl() {
 if [[ ${*: -1} == */_refresh ]]; then
  printf '{"_shards":{"failed":0}}'
 elif [[ -f $HUATUO_BAMAI_TEST_TMPDIR/second ]]; then
  printf '{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"relation":"eq","value":1},"hits":[{"_source":{"value":42}}]}}'
 else
  touch "$HUATUO_BAMAI_TEST_TMPDIR/second"
  printf '{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"relation":"eq","value":0},"hits":[]}}'
 fi
}
wait_until() { shift 2; "$@" && return 9; "$@"; }
verify_bpf_mock_es_events feature "$HUATUO_BAMAI_TEST_TMPDIR/events.json" --container expected.json "$HUATUO_BAMAI_TEST_TMPDIR/events.json" scenario
`)
	cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "HUATUO_BAMAI_TEST_TMPDIR="+workspace, "BPF_MOCK_CHECKER_BIN="+checker)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ES checker polling: %v: %s", err, out)
	}
}
