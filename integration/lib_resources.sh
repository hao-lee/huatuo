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

# Resources shared by cases in one runner invocation.
# Allocation state lives outside per-case mount/PID namespaces.

allocate_test_port() (
	local protocol=$1 port=20000 sockets first_ephemeral last_ephemeral
	flock 9
	sockets=$(ss -H -an "--${protocol}") || return 1
	read -r first_ephemeral last_ephemeral < /proc/sys/net/ipv4/ip_local_port_range
	if [[ -f ${INTEGRATION_RESOURCE_DIR}/next-port ]]; then
		read -r port < "${INTEGRATION_RESOURCE_DIR}/next-port"
	fi
	while ((port <= 60999)); do
		# Keep listeners outside the kernel's ephemeral range so outgoing
		# connections cannot claim a port before its service binds.
		if ((port >= first_ephemeral && port <= last_ephemeral)); then
			port=$((last_ephemeral + 1))
			continue
		fi
		printf '%s\n' "$((port + 1))" > "${INTEGRATION_RESOURCE_DIR}/next-port"
		if ! awk '{ print $4 }' <<< "${sockets}" | grep -Eq "[:.]${port}$"; then
			printf '%s\n' "${port}"
			return 0
		fi
		port=$((port + 1))
	done
	log_error 'integration port pool is exhausted outside the ephemeral range'
	return 1
)

# All leases are acquired before a case starts. Failed attempts release every
# partial acquisition, so blocked cases consume no worker and cannot deadlock.
runner_try_resources() {
	local slot=$1 resources=$2 mode=--shared resource token fd found
	local -a acquired=()
	[[ ${resources} != exclusive ]] || mode=--exclusive
	exec {fd}> "${INTEGRATION_RESOURCE_DIR}/machine.lock"
	if ! flock -n "${mode}" "${fd}"; then
		exec {fd}>&-
		return 1
	fi
	acquired+=("${fd}")
	if [[ ${resources} != exclusive ]]; then
		for resource in ${resources}; do
			found=false
			for ((token = 0; token < ${runner_resource_limits[${resource}]:-1}; token++)); do
				exec {fd}> "${INTEGRATION_RESOURCE_DIR}/${resource}-${token}.lock"
				if flock -n "${fd}"; then
					acquired+=("${fd}")
					found=true
					break
				fi
				exec {fd}>&-
			done
			if [[ ${found} == false ]]; then
				for fd in "${acquired[@]}"; do exec {fd}>&-; done
				return 1
			fi
		done
	fi
	runner_resource_fds[slot]="${acquired[*]}"
}

runner_release_resources() {
	local fd
	for fd in ${runner_resource_fds[$1]:-}; do exec {fd}>&-; done
	unset 'runner_resource_fds[$1]'
}
