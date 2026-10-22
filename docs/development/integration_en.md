---
title: Integration Test
type: docs
description:
author: HUATUO Team
date: 2026-03-04
weight: 5
---

The integration suite includes fixture tests for **huatuo-bamai** metrics and
[kernel mock tests for BPF](ebpf-integration-mock_en.md). The kernel
mock guide covers event collectors, compaction metrics, and native memory
profiles using production userspace handling.

The metrics fixture tests run the real `huatuo-bamai` binary with mocked
`/proc` and `/sys` filesystems and verify the `/metrics` endpoint output.

### What the Script Does
The integration test performs the following steps:

1. Generates a temporary `bamai.conf`
2. Starts `huatuo-bamai` with mocked `procfs` and `sysfs`
3. Waits for the Prometheus `/metrics` endpoint to become available
4. Fetches all metrics from `/metrics`
5. Verifies that all expected metrics exist
6. Stops the service and cleans up resources

If any expected metric is missing, the test fails.

### How to Run
Run the integration test from the project root:

```bash
bash integration/run.sh
```

Pass a file name to run one integration test. The optional second argument is
the repeat count and defaults to 1:

```bash
bash integration/run.sh test_metrics_exclude_filter.sh 10
```

or
```bash
make integration
```
### Parallel Integration Tests

Local runs default to one worker. Use two workers with:

```bash
bash integration/run.sh -j 2
# Or rebuild and run:
INTEGRATION_JOBS=2 make integration
```

VM CI uses three workers. Reviewed tests run in a bounded pool; cases in the same
resource group serialize. Physical allocation/usage mocks share a group because
their hook symbols overlap. Network tests share a group for traffic and device
isolation. Storage compatibility uses the network group and tests its three
backends sequentially, overlapping other groups without starting three backend
containers together. Reviewed standalone profiler tests run in a separate pool
of at most two workers after shared Elasticsearch shuts down. Java cases share
an agent library path and serialize within that pool. Benchmarks, PMU,
CPU-pinned, and unreviewed tests run alone afterward. Profiling durations and
assertions are unchanged. E2E remains serial, including when `INTEGRATION_JOBS`
is set.

Each worker reserves ports from its own range. Each parallel case gets a private
mount namespace for Huatuo's PID files and toolstream socket, while retaining
access to the host container runtime. Run one suite runner per host.
Shared Elasticsearch starts under a lock and remains alive until the batch
finishes. On failure, the runner stops scheduling, waits for active cases to
finish cleanup, and preserves failed-case logs. Results include case durations;
full case output is saved in `test.log` and printed on failure.
Phase measurements in `timings.log` are printed even for successful cases.
Storage timings separate image pulls, container startup, readiness, and shared
startup lock waits. Compatibility cases also time verification and cleanup.

New tests default to serial execution. Review shared kernel state, traffic,
paths, ports, and CPU assumptions before adding a group in `lib_runner.sh`.

### Prerequisites and Results

Both suites use `integration/run.sh`, which defaults to integration.
`e2e/run.sh` forwards to `--suite e2e`. Existing `make integration`, `make e2e`,
and single-case entry points remain available:

```bash
bash integration/run.sh --suite e2e
bash integration/run.sh --suite e2e test_metrics.sh 2
bash e2e/run.sh test_metrics.sh 2
```

Both suites run in separate UTS and mount namespaces with hostname `huatuo-dev`
and private mount propagation. Integration cases start their own services;
e2e starts the baseline bamai before each case, then stops it and checks its log.
Idle e2e fixture pods use a one-second termination grace period. Cleanup still
waits for deletion, and discovery tests require Huatuo to remove their metadata.
Their timings distinguish pod deletion from kubelet and Huatuo discovery waits.
Integration removes workspaces after passed or skipped cases and preserves them
on failure. E2E retains its workspaces.

The runner checks only that `_output` exists, without checking individual
project binaries or BPF objects. Run
`make build` before invoking a runner directly. An existing directory does not
prove that the build is complete; missing artifacts fail when used.

Use the same helper for commands on PATH and executable file paths. Missing
commands skip the case:

```bash
require_commands jq curl ss
require_commands "${PROFILER_TOOL_DIR}/bin/asprof"
```

Use `require_readable` for input files such as certificates. Unreadable inputs
skip the case and report the affected path:

```bash
require_readable "${KUBELET_CERT}" "${KUBELET_KEY}"
```

`skip` prints its reason and exits with 77; `EXIT` cleanup still runs. The runner
classifies 0 as PASS, 77 as SKIP, and other statuses as FAIL. Cleanup failures also
count as FAIL. A suite containing only passed and skipped cases returns 0.
A failure stops subsequent cases and prints the results collected so far.

Each execution counts separately, including repetitions. For example:

```text
summary: total=5 passed=3 skipped=2 failed=0
```

When root privileges or namespace commands are unavailable, both suites count
all selected executions as SKIP.

#### On Failure

- The `huatuo-bamai` service metrics and logs are printed to stdout
- The temporary working directory is kept for debugging

#### On Success

- Output the list of successfully validated metrics

Go fixture builds reuse the inherited `GOCACHE` or Go's default build cache.
Build temporary files and test outputs stay in the case workspace. Live Go test
invocations retain `-count=1` to execute again while reusing compiled dependencies.

The continuous-profiling API, profile-filter, IRQ autotracing, and storage-values
tests share one default Elasticsearch instance per suite repetition. Indices
include each case's workspace name. The runner stops storage after the last
selected case in this group, or on failure; standalone scripts own their own
instance. Authenticated BPF mock storage and the ES8 compatibility case share
one authenticated ES8 instance with separate indices. ES7 and OpenSearch keep
their own instances. The first owner checks image pullability; the runner owns
shared cleanup, and standalone scripts retain private instances.
VM CI preloads the ES7 and OpenSearch images during compilation and unit tests.
The compatibility test still checks pullability. Prefetch logs are saved under
`_output/storage-image-prefetch`; failed prefetch does not replace normal checks.

---

### Background Process Cleanup

`stop_and_wait_by_pid PID [TIMEOUT]` stops and waits for a child process,
returning its exit status. An omitted or empty PID returns success immediately,
so callers can pass an initialized PID variable without checking it first.
Clear the variable after waiting; use `|| true` in best-effort cleanup and
assert the exit status when it is part of the test contract.

### Network Namespace Helpers

Network tests can source `integration/lib_namespace.sh` after `lib.sh`:

- `namespace_setup NAME` creates one network namespace and exposes its name as
  `NETNS`. The caller brings up `lo` and configures any addresses it needs.
- `namespace_setup_with_pair PREFIX SERVER_ADDR CLIENT_ADDR [PREFIX_LENGTH]`
  creates two namespaces connected by a veth pair, configures their addresses,
  and brings up the interfaces. The prefix length defaults to 24. Names and
  addresses are exposed through `NETNS_SERVER`, `NETNS_CLIENT`,
  `NETNS_VETH_SERVER`, `NETNS_VETH_CLIENT`, `NETNS_SERVER_ADDR`, and
  `NETNS_CLIENT_ADDR`.
- `namespace_cleanup` deletes the namespaces created by either helper and
  clears their state. Call it from the test's `EXIT` cleanup after stopping
  processes in those namespaces. Repeated cleanup calls are supported.

### Shared Packet Sender

Tests that construct raw packets should reuse
`integration/testdata/test_packet_sender.py` and extend it for new workloads.
It requires Python 3 and raw-socket privileges, and supports `ipv4-options`
(TCP/UDP EOL options, including maximum IHL) and `icmpv6-echo`.
Run a subcommand with `--help` for its address, port, or Echo identity arguments.
The sender repeats traffic every 100 ms until SIGTERM and exits successfully.
Shell tests own namespaces, drop rules, assertions, logs, and process cleanup.
Workloads requiring real TCP connections continue to use `testdata/tcp_server.py`.

### How to Add New Metrics Tests
#### 1: Add or Update Fixture Data

If the metric depends on /proc or /sys, add or update mock data under:
```bash
integration/fixtures/
```

Only kernel filesystem mirrors (`proc/`, `sys/`, and `dev/`) belong here;
preserve their original paths and filenames. Store tool fixtures under
`integration/testdata/tools/` with a `test_` or `Test` filename prefix.
For example, copy `test_perf.sh` to the temporary tool directory as `perf`
before starting the test.
#### 2: Add Expected Metrics

Create a new file under:
```bash
integration/testdata/expected_metrics/
├── test_cpu.txt
├── test_memory.txt
└── ...
```

Each non-empty, non-comment line represents one expected Prometheus metric line
and must match the /metrics output exactly.

The test automatically loads `test_*.txt` files and strips `test_` from the
filename when deriving the metric prefix.

#### 3: Run the Test
```bash
bash integration/run.sh
```
The test fails if any expected metric is missing or mismatched.

### VM Disk Capacity

VM CI requests a 40 GB disk. Guest setup expands partition-backed XFS/ext4 root
filesystems to use the available space before running tests. It installs
`cloud-guest-utils` on Debian/Ubuntu or `cloud-utils-growpart` on RPM systems.
Partition or filesystem growth failures stop setup; pod evictions remain failures.


### VM Build Reuse

CI generates the architecture-independent OpenAPI, mock, and Cap’n Proto sources
once per workflow and transfers them to each VM. The artifact must match the
checkout’s commit and contain every required source; tracked files must be clean.
BPF objects and their Go ABI types are still generated inside each VM.
Local builds continue to run all generators.

After guest setup expands the root filesystem, each VM restores a Go build
cache scoped to its image and toolchain. Cache misses
or transfer failures fall back to compilation. Unit and mock framework tests use
`-count=1` so cached test results never replace execution.

Command binaries build in one Go invocation, with two build workers by default
(`GO_BUILD_PARALLELISM=2`). Compiler and linker flags remain unchanged. The
integration runner also reuses successfully built mock modules and checkers
between cases, including host and container variants. This cache belongs to one
runner invocation and is removed after success; modules never cross VM kernels.

Measure both a cold run and a warm run when evaluating CI time: the first run
populates the persistent cache, and cache download/upload time also counts.
