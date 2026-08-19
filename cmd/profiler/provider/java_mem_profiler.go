// Copyright 2025, 2026 The HuaTuo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package provider

import (
	"context"
	"fmt"
	"time"

	"huatuo-bamai/internal/profiler/aggregator"
	pcontext "huatuo-bamai/internal/profiler/context"
	"huatuo-bamai/internal/profiler/registry"
	javaruntime "huatuo-bamai/internal/profiler/runtime/java"
	"huatuo-bamai/pkg/profiling"
)

const (
	javaAllocInterval    = "512k"
	javaMemoryStackDepth = "256"
)

func init() {
	impl := &javaMemoryProfiler{}
	registry.Register(registry.ProfilerMeta{
		Type:           profiling.TypeMemory,
		Implementation: profiling.ImplementationJava,
		Description:    "Java memory profiler using async-profiler",
		Impl:           impl,
		NewAggregator:  impl.NewAggregator,
	})
}

type javaMemoryProfiler struct {
	targetPlan *javaTargetPlan
}

func (p *javaMemoryProfiler) NewAggregator(pctx *pcontext.ProfilerContext) (aggregator.Aggregator, error) {
	return newJavaAggregator(pctx)
}

// ManagesDuration marks each Java target session as owning its sampling
// window, allowing bounded batches to give every process the full duration.
func (*javaMemoryProfiler) ManagesDuration() {}

func (p *javaMemoryProfiler) Start(pctx *pcontext.ProfilerContext) error {
	p.targetPlan = nil

	if err := validateJavaToolPath(pctx.ToolPath); err != nil {
		return err
	}

	pids := pctx.PIDs
	if len(pids) == 0 {
		var err error
		pids, err = javaruntime.ResolveJavaPids(
			pctx.ExecPath,
			pctx.ContainerID,
		)
		if err != nil {
			return err
		}
	}
	if err := validateResolvedPIDs("Java", pids); err != nil {
		return err
	}
	if len(pctx.PIDs) > 0 {
		if err := validateProcessExecutables("Java", "java", pids); err != nil {
			return err
		}
		if err := validateExpectedExecPath(pids, pctx.ExecPath); err != nil {
			return err
		}
	}

	extraArgs, err := validateJavaMemoryMode(pctx.MemoryMode)
	if err != nil {
		return err
	}

	baseArgs := []string{
		"--libpath", "/tmp/libasyncProfiler.so",
		"-e", "alloc",
		"--alloc", javaAllocInterval,
		"-j", javaMemoryStackDepth,
	}
	baseArgs = append(baseArgs, extraArgs...)

	targetPlan, err := prepareJavaTargetPlan(
		pids,
		pctx.MaxProfilerProcesses,
		pctx.ToolPath,
		baseArgs,
		"mem",
		javaAggregationInterval(pctx),
		time.Duration(pctx.Duration)*time.Second,
	)
	if err != nil {
		return err
	}
	p.targetPlan = targetPlan
	return nil
}

func (p *javaMemoryProfiler) Stop(_ *pcontext.ProfilerContext) error {
	return p.targetPlan.cleanup()
}

func (p *javaMemoryProfiler) ReadDataLoop(ctx context.Context, enqueue func(any)) error {
	if p.targetPlan == nil {
		return fmt.Errorf("read Java memory profile: profiler is not started")
	}
	return p.targetPlan.run(ctx, enqueue)
}
