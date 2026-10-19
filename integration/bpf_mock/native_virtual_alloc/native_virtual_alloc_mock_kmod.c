// SPDX-License-Identifier: (GPL-2.0 OR Apache-2.0)
// Copyright 2026 The HuaTuo Authors.

#include <linux/mm.h>
#include <linux/slab.h>

#include "native_virtual_alloc_hook.h"
#include "mock_device.h"
#include "native_virtual_alloc_mock.h"

static long run(const struct native_virtual_alloc_request *request)
{
	struct file *mock_file;
	if (request->scenario > NATIVE_VIRTUAL_ALLOC_FILE_ONLY)
		return -EINVAL;
	mock_file = kzalloc(sizeof(*mock_file), GFP_KERNEL);
	if (!mock_file)
		return -ENOMEM;
	CALL_do_mmap(mock_file, 0, 16384);
	if (request->scenario == NATIVE_VIRTUAL_ALLOC_MIXED) {
		CALL_do_mmap(NULL, 0, 4096);
		CALL_do_mmap(NULL, 0, 8192);
		CALL_do_mmap(NULL, 0, 0);
	}
	kfree(mock_file);
	return 0;
}

BPF_MOCK_RUN_DEVICE(native_virtual_alloc, struct native_virtual_alloc_request,
		    NATIVE_VIRTUAL_ALLOC_RUN, run);
MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Huatuo native_virtual_alloc integration fixture");
