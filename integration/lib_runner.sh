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

# Test execution inside the runner's UTS and mount namespaces.

# Reviewed cases declare only resources that cannot be isolated by allocation.
# Unreviewed cases and whole-machine measurements hold the exclusive machine lock.
# One shared ES may remain alive; allow at most one private storage case beside it.
declare -A runner_resource_limits=(["profiler"]=2 ["private-storage"]=1)
declare -A runner_test_resources=(
	["test_apiserver_continuous_profiling_api.sh"]="profiler"
	["test_apiserver_continuous_profiling_concurrent_jobs.sh"]="profiler"
	["test_apiserver_continuous_profiling_control.sh"]="profiler"
	["test_apiserver_job_recovery.sh"]="profiler"
	["test_apiserver_profile_disabled.sh"]=""
	["test_apiserver_profile_filters.sh"]=""
	["test_apiserver_profiling.sh"]="private-storage profiler"
	["test_apiserver_readiness_metrics.sh"]=""
	["test_autotracing_cpu_profiler.sh"]="profiler"
	["test_autotracing_cpusys.sh"]=""
	["test_autotracing_iotracing.sh"]=""
	["test_autotracing_irqtracing.sh"]=""
	["test_autotracing_memburst.sh"]="exclusive"
	["test_autotracing_memory_threshold_snapshot_config.sh"]=""
	["test_basic_bpf_debug_macro.sh"]=""
	["test_basic_cgroup_css.sh"]="build"
	["test_basic_cgroup_helpers.sh"]=""
	["test_basic_cgroup_subsys_id.sh"]="build"
	["test_basic_dockerfile_check.sh"]=""
	["test_basic_runner.sh"]="exclusive"
	["test_basic_startup_log.sh"]=""
	["test_basic_storage_elasticsearch_config.sh"]=""
	["test_basic_storage_elasticsearch_values.sh"]=""
	["test_basic_storage_localfile_rotation.sh"]=""
	["test_basic_storage_opensearch_elasticsearch.sh"]="private-storage"
	["test_basic_test_helpers.sh"]="exclusive"
	["test_dropwatch_device_filter.sh"]=""
	["test_dropwatch_devlink.sh"]=""
	["test_dropwatch_icmpv6.sh"]=""
	["test_dropwatch_ipv4_options.sh"]=""
	["test_dropwatch_netdevsim.sh"]="netdevsim"
	["test_dropwatch_ratelimit.sh"]="exclusive"
	["test_dropwatch_reason.sh"]=""
	["test_events_io_health.sh"]="io-devices"
	["test_events_memory_oom_kill.sh"]=""
	["test_events_net_rx_latency.sh"]=""
	["test_events_netdev_bonding_lacp.sh"]="bonding"
	["test_events_sched_tick.sh"]=""
	["test_events_sched_tick_irqoff.sh"]="cpu-pinned"
	["test_events_tcp_retransmit.sh"]=""
	["test_io_kernel.sh"]="exclusive"
	["test_iocost.sh"]="exclusive"
	["test_iotracing_ext4.sh"]="io-devices build"
	# XFS devices appear in global sysfs, including sibling daemon collectors.
	["test_iotracing_xfs.sh"]="exclusive"
	["test_irqtracing_ratelimit.sh"]="cpu-pinned"
	["test_metrics.sh"]=""
	["test_metrics_exclude_filter.sh"]=""
	["test_metrics_include_filter.sh"]=""
	["test_metrics_iotracing.sh"]=""
	["test_node_cloud_events_api.sh"]=""
	["test_node_config_api.sh"]=""
	["test_node_runtime_limits.sh"]="exclusive"
	["test_profiler_benchmark_self_cpu.sh"]="exclusive"
	["test_profiler_config.sh"]=""
	["test_profiler_flamegraph.sh"]="profiler"
	["test_profiler_java_cpu_container.sh"]="profiler java-agent"
	["test_profiler_java_cpu_multi_pid.sh"]="profiler java-agent"
	["test_profiler_java_memory_usage_alloc.sh"]="profiler java-agent"
	["test_profiler_native_cpu.sh"]="profiler"
	["test_profiler_native_cpu_container.sh"]="profiler"
	["test_profiler_native_cpu_cpp_symbols.sh"]="profiler"
	["test_profiler_native_cpu_cpuid.sh"]="profiler cpu-pinned"
	["test_profiler_native_cpu_hardware_pmu.sh"]="exclusive"
	["test_profiler_native_cpu_offcpu.sh"]="profiler cpu-pinned"
	["test_profiler_native_cpu_rust_symbols.sh"]="profiler"
	["test_profiler_native_cpu_thread_group.sh"]="profiler"
	["test_profiler_native_memory_physical_usage.sh"]="profiler"
	["test_profiler_native_memory_thread_group.sh"]="profiler"
	["test_profiler_native_memory_virtual_alloc.sh"]="profiler"
	["test_profiler_output_flags.sh"]=""
	["test_profiler_python_cpu_multi_pid.sh"]="profiler"
	["test_profiler_user_kernel_frames.sh"]="profiler"
	["test_sched_blame.sh"]="exclusive"
	["test_tcp_retransmit_ratelimit.sh"]=""
	["test_tcp_retransmit_syn.sh"]=""
	["test_tcp_retransmit_synack.sh"]=""
	["test_tcp_retransmit_tlp.sh"]=""
	["test_tcpshark_output_cancellation.sh"]=""
	["test_tcpshark_retransmit_dropwatch_correlation.sh"]=""
	["test_tcpshark_retransmit_fast.sh"]=""
)

runner_start_case() {
	local -a command=(bash "$1")
	local script=$1 slot=$2 resources=$3 workspace name address=${HUATUO_BAMAI_ADDR:-http://127.0.0.1:19704}
	name=${script##*/}
	if ((runner_jobs > 1)) && [[ ${resources} == exclusive ]]; then
		runner_stop_storage 0
		((runner_failed == 0)) || fatal "shared storage cleanup failed before exclusive case"
	fi
	if ((runner_jobs > 1)) && [[ ${resources} != exclusive ]]; then
		command=(unshare --mount bash "${ROOT_DIR}/integration/exec_case.sh" "${script}")
	fi
	if [[ ${runner_suite} == integration ]]; then
		local port
		port=$(allocate_available_port) || fatal "cannot allocate case daemon port"
		address="http://127.0.0.1:${port}"
	fi
	workspace=$(mktemp -d "${HUATUO_BAMAI_TEST_TMPDIR}/${name%.sh}.XXXXXX")
	runner_pids[slot]=""
	runner_names[slot]="${name} (${runner_round}/${runner_repeats})"
	runner_workspaces[slot]=${workspace}
	runner_started[slot]=${SECONDS}
	log_info "🚀🚀 start: ${runner_names[slot]}"
	if [[ ${runner_suite} == e2e ]]; then
		HUATUO_BAMAI_TEST_TMPDIR="${workspace}" huatuo_bamai_start "${HUATUO_BAMAI_ARGS_E2E[@]}"
	fi
	# Job control gives each case its own process group and preserves SIGINT.
	# Closed stdin keeps background tools from stopping on terminal reads.
	set -m
	(
		# Only the scheduler owns leases. Siblings must not inherit each other's
		# locks through the shell, daemon, or workload processes they launch.
		local lease fd
		for lease in "${runner_resource_fds[@]}"; do
			for fd in ${lease}; do exec {fd}>&-; done
		done
		export HUATUO_BAMAI_TEST_TMPDIR="${workspace}"
		export HUATUO_BAMAI_ADDR="${address}" HUATUO_BAMAI_METRICS_API="${address}/metrics"
		exec "${command[@]}"
	) < /dev/null > "${workspace}/test.log" 2>&1 &
	runner_pids[slot]=$!
	set +m
}

runner_finish_case() {
	local slot=$1 status=0 workspace=${runner_workspaces[$1]}
	if [[ -n ${runner_pids[slot]} ]]; then wait "${runner_pids[slot]}" || status=$?; else status=1; fi
	find "${workspace}" -type f -name timings.log -exec cat {} + || true
	if [[ ${runner_suite} == e2e ]]; then
		HUATUO_BAMAI_TEST_TMPDIR="${workspace}" e2e_test_teardown "${status}" || status=1
		if [[ ${status} -ne 0 && ${status} -ne 77 ]]; then
			log_error "e2e artifacts: ${workspace}"
			dump_text_files "${workspace}" || true
		fi
	else
		integration_test_exit "${status}" "${workspace}" || status=1
	fi
	test_result_record "${runner_names[slot]} duration=$((SECONDS - runner_started[slot]))s" "${status}"
	if [[ ${status} -ne 0 && ${status} -ne 77 ]]; then runner_failed=1; fi
	runner_release_resources "${slot}"
	unset 'runner_pids[slot]' 'runner_workspaces[slot]'
}

runner_stop_storage() {
	local status=$1
	if [[ -n ${runner_last_storage} ]]; then stop_shared_storage "${status}" || runner_failed=1; fi
}

runner_cleanup() {
	local status=$? slot pid
	local -a stopping=()
	trap - EXIT
	trap '' INT TERM
	if [[ ${status} == 130 || ${status} == 143 ]]; then
		# Cancel all cases together; each gets five seconds for its EXIT cleanup.
		for pid in "${runner_pids[@]}"; do
			[[ -n ${pid} ]] || continue
			(
				trap - EXIT
				stop_by_process_group "${pid}" 5
			) &
			stopping+=("$!")
		done
		for pid in "${stopping[@]}"; do wait "${pid}" || true; done
	fi
	# Ordinary failures still let other in-flight cases finish normally.
	for slot in "${!runner_pids[@]}"; do runner_finish_case "${slot}"; done
	for slot in "${!runner_resource_fds[@]}"; do runner_release_resources "${slot}"; done
	if ((runner_failed)) && [[ ${status} != 130 && ${status} != 143 ]]; then status=1; fi
	runner_stop_storage "${status}"
	if ((runner_failed)) && [[ ${status} != 130 && ${status} != 143 ]]; then status=1; fi
	if [[ ${runner_suite} == integration && ${status} -eq 0 ]]; then
		rm -rf -- "${INTEGRATION_RESOURCE_DIR}"
		rmdir -- "${HUATUO_BAMAI_TEST_TMPDIR}" || status=1
	fi
	test_results_summary
	[[ ${status} -ne 77 ]] || status=0
	exit "${status}"
}

runner_parallel_cases() {
	local jobs=$1
	shift
	local -a pending=("$@")
	local slot index resources
	while ((${#pending[@]} || ${#runner_pids[@]})); do
		# Reap every completed case before scheduling more work after a failure.
		for slot in "${!runner_pids[@]}"; do
			if ! kill -0 "${runner_pids[slot]}" 2> /dev/null; then runner_finish_case "${slot}"; fi
		done
		((runner_failed == 0)) || return 1
		for ((slot = 0; slot < jobs; slot++)); do
			[[ -z ${runner_pids[slot]:-} ]] || continue
			for index in "${!pending[@]}"; do
				resources=${runner_test_resources[${pending[index]##*/}]-exclusive}
				runner_try_resources "${slot}" "${resources}" || continue
				runner_start_case "${pending[index]}" "${slot}" "${resources}"
				unset 'pending[index]'
				break
			done
		done
		if ((${#runner_pids[@]})); then sleep 0.1; fi
	done
}

run_test_suite() {
	runner_repeats=$1 runner_suite=$2 runner_jobs=$3
	shift 3
	local -a scripts=("$@")
	local script
	runner_last_storage="" runner_failed=0
	declare -ga runner_pids=() runner_workspaces=() runner_names=() runner_started=() runner_resource_fds=()
	export INTEGRATION_RESOURCE_DIR="${HUATUO_BAMAI_TEST_TMPDIR}/resources"
	mkdir -p "${INTEGRATION_RESOURCE_DIR}"
	if [[ ${runner_suite} == e2e ]]; then
		source "${ROOT_DIR}/e2e/lib.sh"
		if ! kernel_version_le 6 5; then
			log_info "disabling sched-blame on unsupported kernel $(uname -r)"
			HUATUO_BAMAI_ARGS_E2E+=("--disable-tracing" "sched-blame")
		fi
	fi
	for script in "${scripts[@]}"; do
		if [[ ${runner_suite} == integration ]]; then
			case ${script##*/} in
			test_apiserver_continuous_profiling_*.sh | test_apiserver_job_recovery.sh | test_apiserver_profile_filters.sh | test_autotracing_irqtracing.sh | test_autotracing_cpu_profiler.sh | test_basic_storage_elasticsearch_values.sh) runner_last_storage=${script} ;;
			esac
		fi
	done
	if [[ -n ${runner_last_storage} ]]; then
		export STORAGE_SHARED_DIR="${HUATUO_BAMAI_TEST_TMPDIR}/default-es"
		source "${ROOT_DIR}/integration/lib_storage.sh"
	fi
	test_results_init
	trap runner_cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	for ((runner_round = 1; runner_round <= runner_repeats; runner_round++)); do
		if ((runner_jobs > 1)); then
			runner_parallel_cases "${runner_jobs}" "${scripts[@]}"
		else
			for script in "${scripts[@]}"; do
				runner_try_resources 0 exclusive || fatal "cannot acquire serial case resources"
				runner_start_case "${script}" 0 "${runner_test_resources[${script##*/}]-exclusive}"
				runner_finish_case 0
				((runner_failed == 0)) || exit 1
				if [[ ${script} == "${runner_last_storage}" ]]; then runner_stop_storage 0; fi
			done
		fi
		# Every consumer, including a profiler, has now completed cleanup.
		runner_stop_storage 0
		((runner_failed == 0)) || exit 1
	done
	exit 0
}
