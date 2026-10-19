// SPDX-License-Identifier: (GPL-2.0 OR Apache-2.0)
// Copyright 2026 The HuaTuo Authors.

#include <linux/mm.h>
#include <linux/rmap.h>

#include "native_physical_alloc_hook.h"
#include "mock_device.h"
#include "native_physical_alloc_mock.h"

static void mock_alloc(struct page *page)
{
#ifdef HAVE_folio_add_new_anon_rmap
	CALL_folio_add_new_anon_rmap((struct folio *)page);
#else
	CALL_page_add_new_anon_rmap(page);
#endif
}

/* Older production hooks count one base page per call. */
static void mock_alloc_large(struct page *page)
{
#ifdef HAVE_folio_add_new_anon_rmap
	mock_alloc(page);
#else
	int i;
	for (i = 0; i < 4; i++)
		mock_alloc(page + i);
#endif
}

static long run(const struct native_physical_alloc_request *request)
{
	struct page *small, *large;
	if (request->scenario > NATIVE_PHYSICAL_ALLOC_MIXED)
		return -EINVAL;
	small = alloc_pages(GFP_KERNEL | __GFP_ZERO, 0);
	large = alloc_pages(GFP_KERNEL | __GFP_ZERO | __GFP_COMP, 2);
	if (!small || !large) {
		if (small)
			__free_pages(small, 0);
		if (large)
			__free_pages(large, 2);
		return -ENOMEM;
	}
	mock_alloc(small);
	mock_alloc_large(large);
	/* Only the mock hooks change profiler accounting; real rmap stays
	 * untouched. */
	__free_pages(small, 0);
	__free_pages(large, 2);
	return 0;
}

BPF_MOCK_RUN_DEVICE(native_physical_alloc, struct native_physical_alloc_request,
		    NATIVE_PHYSICAL_ALLOC_RUN, run);
MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Huatuo native_physical_alloc integration fixture");
