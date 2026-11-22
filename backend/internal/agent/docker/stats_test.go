package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fakeStatsAPI struct {
	fakeContainerAPI

	records  []containerapi.StatsResponse
	statsErr error
	opts     client.ContainerStatsOptions
	calls    []string
}

func (f *fakeStatsAPI) ContainerStats(_ context.Context, id string, opts client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	f.calls = append(f.calls, id)
	f.opts = opts
	if f.statsErr != nil {
		return client.ContainerStatsResult{}, f.statsErr
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	for _, record := range f.records {
		if err := enc.Encode(record); err != nil {
			panic(err)
		}
	}
	return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(sb.String()))}, nil
}

var statsEpoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func statsRecord(offset time.Duration, total, system uint64) containerapi.StatsResponse {
	return containerapi.StatsResponse{
		Read: statsEpoch.Add(offset),
		CPUStats: containerapi.CPUStats{
			CPUUsage:    containerapi.CPUUsage{TotalUsage: total},
			SystemUsage: system,
			OnlineCPUs:  4,
		},
		MemoryStats: containerapi.MemoryStats{Usage: 1000, Limit: 4000},
		PidsStats:   containerapi.PidsStats{Current: 7},
	}
}

func collectStats(t *testing.T, cli client.APIClient, interval time.Duration) ([]*messages.ContainerStatsSample, bool, error) {
	t.Helper()
	var samples []*messages.ContainerStatsSample
	started := false
	err := streamStats(t.Context(), cli, "app-1", webID[:12], interval, func() { started = true }, func(s *messages.ContainerStatsSample) error {
		samples = append(samples, s)
		return nil
	})
	return samples, started, err
}

func TestStreamStats_BuildsSamples(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	first := statsRecord(0, 1000, 10000)
	first.Networks = map[string]containerapi.NetworkStats{
		"eth0": {RxBytes: 100, TxBytes: 10},
		"eth1": {RxBytes: 5, TxBytes: 1},
	}
	cli := &fakeStatsAPI{records: []containerapi.StatsResponse{first}}

	samples, started, err := collectStats(t, cli, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !started || !cli.opts.Stream {
		t.Errorf("started = %v, opts = %+v", started, cli.opts)
	}
	if len(cli.calls) != 1 || cli.calls[0] != webID {
		t.Errorf("stats must be read with the resolved full ID, got %v", cli.calls)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %v", samples)
	}
	s := samples[0]
	if s.TimestampMs != statsEpoch.UnixMilli() || s.MemUsageBytes != 1000 || s.MemLimitBytes != 4000 || s.Pids != 7 {
		t.Errorf("sample = %v", s)
	}
	if s.OnlineCpus != 4 {
		t.Errorf("OnlineCpus = %d, want 4", s.OnlineCpus)
	}
	if s.NetRxBytes != 105 || s.NetTxBytes != 11 {
		t.Errorf("network counters must be summed over all interfaces, got rx=%d tx=%d", s.NetRxBytes, s.NetTxBytes)
	}
	if s.CpuPercent != 0 {
		t.Errorf("the first sample has nothing to compare with, got %v", s.CpuPercent)
	}
}

func TestStreamStats_CPUDeltaBetweenSamples(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeStatsAPI{records: []containerapi.StatsResponse{
		statsRecord(0, 1000, 10000),
		statsRecord(2*time.Second, 1500, 12000),
	}}

	samples, _, err := collectStats(t, cli, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %v", samples)
	}
	// 500 / 2000 * 4 cpus * 100
	if got := samples[1].CpuPercent; math.Abs(got-100) > 1e-9 {
		t.Errorf("cpu = %v, want 100", got)
	}
}

func TestStreamStats_UsesPreviousSampleOfFirstRecord(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	first := statsRecord(0, 1500, 12000)
	first.PreCPUStats = statsRecord(0, 1000, 10000).CPUStats
	cli := &fakeStatsAPI{records: []containerapi.StatsResponse{first}}

	samples, _, err := collectStats(t, cli, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := samples[0].CpuPercent; math.Abs(got-100) > 1e-9 {
		t.Errorf("cpu = %v, want 100", got)
	}
}

func TestStreamStats_SamplesAtInterval(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	var records []containerapi.StatsResponse
	for i := range 7 {
		records = append(records, statsRecord(time.Duration(i)*time.Second, uint64(1000+i*100), uint64(10000+i*1000)))
	}
	cli := &fakeStatsAPI{records: records}

	samples, _, err := collectStats(t, cli, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, s := range samples {
		got = append(got, (s.TimestampMs-statsEpoch.UnixMilli())/1000)
	}
	if len(got) != 4 || got[0] != 0 || got[1] != 2 || got[2] != 4 || got[3] != 6 {
		t.Errorf("sample offsets = %v, want [0 2 4 6]", got)
	}
	// 200 / 2000 * 4 * 100 over the two second window, not the last second.
	if math.Abs(samples[1].CpuPercent-40) > 1e-9 {
		t.Errorf("cpu = %v, want 40", samples[1].CpuPercent)
	}
}

func TestStreamStats_SkipsRecordsOfStoppedContainers(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "exited")})
	cli := &fakeStatsAPI{records: []containerapi.StatsResponse{{}}}

	samples, started, err := collectStats(t, cli, 0)
	if err != nil || !started || len(samples) != 0 {
		t.Errorf("samples = %v, started = %v, err = %v", samples, started, err)
	}
}

func TestStreamStats_ContainerNotOwnedByApplication(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeStatsAPI{}

	err := streamStats(t.Context(), cli, "app-1", foreign, 0, func() {}, func(*messages.ContainerStatsSample) error { return nil })
	if !errors.Is(err, ErrContainerNotFound) || len(cli.calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, cli.calls)
	}
}

func TestStreamStats_OpenError(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeStatsAPI{statsErr: errors.New("daemon gone")}

	_, started, err := collectStats(t, cli, 0)
	if err == nil || !strings.Contains(err.Error(), "daemon gone") || started {
		t.Errorf("err = %v, started = %v", err, started)
	}
}

func TestStreamStats_EmitErrorStopsReading(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeStatsAPI{records: []containerapi.StatsResponse{statsRecord(0, 1, 1), statsRecord(time.Second, 2, 2)}}
	stop := errors.New("stop")
	calls := 0

	err := streamStats(t.Context(), cli, "app-1", webID, 0, func() {}, func(*messages.ContainerStatsSample) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}

func TestCPUPercent(t *testing.T) {
	cpu := func(total, system uint64, online uint32) containerapi.CPUStats {
		return containerapi.CPUStats{
			CPUUsage:    containerapi.CPUUsage{TotalUsage: total},
			SystemUsage: system,
			OnlineCPUs:  online,
		}
	}
	tests := []struct {
		name      string
		prev, cur containerapi.CPUStats
		want      float64
	}{
		{"one core fully used", cpu(0, 1000, 1), cpu(1000, 2000, 1), 100},
		{"more than one core", cpu(0, 1000, 4), cpu(2000, 2000, 4), 800},
		{"idle", cpu(500, 1000, 2), cpu(500, 2000, 2), 0},
		{"defaults to one cpu", cpu(0, 1000, 0), cpu(500, 2000, 0), 50},
		{"no system delta", cpu(100, 1000, 2), cpu(200, 1000, 2), 0},
		{"counters reset after a restart", cpu(900, 9000, 2), cpu(10, 100, 2), 0},
		{"no previous sample", cpu(0, 0, 2), cpu(500, 1000, 2), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cpuPercent(tt.prev, tt.cur); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cpuPercent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnlineCPUs(t *testing.T) {
	tests := []struct {
		name  string
		stats containerapi.CPUStats
		want  uint32
	}{
		{"reported", containerapi.CPUStats{OnlineCPUs: 8}, 8},
		{"defaults to one", containerapi.CPUStats{}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onlineCPUs(tt.stats); got != tt.want {
				t.Errorf("onlineCPUs = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMemoryUsage(t *testing.T) {
	tests := []struct {
		name  string
		stats containerapi.MemoryStats
		want  uint64
	}{
		{"cgroup v1 without cache", containerapi.MemoryStats{Usage: 1000, Stats: map[string]uint64{"total_inactive_file": 300}}, 700},
		{"cgroup v2 without cache", containerapi.MemoryStats{Usage: 1000, Stats: map[string]uint64{"inactive_file": 400}}, 600},
		{"no cache information", containerapi.MemoryStats{Usage: 1000}, 1000},
		{"cache larger than usage", containerapi.MemoryStats{Usage: 100, Stats: map[string]uint64{"inactive_file": 400}}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := memoryUsage(tt.stats); got != tt.want {
				t.Errorf("memoryUsage = %d, want %d", got, tt.want)
			}
		})
	}
}
