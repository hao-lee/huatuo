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

//go:build integration

package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	bpfmock "github.com/ccfos/huatuo/integration/bpf_mock/common"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func metricValue(text, name string) (float64, error) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.SplitN(fields[0], "{", 2)[0] != "huatuo_bamai_memory_free_"+name {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, fmt.Errorf("invalid metric %s", name)
		}
		return value, nil
	}
	return 0, fmt.Errorf("missing metric %s", name)
}

func verify(before, after string, reclaimNS, compactNS uint64) error {
	for _, item := range []struct {
		name    string
		elapsed uint64
	}{
		{"allocpages_stall", reclaimNS}, {"compaction_stall", compactNS},
	} {
		initial, err := metricValue(before, item.name)
		if err != nil {
			return err
		}
		actual, err := metricValue(after, item.name)
		if err != nil {
			return err
		}
		if initial != 0 || item.elapsed < 40000000 || actual < 40 || actual > float64(item.elapsed)/1e6 {
			return fmt.Errorf("%s: baseline %g, actual %g ms outside [40, %g]", item.name, initial, actual, float64(item.elapsed)/1e6)
		}
	}
	return nil
}

func verifyUnmatched(before, after string) error {
	for _, name := range []string{"allocpages_stall", "compaction_stall"} {
		initial, err := metricValue(before, name)
		if err != nil {
			return err
		}
		actual, err := metricValue(after, name)
		if err != nil {
			return err
		}
		if actual != initial {
			return fmt.Errorf("unmatched reclaim end changed %s: %g -> %g", name, initial, actual)
		}
	}
	return nil
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: %s SCENARIO_DIR", os.Args[0])
	}
	dir := os.Args[1]
	before, err := os.ReadFile(filepath.Join(dir, "before.metrics"))
	if err != nil {
		return err
	}
	after, err := os.ReadFile(filepath.Join(dir, "after.metrics"))
	if err != nil {
		return err
	}
	unmatchedBefore, err := os.ReadFile(filepath.Join(dir, "unmatched-before.metrics"))
	if err != nil {
		return err
	}
	paired, err := os.ReadFile(filepath.Join(dir, "paired.metrics"))
	if err != nil {
		return err
	}
	if err := verifyUnmatched(string(before), string(unmatchedBefore)); err != nil {
		return err
	}
	if err := verifyUnmatched(string(paired), string(after)); err != nil {
		return err
	}
	elapsed := map[string]uint64{}
	for _, name := range []string{"reclaim", "compaction"} {
		for i := 1; i <= 2; i++ {
			expected, err := bpfmock.ReadJSON[struct {
				ElapsedNS uint64 `json:"elapsed_ns"`
			}](filepath.Join(dir, fmt.Sprintf("%s-%d.json", name, i)))
			if err != nil {
				return err
			}
			elapsed[name] += expected.ElapsedNS
		}
	}
	if err := verify(string(before), string(after), elapsed["reclaim"], elapsed["compaction"]); err != nil {
		return err
	}
	fmt.Println("PASS memory_free_compact: published reclaim and compaction latency bounds")
	return nil
}
