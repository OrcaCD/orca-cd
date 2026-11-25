package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const StatsInterval = 2 * time.Second

const emptyRecordsUntilEnd = 3

// StreamStats blocks until the container stops or ctx is cancelled. started is
// called once the stats are open, a container that cannot be read fails before that.
func (c *Client) StreamStats(ctx context.Context, appID, containerID string, started func(), emit func(*messages.ContainerStatsSample) error) error {
	if !c.Ready() {
		return ErrDaemonUnreachable
	}
	return streamStats(ctx, c.cli.Client(), appID, containerID, StatsInterval, started, emit)
}

func streamStats(ctx context.Context, cli client.APIClient, appID, containerID string, interval time.Duration, started func(), emit func(*messages.ContainerStatsSample) error) error {
	item, err := resolveContainer(ctx, cli, appID, containerID)
	if err != nil {
		return err
	}
	result, err := cli.ContainerStats(ctx, item.ID, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return ErrContainerNotFound
		}
		return fmt.Errorf("read stats: %w", err)
	}
	defer func() { _ = result.Body.Close() }()
	started()

	// The daemon sends a record per second, a sample is taken whenever one
	// arrives a bit less than the interval after the previous sample.
	minGap := interval - interval/4
	decoder := json.NewDecoder(result.Body)
	var previous *container.StatsResponse
	emptyRecords := 0
	for {
		record := new(container.StatsResponse)
		if err := decoder.Decode(record); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode stats: %w", err)
		}
		// Stopped containers answer with an empty record.
		if record.Read.Year() <= 1 {
			if emptyRecords++; emptyRecords >= emptyRecordsUntilEnd {
				return nil
			}
			continue
		}
		emptyRecords = 0
		if previous != nil && record.Read.Sub(previous.Read) < minGap {
			continue
		}

		before := record.PreCPUStats
		if previous != nil {
			before = previous.CPUStats
		}
		// The first record has no PreCPUStats, so it is only a baseline.
		if before.SystemUsage == 0 {
			previous = record
			continue
		}
		if err := emit(buildStatsSample(record, before)); err != nil {
			return err
		}
		previous = record
	}
}

func buildStatsSample(record *container.StatsResponse, before container.CPUStats) *messages.ContainerStatsSample {
	sample := &messages.ContainerStatsSample{
		TimestampMs:   record.Read.UnixMilli(),
		CpuPercent:    cpuPercent(before, record.CPUStats),
		MemUsageBytes: memoryUsage(record.MemoryStats),
		MemLimitBytes: record.MemoryStats.Limit,
		Pids:          record.PidsStats.Current,
		OnlineCpus:    onlineCPUs(record.CPUStats),
	}
	for _, network := range record.Networks {
		sample.NetRxBytes += network.RxBytes
		sample.NetTxBytes += network.TxBytes
	}
	return sample
}

// cpuPercent is relative to one core, so a container using two cores is at 200.
func cpuPercent(before, now container.CPUStats) float64 {
	if before.SystemUsage == 0 || now.CPUUsage.TotalUsage < before.CPUUsage.TotalUsage || now.SystemUsage <= before.SystemUsage {
		return 0
	}
	cpuDelta := float64(now.CPUUsage.TotalUsage - before.CPUUsage.TotalUsage)
	systemDelta := float64(now.SystemUsage - before.SystemUsage)

	return cpuDelta / systemDelta * float64(onlineCPUs(now)) * 100
}

func onlineCPUs(stats container.CPUStats) uint32 {
	return max(stats.OnlineCPUs, 1)
}

// memoryUsage leaves out the page cache, which the kernel gives back under
// pressure, like `docker stats` does.
func memoryUsage(memory container.MemoryStats) uint64 {
	for _, key := range []string{"total_inactive_file", "inactive_file"} {
		if cache, ok := memory.Stats[key]; ok && cache < memory.Usage {
			return memory.Usage - cache
		}
	}
	return memory.Usage
}
