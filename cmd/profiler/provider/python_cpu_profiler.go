// Copyright 2026 The HuaTuo Authors
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
	"path/filepath"
	"strconv"

	"huatuo-bamai/internal/profiler"
	"huatuo-bamai/internal/profiler/aggregator"
	pcontext "huatuo-bamai/internal/profiler/context"
	"huatuo-bamai/internal/profiler/procutil"
	"huatuo-bamai/internal/profiler/registry"
	targetsession "huatuo-bamai/internal/profiler/target"
	"huatuo-bamai/internal/utils/executil"
	"huatuo-bamai/pkg/profiling"
)

type pythonCPUTarget struct {
	pid int
}

type pythonCPUProfiler struct {
	duration      int
	freq          int
	toolPath      string
	maxConcurrent int
	targets       []pythonCPUTarget
}

func init() {
	impl := &pythonCPUProfiler{}
	registry.Register(registry.ProfilerMeta{
		Type:           profiling.TypeCPU,
		Implementation: profiling.ImplementationPython,
		Description:    "Python CPU profiler using py-spy",
		Impl:           impl,
		NewAggregator:  impl.NewAggregator,
	})
}

// ManagesDuration marks py-spy as self-terminating after record --duration.
func (*pythonCPUProfiler) ManagesDuration() {}

func (p *pythonCPUProfiler) NewAggregator(pctx *pcontext.ProfilerContext) (aggregator.Aggregator, error) {
	return newPythonCPUAggregator(pctx)
}

func (p *pythonCPUProfiler) Start(pctx *pcontext.ProfilerContext) error {
	if err := validatePythonToolPath(pctx.ToolPath); err != nil {
		return err
	}
	if err := validatePythonAggregationWindow(pctx.Duration, pctx.AggrInterval); err != nil {
		return err
	}

	p.duration = pctx.Duration
	p.freq = pctx.Freq
	p.toolPath = pctx.ToolPath
	p.maxConcurrent = pctx.MaxProfilerProcesses
	p.targets = nil

	pids, err := resolvePythonPids(pctx)
	if err != nil {
		return err
	}
	if err := validateResolvedPIDs("Python", pids); err != nil {
		return err
	}
	if len(pctx.PIDs) > 0 {
		if err := validateProcessExecutables("Python", "python", pids); err != nil {
			return err
		}
		if err := validateExpectedExecPath(pids, pctx.ExecPath); err != nil {
			return err
		}
	}
	pids, err = pythonRootPids(pids, procutil.ParentPID)
	if err != nil {
		return err
	}
	p.targets = make([]pythonCPUTarget, 0, len(pids))
	for _, pid := range pids {
		p.targets = append(p.targets, pythonCPUTarget{pid: pid})
	}

	return nil
}

func (p *pythonCPUProfiler) ReadDataLoop(ctx context.Context, enqueue func(any)) error {
	if len(p.targets) == 0 {
		return fmt.Errorf("read Python CPU profile: profiler is not started")
	}

	sessions := make([]targetsession.Session, 0, len(p.targets))
	for _, target := range p.targets {
		target := target
		sessions = append(sessions, targetsession.Session{
			PID: target.pid,
			Run: func(ctx context.Context) error {
				return runPySpySession(
					ctx,
					target.pid,
					p.duration,
					p.freq,
					p.toolPath,
					enqueue,
				)
			},
		})
	}

	return targetsession.RunSessions(ctx, sessions, p.maxConcurrent)
}

func (p *pythonCPUProfiler) Stop(_ *pcontext.ProfilerContext) error {
	return nil
}

func resolvePythonPids(pctx *pcontext.ProfilerContext) ([]int, error) {
	if len(pctx.PIDs) > 0 {
		return pctx.PIDs, nil
	}

	pids, err := procutil.GetPidsFromContainer(pctx.ExecPath, "python", pctx.ContainerID)
	if err != nil {
		return nil, err
	}

	if len(pids) == 0 {
		return nil, fmt.Errorf("sampling failed: no target Python processes found in container %q", pctx.ContainerID)
	}

	return pids, nil
}

func runPySpySession(
	ctx context.Context,
	pid int,
	duration int,
	frequency int,
	toolPath string,
	enqueue func(any),
) error {
	pyspyBin := filepath.Join(toolPath, "py-spy")
	result := executil.ExecCmd(
		ctx,
		pid,
		pyspyBin,
		buildPySpyArgs(pid, strconv.Itoa(duration), strconv.Itoa(frequency))...,
	)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !result.Success {
		return fmt.Errorf("sampling failed: %v, stderr: %q", result.CmdErr, string(result.Stderr))
	}
	if len(result.Stdout) > 0 {
		enqueue(profiler.SampleOutput{
			PID:    pid,
			Output: string(result.Stdout),
		})
	}
	return nil
}

func buildPySpyArgs(pid int, duration, frequency string) []string {
	return []string{
		"record",
		"-d", duration,
		"-f", "raw",
		"-r", frequency,
		"--subprocesses",
		"-o", "/dev/stdout",
		"-p", strconv.Itoa(pid),
	}
}
