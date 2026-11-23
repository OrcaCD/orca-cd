import type { HealthStatus } from "./applications";
import { useFetch } from "./api";

export interface ContainerPort {
	ip: string;
	publicPort: number;
	privatePort: number;
	protocol: string;
}

export interface ContainerNetwork {
	name: string;
	ipAddress: string;
	aliases: string[];
}

export interface ContainerMount {
	type: string;
	name: string;
	destination: string;
	readOnly: boolean;
}

export interface ContainerInfo {
	id: string;
	name: string;
	service: string;
	containerNumber: number;
	state: string;
	health: HealthStatus;
	healthRaw: string;
	healthFailingStreak: number;
	image: string;
	imageId: string;
	imageDigest: string;
	restartCount: number;
	restartPolicy: string;
	exitCode: number;
	oomKilled: boolean;
	error: string;
	logDriver: string;
	cpuLimit: number;
	createdAt: string | null;
	startedAt: string | null;
	finishedAt: string | null;
	ports: ContainerPort[];
	networks: ContainerNetwork[];
	mounts: ContainerMount[];
}

const CONTAINER_REFRESH_INTERVAL = 10_000;

// A string key on purpose: live application updates refetch everything below /applications.
export function useApplicationContainers(applicationId: string) {
	return useFetch<{ containers: ContainerInfo[] }>(`/applications/${applicationId}/containers`, {
		refreshInterval: CONTAINER_REFRESH_INTERVAL,
	});
}

export function isScaled(container: ContainerInfo, containers: ContainerInfo[]): boolean {
	return containers.some(
		(other) => other.id !== container.id && other.service === container.service,
	);
}

/** Stable across restarts of the container. */
export function containerKey(container: ContainerInfo, containers: ContainerInfo[]): string {
	return isScaled(container, containers)
		? `${container.service}#${container.containerNumber}`
		: container.service;
}

export function findContainer(
	containers: ContainerInfo[],
	key: string | undefined,
): ContainerInfo | undefined {
	return containers.find((container) => containerKey(container, containers) === key);
}

const WILDCARD_ADDRESSES = new Set(["", "0.0.0.0", "::"]);

export interface PortMapping {
	key: string;
	hostIp: string | null;
	publicPort: number | null;
	privatePort: number;
	protocol: string;
}

/** Docker reports a published port once per host address, which would list it twice for IPv4 and IPv6. */
export function portMappings(ports: ContainerPort[]): PortMapping[] {
	const mappings = new Map<string, PortMapping>();
	for (const port of ports) {
		const hostIp = WILDCARD_ADDRESSES.has(port.ip) ? null : port.ip;
		const publicPort = port.publicPort > 0 ? port.publicPort : null;
		const key = `${hostIp ?? "*"}:${publicPort ?? ""}:${port.privatePort}/${port.protocol}`;
		if (!mappings.has(key)) {
			mappings.set(key, {
				key,
				hostIp,
				publicPort,
				privatePort: port.privatePort,
				protocol: port.protocol,
			});
		}
	}
	return [...mappings.values()];
}
