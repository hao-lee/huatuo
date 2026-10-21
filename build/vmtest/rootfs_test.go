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

package vmtest

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml"
)

func TestGrowRootFilesystem(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/scripts/vm-test/run-in-vm.sh")
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.Index(source, []byte("\nconfigure_proxy\n"))
	if end < 0 {
		t.Fatal("guest script entry point missing")
	}
	for _, tc := range []struct {
		name, device, filesystem, parent, blockType, growOutput string
		growStatus, resizeStatus, wantExit                      int
		wantCalls                                               string
		alias                                                   bool
	}{
		{name: "Anolis XFS", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", growOutput: "CHANGED: partition=3", wantCalls: "growpart /dev/vda 3\nxfs_growfs /\ndf\n"},
		{name: "already expanded", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", growOutput: "NOCHANGE: partition 3", growStatus: 1, wantCalls: "growpart /dev/vda 3\nxfs_growfs /\ndf\n"},
		{name: "NVMe ext4", device: "/dev/nvme0n1p2", filesystem: "ext4", parent: "nvme0n1", blockType: "part", growOutput: "CHANGED: partition=2", wantCalls: "growpart /dev/nvme0n1 2\nresize2fs /dev/nvme0n1p2\ndf\n"},
		{name: "unsupported filesystem", device: "/dev/vda3[/root]", filesystem: "btrfs"},
		{name: "root device alias", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", alias: true, wantCalls: "growpart /dev/vda 3\nxfs_growfs /\ndf\n"},
		{name: "logical volume", device: "/dev/mapper/root", filesystem: "xfs", parent: "vda3", blockType: "lvm"},
		{name: "partition failure", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", growOutput: "FAILED: cannot grow", growStatus: 2, wantExit: 2, wantCalls: "growpart /dev/vda 3\n"},
		{name: "unexpected no-change error", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", growOutput: "error", growStatus: 1, wantExit: 1, wantCalls: "growpart /dev/vda 3\n"},
		{name: "filesystem failure", device: "/dev/vda3", filesystem: "xfs", parent: "vda", blockType: "part", growOutput: "CHANGED: partition=3", resizeStatus: 2, wantExit: 2, wantCalls: "growpart /dev/vda 3\nxfs_growfs /\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := filepath.Join(t.TempDir(), "calls")
			if err := os.WriteFile(calls, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			device := tc.device
			if tc.alias {
				device = filepath.Join(t.TempDir(), "root")
				if err := os.Symlink(tc.device, device); err != nil {
					t.Fatal(err)
				}
			}
			script := string(source[:end]) + `
findmnt() { if [[ $3 == SOURCE ]]; then echo "$TEST_DEVICE"; else echo "$TEST_FILESYSTEM"; fi; }
lsblk() { printf '%s %s\n' "$TEST_PARENT" "$TEST_BLOCK_TYPE"; }
growpart() { echo "growpart $*" >> "$TEST_CALLS"; echo "$TEST_GROW_OUTPUT"; return "$TEST_GROW_STATUS"; }
xfs_growfs() { echo "xfs_growfs $*" >> "$TEST_CALLS"; return "$TEST_RESIZE_STATUS"; }
resize2fs() { echo "resize2fs $*" >> "$TEST_CALLS"; return "$TEST_RESIZE_STATUS"; }
df() { echo df >> "$TEST_CALLS"; }
grow_root_filesystem
`
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "TEST_DEVICE="+device, "TEST_FILESYSTEM="+tc.filesystem, "TEST_PARENT="+tc.parent, "TEST_BLOCK_TYPE="+tc.blockType, "TEST_CALLS="+calls, "TEST_GROW_OUTPUT="+tc.growOutput, "TEST_GROW_STATUS="+strconv.Itoa(tc.growStatus), "TEST_RESIZE_STATUS="+strconv.Itoa(tc.resizeStatus))
			output, err := cmd.CombinedOutput()
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("growth failed: %v: %s", err, output)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != tc.wantExit {
					t.Fatalf("expected exit %d: %v: %s", tc.wantExit, err, output)
				}
			}
			actual, err := os.ReadFile(calls)
			if err != nil || string(actual) != tc.wantCalls {
				t.Fatalf("calls=%q, want %q: %v", actual, tc.wantCalls, err)
			}
		})
	}
}

func TestPortableGenerationGuard(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "stale revision", "missing source", "missing marker", "dirty source"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, contents string) {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(args ...string) string {
				t.Helper()
				cmd := exec.Command(args[0], args[1:]...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v: %v: %s", args, err, out)
				}
				return string(out)
			}
			write("Makefile", string(makefile))
			write("build/clang.sh", "")
			write(".gitignore", "_output/\nbin/\npkg/\ninternal/\n")
			for _, name := range []string{"bpf", "cmd"} {
				if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			files := strings.Fields(run("make", "APP_COMMIT=fixture", "--no-print-directory", "--eval=print-portable:;@echo $(PORTABLE_GENERATED_FILES)", "print-portable"))
			for _, file := range files {
				write(file, "")
			}
			run("git", "init", "-q")
			run("git", "add", ".")
			run("git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture")
			revision := strings.TrimSpace(run("git", "rev-parse", "HEAD"))
			if mode == "stale revision" {
				revision = "stale"
			}
			if mode != "missing marker" {
				write("_output/portable-generated.commit", revision+"\n")
			}
			write("_output/.bpf-build-stamp-0", "")
			write("bin/go", "#!/bin/sh\n[ \"$*\" = 'run ./build/bpfabi-tool' ] || exit 9\necho abi >> _output/calls\n")
			if err := os.Chmod(filepath.Join(dir, "bin/go"), 0o700); err != nil {
				t.Fatal(err)
			}
			if mode == "missing source" {
				if err := os.Remove(filepath.Join(dir, "internal/toolstream/transport/event.capnp.go")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "dirty source" {
				write("build/clang.sh", "# modified\n")
				write("_output/.bpf-build-stamp-0", "")
			}
			cmd := exec.Command("make", "gen-build", "SKIP_PORTABLE_GENERATION=1")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "valid") {
				t.Fatalf("guard: %v: %s", err, out)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "_output/calls"))
			if (string(calls) == "abi\n") != (mode == "valid") {
				t.Fatalf("unexpected ABI generation: %q", calls)
			}
		})
	}
}

func TestVMGoCache(t *testing.T) {
	script, err := filepath.Abs("../../.github/workflows/scripts/vm-test/go-cache.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, contents := range map[string]string{
		"vm.env": "VM_IP=192.0.2.1\nVM_SSH_KEY=/tmp/test-key\n",
		"ssh":    "#!/bin/sh\nif [ \"$TEST_MODE\" = key ]; then printf '%s\\n' \"$TEST_METADATA\"; fi\n",
		"rsync":  "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RUNNER_TEMP/rsync-args\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(mode, image, metadata string) string {
		t.Helper()
		cmd := exec.Command("bash", script, mode)
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "VM_ENV_FILE="+filepath.Join(dir, "vm.env"), "RUNNER_TEMP="+dir,
			"VM_IMAGE_REF="+image, "TEST_MODE="+mode, "TEST_METADATA="+metadata)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v: %s", mode, err, out)
		}
		return string(out)
	}
	key := run("key", "image-a", "go1.24 gcc14")
	if key != run("key", "image-a", "go1.24 gcc14") ||
		key == run("key", "image-b", "go1.24 gcc14") || key == run("key", "image-a", "go1.25 gcc14") {
		t.Fatal("cache key must be stable and change with the VM image or toolchain")
	}
	for _, mode := range []string{"restore", "save"} {
		run(mode, "", "")
		args, err := os.ReadFile(filepath.Join(dir, "rsync-args"))
		if err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(dir, "huatuo-go-build") + "/"
		remote := "root@192.0.2.1:/root/.cache/go-build/"
		want := local + "\n" + remote + "\n"
		if mode == "save" {
			want = remote + "\n" + local + "\n"
		}
		if !strings.HasSuffix(string(args), want) || strings.Contains(string(args), "--delete\n") != (mode == "save") {
			t.Fatalf("%s cache transport: %s", mode, args)
		}
	}
}

func TestGuestPreparationPhases(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/scripts/vm-test/run-in-vm.sh")
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.Index(source, []byte("\nconfigure_proxy\n"))
	if end < 0 {
		t.Fatal("guest script entry point missing")
	}
	for _, tc := range []struct {
		mode string
		want string
	}{
		{"--prepare-only", "proxy\ninfo\ndependencies\ngrow\n"},
		{"--test-only", "proxy\nmake install-tools\nmake test SKIP_PORTABLE_GENERATION=1\n"},
		{"", "proxy\ninfo\ndependencies\ngrow\nmake install-tools\nmake test SKIP_PORTABLE_GENERATION=1\n"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "_output"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "_output/portable-generated.commit"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			const stubs = `
configure_proxy() { echo proxy >> "$TEST_CALLS"; }
print_sys_info() { echo info >> "$TEST_CALLS"; }
install_test_dependencies() { echo dependencies >> "$TEST_CALLS"; }
grow_root_filesystem() { echo grow >> "$TEST_CALLS"; }
install_temporary_go_tools() { exit 99; }
make() { echo "make $*" >> "$TEST_CALLS"; }
git() { :; }
`
			script := string(source[:end]) + stubs + strings.ReplaceAll(string(source[end:]), "/mnt/host", dir)
			cmd := exec.Command("bash", "-c", script, "guest", tc.mode)
			calls := filepath.Join(dir, "calls")
			cmd.Env = append(os.Environ(), "TEST_CALLS="+calls)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("guest phase: %v: %s", err, out)
			}
			actual, err := os.ReadFile(calls)
			if err != nil || string(actual) != tc.want {
				t.Fatalf("calls: %q; want %q: %v", actual, tc.want, err)
			}
		})
	}
}

func TestIntegrationRunnerPool(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"refill", "resource", "failure", "skip", "serial", "repeated", "cleanup failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			workspace := filepath.Join(dir, "workspace")
			if err := os.Mkdir(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			// Mount isolation is covered by test_basic_runner.sh on the VM.
			// These stubs let the actual scheduler execute without CAP_SYS_ADMIN.
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"unshare", "mount"} {
				body := "#!/bin/sh\nexit 0\n"
				if name == "unshare" {
					body = "#!/bin/sh\n[ \"$1\" = --mount ] || exit 99\nshift\nexec \"$@\"\n"
				}
				path := filepath.Join(bin, name)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			names := []string{"test_metrics.sh", "test_node_config_api.sh", "test_metrics_include_filter.sh", "test_unknown.sh"}
			if mode == "resource" {
				names = []string{"test_profiler_native_physical_alloc_mock.sh", "test_profiler_native_physical_usage_mock.sh", "test_profiler_native_virtual_alloc_mock.sh", "test_unknown.sh"}
			}
			const script = `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
name=${0##*/}
wait_for() {
 for ((i=0;i<300;i++)); do [[ -f $1 ]] && return 0; sleep 0.01; done
 echo "timed out: $1" >&2; return 1
}
case $name in
 test_metrics.sh|test_profiler_native_physical_alloc_mock.sh)
  if [[ $TEST_MODE == failure ]]; then for ((n=0;n<180;n++)); do echo fixture-progress; done; fi
  echo diagnostic-from-first-case
  touch "$TEST_STATE/first-started"
  if [[ $TEST_MODE == refill || $TEST_MODE == failure ]]; then wait_for "$TEST_STATE/second-started"; fi
  if [[ $TEST_MODE == failure ]]; then exit 42; fi
  if [[ $TEST_MODE == refill ]]; then wait_for "$TEST_STATE/third-done"; fi
  if [[ $TEST_MODE == resource ]]; then wait_for "$TEST_STATE/third-started"; fi
  touch "$TEST_STATE/first-done"
  if [[ $TEST_MODE == skip ]]; then exit 77; fi
  ;;
 test_node_config_api.sh|test_profiler_native_physical_usage_mock.sh)
  if [[ $TEST_MODE == serial || $TEST_MODE == resource ]]; then [[ -f $TEST_STATE/first-done ]]; fi
  touch "$TEST_STATE/second-started"
  trap 'touch "$TEST_STATE/second-cleaned"' EXIT
  if [[ $TEST_MODE == failure ]]; then wait_for "$TEST_STATE/first-cleaned"; fi
  touch "$TEST_STATE/second-done"
  ;;
 test_metrics_include_filter.sh|test_profiler_native_virtual_alloc_mock.sh)
  [[ $TEST_MODE != failure ]] || touch "$TEST_STATE/unexpected-start"
  touch "$TEST_STATE/third-started"
  if [[ $TEST_MODE == refill ]]; then [[ -f $TEST_STATE/second-done && ! -f $TEST_STATE/first-done ]]; fi
  touch "$TEST_STATE/third-done"
  ;;
 test_unknown.sh)
  [[ -f $TEST_STATE/first-done && -f $TEST_STATE/second-done && -f $TEST_STATE/third-done ]]
  touch "$TEST_STATE/exclusive-done"
  ;;
esac
`
			args := []string{"-c", `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
source "$ROOT_DIR/integration/lib_runner.sh"
if [[ $TEST_MODE == failure ]]; then
 huatuo_bamai_stop() {
  if [[ $1 == */test_metrics.* ]]; then touch "$TEST_STATE/first-cleaned"; fi
 }
elif [[ $TEST_MODE == 'cleanup failure' ]]; then huatuo_bamai_stop() { return 1; }; fi
run_test_suite "$TEST_REPEATS" integration "$TEST_JOBS" "$@"
`, "pool"}
			for _, name := range names {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, path)
			}
			jobs, repeats := "2", "1"
			if mode == "repeated" {
				repeats = "2"
			}
			if mode == "serial" {
				jobs = "1"
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "HUATUO_BAMAI_TEST_TMPDIR="+workspace, "TEST_STATE="+dir,
				"TEST_MODE="+mode, "TEST_JOBS="+jobs, "TEST_REPEATS="+repeats, "PATH="+bin+":"+os.Getenv("PATH"), "BPF_MOCK_ES_DIR=", "STORAGE_SHARED_DIR=")
			out, err := cmd.CombinedOutput()
			if mode == "failure" || mode == "cleanup failure" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("want failure: %v: %s", err, out)
				}
				if mode == "failure" && !strings.Contains(string(out), "total=2 passed=1 skipped=0 failed=1") {
					t.Fatalf("in-flight result was not drained: %s", out)
				}
				if _, err := os.Stat(filepath.Join(dir, "unexpected-start")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("runner started a pending case after failure")
				}
				if mode == "failure" && !strings.Contains(string(out), "diagnostic-from-first-case") {
					t.Fatalf("failed case log was not printed: %s", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("pool: %v: %s", err, out)
			}
			want := "total=4 passed=4 skipped=0 failed=0"
			if mode == "repeated" {
				want = "total=8 passed=8 skipped=0 failed=0"
			}
			if mode == "skip" {
				want = "total=4 passed=3 skipped=1 failed=0"
			}
			if !strings.Contains(string(out), want) {
				t.Fatalf("missing summary %s: %s", want, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "exclusive-done")); err != nil {
				t.Fatal("exclusive test did not run after the pool")
			}
			if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successful workspace was not removed: %v", err)
			}
		})
	}
}

func TestIntegrationWorkerPorts(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
ss() { echo 'LISTEN 0 128 127.0.0.1:20000 0.0.0.0:*'; }
export INTEGRATION_PORT_BASE=20000 INTEGRATION_PORT_DIR="$TEST_STATE/ports"
for ((i=0;i<12;i++)); do (allocate_available_port > "$TEST_STATE/port-$i") & done
wait
INTEGRATION_PORT_BASE=21000 INTEGRATION_PORT_DIR="$TEST_STATE/other" allocate_available_port > "$TEST_STATE/other-port"
# A failed socket query must fail allocation instead of treating every port as free.
ss() { return 1; }
if allocate_available_port; then exit 99; fi
`)
	cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "TEST_STATE="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("allocate worker ports: %v: %s", err, out)
	}
	seen := map[int]bool{}
	for i := range 12 {
		data, err := os.ReadFile(filepath.Join(dir, "port-"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || port <= 20000 || port >= 21000 || seen[port] {
			t.Fatalf("invalid or duplicate worker port %q: %v", data, err)
		}
		seen[port] = true
	}
	other, err := os.ReadFile(filepath.Join(dir, "other-port"))
	if err != nil || strings.TrimSpace(string(other)) != "21000" {
		t.Fatalf("second worker port = %q: %v", other, err)
	}
}

func TestIntegrationWorkerConfigs(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config, address string
		port                  int64
	}{
		{"implicit parent", "[HTTPServer.Auth]\nBearerToken = \"secret\"\n", "127.0.0.1:21001", 21001},
		{"explicit parent", "[HTTPServer]\n[HTTPServer.Auth]\nBearerToken = \"secret\"\n", "127.0.0.1:21001", 21001},
		{"custom address", "[HTTPServer]\nListenAddress = \"127.0.0.1:23456\"\n[HTTPServer.Auth]\nBearerToken = \"secret\"\n", "127.0.0.1:23456", 23456},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bamai.conf"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", `
set -euo pipefail
source "$ROOT_DIR/integration/lib.sh"
source "$ROOT_DIR/integration/config.sh"
source "$ROOT_DIR/integration/lib_storage.sh"
configure_worker_bamai_address
APISERVER_PORT=21100 API_TOKEN=admin OTHER_API_TOKEN=reader ES_PASSWORD=test STORAGE_ADDR=http://127.0.0.1:9200
for writer in write_apiserver_apis_config write_apiserver_without_profile_storage_config write_continuous_profiling_apiserver_config; do
 "$writer"
 cp "$HUATUO_BAMAI_TEST_TMPDIR/apiserver.conf" "$HUATUO_BAMAI_TEST_TMPDIR/$writer.conf"
done
`)
			cmd.Env = append(os.Environ(), "ROOT_DIR="+root, "HUATUO_BAMAI_TEST_TMPDIR="+dir, "HUATUO_BAMAI_ADDR=http://"+tc.address)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("configure worker: %v: %s", err, out)
			}
			cfg, err := toml.LoadFile(filepath.Join(dir, "bamai.conf"))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Get("HTTPServer.ListenAddress") != tc.address || cfg.Get("HTTPServer.Auth.BearerToken") != "secret" {
				t.Fatalf("incorrect daemon config: %s", cfg)
			}
			for _, writer := range []string{"write_apiserver_apis_config", "write_apiserver_without_profile_storage_config", "write_continuous_profiling_apiserver_config"} {
				api, err := toml.LoadFile(filepath.Join(dir, writer+".conf"))
				if err != nil {
					t.Fatal(err)
				}
				if api.Get("Agent.HTTPPort") != tc.port || api.Get("Agent.Auth.BearerToken") != "integration-node-token" {
					t.Fatalf("API did not target its worker's agent: %s", api)
				}
			}
		})
	}
}
