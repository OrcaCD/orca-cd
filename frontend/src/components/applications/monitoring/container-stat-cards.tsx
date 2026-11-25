import { CpuIcon, MemoryStickIcon, NetworkIcon, RotateCwIcon } from "lucide-react";
import { StatCard } from "@/components/applications/monitoring/stat-card";
import { StatChart } from "@/components/applications/monitoring/stat-chart";
import { useContainerStats, type StatsStatus } from "@/hooks/use-container-stats";
import { useNow } from "@/hooks/use-now";
import type { ContainerInfo } from "@/lib/application-containers";
import { cpuShare, formatCpuCount, formatPercent } from "@/lib/container-stats";
import { m } from "@/lib/paraglide/messages";
import { formatBytes, formatByteRate, formatRelativeTime } from "@/lib/utils";

const NO_VALUE = "—";
const COLORS = {
	cpu: "oklch(0.685 0.169 237.323)",
	memory: "oklch(0.606 0.25 292.717)",
	rx: "oklch(0.696 0.17 162.48)",
	tx: "oklch(0.769 0.188 70.08)",
};

function statsHint(running: boolean, status: StatsStatus, hasData: boolean): string | null {
	if (!running || status === "ended") {
		return hasData ? m.monitoringStatsStopped() : m.monitoringStatsNotRunning();
	}
	if (!hasData) {
		return m.monitoringStatsWaiting();
	}
	return status === "connecting" ? m.monitoringStatsReconnecting() : null;
}

export function ContainerStatCards({
	applicationId,
	container,
}: {
	applicationId: string;
	container: ContainerInfo;
}) {
	const now = useNow();
	const running = container.state === "running";
	const { points, status } = useContainerStats({
		applicationId,
		containerId: container.id,
		enabled: running,
	});
	const latest = points.at(-1);
	const hint = statsHint(running, status, latest !== undefined);
	const cpu = latest ? cpuShare(latest, container.cpuLimit) : null;
	const rxRate = latest?.rxRate ?? null;
	const txRate = latest?.txRate ?? null;
	const txLine = txRate !== null ? `↑ ${formatByteRate(txRate)}` : latest ? `↑ ${NO_VALUE}` : hint;

	return (
		<div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
			<StatCard
				icon={CpuIcon}
				title={m.monitoringCpu()}
				value={cpu ? formatPercent(cpu.percent) : NO_VALUE}
				chart={
					<StatChart
						points={points}
						formatValue={formatPercent}
						series={[
							{
								key: "cpu",
								label: m.monitoringCpu(),
								color: COLORS.cpu,
								value: (point) => cpuShare(point, container.cpuLimit).percent,
							},
						]}
					/>
				}
			>
				{cpu ? (
					<span>
						{cpu.cpus > 1
							? m.monitoringCpuOf({ count: formatCpuCount(cpu.cpus) })
							: m.monitoringCpuOfOne({ count: formatCpuCount(cpu.cpus) })}
					</span>
				) : null}
				{hint ? <span>{hint}</span> : null}
			</StatCard>
			<StatCard
				icon={MemoryStickIcon}
				title={m.monitoringMemory()}
				value={latest ? formatBytes(latest.memUsageBytes) : NO_VALUE}
				chart={
					<StatChart
						points={points}
						formatValue={formatBytes}
						series={[
							{
								key: "memory",
								label: m.monitoringMemory(),
								color: COLORS.memory,
								value: (point) => point.memUsageBytes,
							},
						]}
					/>
				}
			>
				{latest && latest.memLimitBytes > 0 ? (
					<span>{m.monitoringMemoryOf({ limit: formatBytes(latest.memLimitBytes) })}</span>
				) : null}
				{hint ? <span>{hint}</span> : null}
			</StatCard>
			<StatCard
				icon={NetworkIcon}
				title={m.monitoringNetwork()}
				value={rxRate !== null ? `↓ ${formatByteRate(rxRate)}` : NO_VALUE}
				chart={
					<StatChart
						points={points}
						formatValue={formatByteRate}
						series={[
							{
								key: "rx",
								label: m.monitoringNetworkReceived(),
								color: COLORS.rx,
								value: (point) => point.rxRate,
							},
							{
								key: "tx",
								label: m.monitoringNetworkSent(),
								color: COLORS.tx,
								value: (point) => point.txRate,
							},
						]}
					/>
				}
			>
				<div className="flex flex-wrap justify-between gap-x-3 tabular-nums">
					<span>{txLine}</span>
					{latest ? (
						<span>
							{m.monitoringNetworkTotal()} ↓ {formatBytes(latest.netRxBytes)} ↑{" "}
							{formatBytes(latest.netTxBytes)}
						</span>
					) : null}
				</div>
				{latest && hint ? <span>{hint}</span> : null}
			</StatCard>
			<StatCard
				icon={RotateCwIcon}
				title={m.monitoringRestarts()}
				value={String(container.restartCount)}
			>
				<span>
					{container.startedAt
						? m.monitoringStarted({ time: formatRelativeTime(container.startedAt, now) })
						: m.monitoringNeverStarted()}
				</span>
				{container.finishedAt ? (
					<span>{m.monitoringExitCode({ code: container.exitCode })}</span>
				) : null}
			</StatCard>
		</div>
	);
}
