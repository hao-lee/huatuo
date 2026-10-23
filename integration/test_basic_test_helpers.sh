#!/usr/bin/env bash

# Copyright 2026 The HuaTuo Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail
source "${ROOT_DIR}/integration/lib.sh"
require_commands python3 jq

check_process_shutdown() (
	local mode=$1 expected_status=$2 signal=${3:-TERM} pid="" status=0
	local ready="${HUATUO_BAMAI_TEST_TMPDIR}/${mode}.ready"
	trap 'stop_and_wait_by_pid "${pid}" 1 || true' EXIT
	python3 - "${ready}" "${mode}" << 'PY' &
import pathlib
import signal
import sys
import time

signal.signal(signal.SIGTERM, signal.SIG_IGN if sys.argv[2] == "forced" else lambda *_: sys.exit(7))
signal.signal(signal.SIGUSR2, lambda *_: sys.exit(0))
pathlib.Path(sys.argv[1]).touch()
while True:
    time.sleep(0.01)
PY
	pid=$!
	wait_until 5 0.05 test -f "${ready}" || fatal "${mode}: child did not become ready"
	stop_and_wait_by_pid "${pid}" 1 "${signal}" || status=$?
	assert_eq "${status}" "${expected_status}" "${mode}: child exit status"
	! kill -0 "${pid}" 2> /dev/null || fatal "${mode}: child survived shutdown"
	pid=""
)

stop_and_wait_by_pid ""
check_process_shutdown graceful_failure 7
check_process_shutdown forced 137
check_process_shutdown custom_signal 0 USR2

cat > "${HUATUO_BAMAI_TEST_TMPDIR}/profiler.py" << 'PY'
import json
import signal
import sys
import time

def finish(*_):
    print(json.dumps({"final": True, "profile": {"sample": []}}), flush=True)
    sys.exit(int(sys.argv[2]))

signal.signal(signal.SIGINT, finish)
for _ in range(int(sys.argv[1])):
    print(json.dumps({"profile": {"sample": [1]}}), flush=True)
    time.sleep(0.05)
while True:
    time.sleep(0.01)
PY

for helper_case in complete shutdown_failure missing_window; do
	helper_windows=2 helper_exit=0 helper_error=""
	case ${helper_case} in
	shutdown_failure) helper_exit=7 helper_error='exited 7' ;;
	missing_window) helper_windows=1 helper_error='two Java windows not published' ;;
	esac
	helper_status=0
	bash -euc '
source "$ROOT_DIR/integration/lib.sh"
# Bound the missing-window fixture without changing its completion predicate.
wait_until() {
 shift 2
 local i
 for ((i=0; i<20; i++)); do
  if "$@"; then return 0; fi
  sleep 0.05
 done
 return 1
}
assert_java_profiler_streams "$1" python3 "$2" "$3" "$4"
jq -se "last.final == true" "$1/windows.json"
' bash "${HUATUO_BAMAI_TEST_TMPDIR}/${helper_case}" \
		"${HUATUO_BAMAI_TEST_TMPDIR}/profiler.py" "${helper_windows}" "${helper_exit}" \
		> "${HUATUO_BAMAI_TEST_TMPDIR}/${helper_case}.log" 2>&1 || helper_status=$?
	if [[ -z ${helper_error} ]]; then
		[[ ${helper_status} == 0 ]] || fatal "${helper_case}: streaming failed; see ${helper_case}.log"
	else
		[[ ${helper_status} != 0 ]] || fatal "${helper_case}: invalid streaming session passed"
		grep -Fq "${helper_error}" "${HUATUO_BAMAI_TEST_TMPDIR}/${helper_case}.log" \
			|| fatal "${helper_case}: expected failure missing; see ${helper_case}.log"
	fi
done

python3 - "${ROOT_DIR}/integration" "${HUATUO_BAMAI_TEST_TMPDIR}" << 'PY'
import os
import pathlib
import subprocess
import sys

source_dir, workspace = map(pathlib.Path, sys.argv[1:])
ipv4_tcp = '{"drop_source":"software","netdev_name":"lo","layers":{"label":"IPv4/TCP","ipv4":{"version":4,"ihl":6,"len":44,"tos":16,"id":4885,"flags":"DF","ttl":64,"saddr":"127.0.0.2","daddr":"127.0.0.1","protocol":"TCP"},"tcp":{"sport":20001,"dport":20002,"flags":"SYN","seq":1,"data_offset":5}}}'
ipv4_udp = '{"drop_source":"software","netdev_name":"lo","layers":{"label":"IPv4/UDP","ipv4":{"version":4,"ihl":6,"len":32,"tos":16,"id":4885,"flags":"DF","ttl":64,"saddr":"127.0.0.2","daddr":"127.0.0.1","protocol":"UDP"},"udp":{"sport":20001,"dport":20002,"len":8}}}'
ipv4_max_tcp = ipv4_tcp.replace('"ihl":6', '"ihl":15').replace('"len":44', '"len":80')
ipv4_packets = "\n".join([ipv4_tcp, ipv4_udp, ipv4_max_tcp])
icmp_request = '{"layers":{"label":"IPv6/ICMPv6","icmp":{"type":"EchoRequest","id":4660,"seq":7},"ipv6":{"saddr":"2001:db8::2","daddr":"2001:db8::1"}}}'
icmp_zero = '{"layers":{"label":"IPv6/ICMPv6","icmp":{"type":"EchoRequest"},"ipv6":{"saddr":"2001:db8::2","daddr":"2001:db8::1"}}}'
latency_event = '{"tracer_data":{"tcp_saddr":"192.0.2.1","tcp_daddr":"192.0.2.2","latency_stage":"receive","latency_ms":2,"latency_threshold_ms":1,"packet_len_bytes":64,"net_namespace_inum":1,"net_namespace_cookie":1}}'

cases = [
    (
        "ipv4_options", "test_dropwatch_ipv4_options.sh",
        ["ipv4_options_packet_matches", "ipv4_options_capture_is_complete"],
        "IPV4_OPTIONS_SOURCE=127.0.0.2; IPV4_OPTIONS_DESTINATION=127.0.0.1; ipv4_options_source_port=20001; ipv4_options_destination_port=20002",
        "ipv4_options_capture_is_complete", ipv4_packets,
        [ipv4_tcp + "\n" + ipv4_udp, ipv4_tcp + "\n" + ipv4_max_tcp,
         ipv4_packets.replace('"dport":20002', '"dport":20003')],
    ),
    (
        "icmpv6_json", "test_dropwatch_icmpv6.sh", ["icmpv6_json_matches"], "",
        'icmpv6_json_matches "$TOOL_OUT" EchoRequest 2001:db8::2 2001:db8::1 4660 7',
        icmp_request,
        [icmp_request.replace('"id":4660', '"id":4661'), icmp_request.replace("EchoRequest", "EchoReply")],
    ),
    (
        "icmpv6_zero", "test_dropwatch_icmpv6.sh", ["icmpv6_json_matches"], "",
        'icmpv6_json_matches "$TOOL_OUT" EchoRequest 2001:db8::2 2001:db8::1 0 0',
        icmp_zero,
        [icmp_zero.replace('"type":"EchoRequest"', '"type":"EchoRequest","id":0')],
    ),
    (
        "icmpv6_text", "test_dropwatch_icmpv6.sh", ["icmpv6_text_matches"], "",
        'icmpv6_text_matches "$TOOL_OUT" EchoReply 2001:db8::1 2001:db8::2 4660 7',
        "timestamp IPv6/ICMPv6 2001:db8::1 > 2001:db8::2 type=EchoReply id=4660 seq=7 stack\n",
        ["timestamp IPv6/ICMPv6 2001:db8::2 > 2001:db8::1 type=EchoReply id=4660 seq=7 stack\n"],
    ),
    (
        "fast_retransmit", "test_tcpshark_retransmit_fast.sh", ["fast_retransmit_is_stored"],
        "TEST_PORT=19998", "fast_retransmit_is_stored",
        '{"tcp_sport":19998,"tcp_reason":"fast_retransmit"}',
        ['{"tcp_sport":19999,"tcp_reason":"fast_retransmit"}',
         '{"tcp_sport":19998,"tcp_reason":"unknown"}',
         '{"tcp_sport":19998,"tcp_reason":"RTO"}', '{"tcp_sport":19998,"ca_state":3}'],
    ),
    (
        "net_rx_latency", "test_events_net_rx_latency.sh", ["net_rx_latency_event_is_stored"],
        'NETNS_CLIENT_ADDR=192.0.2.1; NETNS_SERVER_ADDR=192.0.2.2; EVENTS_FILE=$TOOL_OUT',
        "net_rx_latency_event_is_stored", latency_event,
        [latency_event.replace('"latency_stage":"receive"', '"latency_stage":null'),
         latency_event.replace("192.0.2.1", "192.0.2.3")],
    ),
]
for name, filename, names, setup, check, valid, invalid in cases:
    source = (source_dir / filename).read_text()
    functions = []
    for function in names:
        start = source.index("\n" + function + "() {") + 1
        end = source.index("\n}\n", start) + 3
        functions.append(source[start:end])
    script = "set -euo pipefail\n" + "\n".join(functions) + "\n" + setup + "\n" + check
    fixtures = [valid, ""] + invalid
    if name != "icmpv6_text":
        fixtures.append(valid + "\n{")
    for index, fixture in enumerate(fixtures):
        path = workspace / "events.json"
        path.write_text(fixture)
        env = dict(os.environ, TOOL_OUT=str(path), OUTPUT_DIR=str(workspace))
        result = subprocess.run(["bash", "-euc", script], env=env, capture_output=True, text=True)
        if (result.returncode == 0) != (index == 0):
            raise SystemExit(f"{name} fixture {index}: expected match={index == 0}, exit={result.returncode}: {result.stderr}")
PY

log_info "process shutdown, Java streaming and network capture completion checks passed"
