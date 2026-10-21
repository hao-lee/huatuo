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
