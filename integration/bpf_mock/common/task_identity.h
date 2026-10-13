// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The HuaTuo Authors.

#ifndef HUATUO_MOCK_TASK_IDENTITY_H
#define HUATUO_MOCK_TASK_IDENTITY_H

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <unistd.h>

/* A mounted procfs reports IDs in its PID namespace, including for workers. */
static inline int read_mock_task_ids(const char *host_proc, pid_t *pid, pid_t *tid)
{
	char path[PATH_MAX], line[256];
	FILE *status;
	int length;

	if (!host_proc) {
		*pid = getpid();
		*tid = syscall(SYS_gettid);
		return 0;
	}
	length = snprintf(path, sizeof(path), "%s/thread-self/status", host_proc);
	if (length < 0 || (size_t)length >= sizeof(path)) {
		errno = ENAMETOOLONG;
		return -1;
	}
	status = fopen(path, "r");
	if (!status)
		return -1;
	*pid = *tid = 0;
	while (fgets(line, sizeof(line), status)) {
		if (sscanf(line, "Tgid:%d", pid) == 1)
			continue;
		sscanf(line, "Pid:%d", tid);
	}
	if (fclose(status))
		return -1;
	if (*pid <= 0 || *tid <= 0) {
		errno = EINVAL;
		return -1;
	}
	return 0;
}

#endif
