// SPDX-License-Identifier: (GPL-2.0 OR Apache-2.0)
// Copyright 2026 The HuaTuo Authors.

#include <linux/mm.h>
#include <linux/compaction.h>
#include <linux/delay.h>

#include "mock_device.h"
#include "memory_free_compact_mock.h"

#include "memory_free_compact_hook.h"
#define CREATE_TRACE_POINTS
#include "memory_free_compact_trace.h"

static long run(const struct memory_free_compact_request *request)
{
	switch (request->scenario) {
	case FREE_COMPACT_RECLAIM:
		CALL_RECLAIM_BEGIN();
		msleep(FREE_COMPACT_DELAY_MS);
		CALL_RECLAIM_END();
		break;
	case FREE_COMPACT_COMPACTION:
		CALL_try_to_compact_pages();
		break;
	case FREE_COMPACT_UNMATCHED_END:
		CALL_RECLAIM_END();
		break;
	default:
		return -EINVAL;
	}
	return 0;
}

BPF_MOCK_RUN_DEVICE(memory_free_compact, struct memory_free_compact_request,
		    MEMORY_FREE_COMPACT_RUN, run);
MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Huatuo memory_free_compact integration fixture");
