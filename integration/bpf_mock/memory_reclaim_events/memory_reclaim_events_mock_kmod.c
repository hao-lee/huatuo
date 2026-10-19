// SPDX-License-Identifier: (GPL-2.0 OR Apache-2.0)
// Copyright 2026 The HuaTuo Authors.

#include <linux/delay.h>
#include <linux/gfp.h>
#include <linux/nodemask.h>

#include "mock_device.h"
#include "memory_reclaim_events_mock.h"
#include "memory_reclaim_events_hook.h"

unsigned long try_to_free_pages_mock RECLAIM_MOCK_HOOK_ARGS;

unsigned long noinline __noclone try_to_free_pages_mock RECLAIM_MOCK_HOOK_ARGS
{
	/* Exercise entry/return timing without reclaiming any pages. */
	msleep(RECLAIM_MOCK_DELAY_MS);
	return 0;
}

static long run(const struct reclaim_mock_request *request)
{

	if (request->scenario != RECLAIM_MOCK_CURRENT_TASK)
		return -EINVAL;

	try_to_free_pages_mock(node_zonelist(numa_node_id(), GFP_KERNEL), 0,
			       GFP_KERNEL, NULL);
	return 0;
}

BPF_MOCK_RUN_DEVICE(memory_reclaim_events, struct reclaim_mock_request,
		    HUATUO_RECLAIM_MOCK_RUN, run);

MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Huatuo direct reclaim kprobe and kretprobe fixture");
