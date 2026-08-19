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
	"errors"
	"fmt"
	"time"

	javaruntime "huatuo-bamai/internal/profiler/runtime/java"
	targetsession "huatuo-bamai/internal/profiler/target"
)

type javaTarget struct {
	pid int
}

type javaTargetPlan struct {
	targets       []javaTarget
	maxConcurrent int
	toolPath      string
	baseArgs      []string
	outFilePrefix string
	aggrInterval  time.Duration
	duration      time.Duration
}

func prepareJavaTargetPlan(
	pids []int,
	maxConcurrent int,
	toolPath string,
	baseArgs []string,
	outFilePrefix string,
	aggrInterval time.Duration,
	duration time.Duration,
) (*javaTargetPlan, error) {
	if maxConcurrent < 0 {
		return nil, fmt.Errorf("prepare Java targets: maximum concurrency must not be negative")
	}

	targets := make([]javaTarget, 0, len(pids))
	prepared := make([]int, 0, len(pids))
	for _, pid := range pids {
		if err := javaruntime.PrepareJavaAgent(pid, toolPath); err != nil {
			return nil, errors.Join(
				fmt.Errorf("prepare Java agent for PID %d: %w", pid, err),
				cleanupJavaAgents(prepared),
			)
		}
		prepared = append(prepared, pid)
		targets = append(targets, javaTarget{pid: pid})
	}

	return &javaTargetPlan{
		targets:       targets,
		maxConcurrent: maxConcurrent,
		toolPath:      toolPath,
		baseArgs:      append([]string(nil), baseArgs...),
		outFilePrefix: outFilePrefix,
		aggrInterval:  aggrInterval,
		duration:      duration,
	}, nil
}

func (p *javaTargetPlan) run(ctx context.Context, enqueue func(any)) error {
	sessions := make([]targetsession.Session, 0, len(p.targets))
	for _, target := range p.targets {
		target := target
		sessions = append(sessions, targetsession.Session{
			PID: target.pid,
			Run: func(ctx context.Context) error {
				return p.runTarget(ctx, target, enqueue)
			},
		})
	}
	return targetsession.RunSessions(ctx, sessions, p.maxConcurrent)
}

func (p *javaTargetPlan) runTarget(
	ctx context.Context,
	target javaTarget,
	enqueue func(any),
) error {
	opt := &javaruntime.AsprofSamplingOption{
		Pids:          []int{target.pid},
		ToolPath:      p.toolPath,
		BaseArgs:      append([]string(nil), p.baseArgs...),
		OutFilePrefix: p.outFilePrefix,
		AggrInterval:  p.aggrInterval,
		Duration:      p.duration,
	}
	profileOutFile, err := javaruntime.StartAsprofSampling(ctx, opt)
	if err != nil {
		return err
	}

	sessionCtx, cancel := context.WithTimeout(ctx, p.duration)
	defer cancel()
	return javaruntime.ReadAsprofDataLoop(sessionCtx, opt, profileOutFile, enqueue)
}

func (p *javaTargetPlan) cleanup() error {
	if p == nil {
		return nil
	}
	pids := make([]int, 0, len(p.targets))
	for _, target := range p.targets {
		pids = append(pids, target.pid)
	}
	return cleanupJavaAgents(pids)
}

func cleanupJavaAgents(pids []int) error {
	cleanupErrs := make([]error, 0)
	for _, pid := range pids {
		if err := javaruntime.CleanupJavaAgent(pid); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("cleanup Java agent for PID %d: %w", pid, err))
		}
	}
	return errors.Join(cleanupErrs...)
}
