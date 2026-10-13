// SPDX-License-Identifier: (GPL-2.0 OR Apache-2.0)
// Copyright 2026 The HuaTuo Authors.

#ifndef HUATUO_MOCK_DEVICE_H
#define HUATUO_MOCK_DEVICE_H

#include <linux/fs.h>
#include <linux/miscdevice.h>
#include <linux/module.h>
#include <linux/uaccess.h>

#define BPF_MOCK_DEVICE(feature, handler)                 \
	static const struct file_operations mock_fops = { \
	    .owner = THIS_MODULE,                         \
	    .unlocked_ioctl = handler,                    \
	};                                                \
	static struct miscdevice mock_device = {          \
	    .minor = MISC_DYNAMIC_MINOR,                  \
	    .name = #feature "_mock",                     \
	    .fops = &mock_fops,                           \
	    .mode = 0600,                                 \
	}

/* Preserve named request fields while sharing the userspace boundary. */
#define BPF_MOCK_RUN_DEVICE(feature, request_type, command, run)    \
	static long mock_ioctl(struct file *file, unsigned int cmd, \
			       unsigned long arg)                   \
	{                                                           \
		request_type request;                               \
		if (cmd != command)                                 \
			return -ENOTTY;                             \
		if (copy_from_user(&request, (void __user *)arg,    \
				   sizeof(request)))                \
			return -EFAULT;                             \
		return run(&request);                               \
	}                                                           \
	BPF_MOCK_DEVICE(feature, mock_ioctl);                       \
	module_misc_device(mock_device)

#endif
