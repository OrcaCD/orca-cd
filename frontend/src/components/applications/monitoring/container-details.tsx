import type { ReactNode } from "react";
import CopyButton from "@/components/copy-btn";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { portMappings, type ContainerInfo } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";

const SHORT_ID_LENGTH = 12;

export function ContainerDetails({ container }: { container: ContainerInfo }) {
	return (
		<div className="grid gap-4 lg:grid-cols-3">
			<PortsCard container={container} />
			<ContainerCard container={container} className="lg:col-span-2" />
		</div>
	);
}

function PortsCard({ container }: { container: ContainerInfo }) {
	const ports = portMappings(container.ports);

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.monitoringPorts()}</CardTitle>
			</CardHeader>
			<CardContent>
				{ports.length === 0 ? (
					<p className="text-sm text-muted-foreground">{m.monitoringNoPorts()}</p>
				) : (
					<ul className="flex flex-col gap-2 font-mono text-sm">
						{ports.map((port) => (
							<li key={port.key} className="flex flex-wrap items-center gap-x-2">
								{port.publicPort === null ? (
									<>
										<span>
											{port.privatePort}/{port.protocol}
										</span>
										<span className="font-sans text-xs text-muted-foreground">
											{m.monitoringPortNotPublished()}
										</span>
									</>
								) : (
									<>
										<span>
											{port.hostIp ? `${port.hostIp}:` : ""}
											{port.publicPort}
										</span>
										<span className="text-muted-foreground">→</span>
										<span>
											{port.privatePort}/{port.protocol}
										</span>
									</>
								)}
							</li>
						))}
					</ul>
				)}
			</CardContent>
		</Card>
	);
}

function ContainerCard({ container, className }: { container: ContainerInfo; className?: string }) {
	return (
		<Card className={className}>
			<CardHeader>
				<CardTitle>{m.monitoringContainer()}</CardTitle>
			</CardHeader>
			<CardContent>
				<dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[auto_1fr]">
					<DetailRow label={m.monitoringImage()}>
						<CopyableValue value={container.image} />
					</DetailRow>
					<DetailRow label={m.monitoringImageDigest()}>
						<CopyableValue value={container.imageDigest} />
					</DetailRow>
					<DetailRow label={m.monitoringContainerId()}>
						<CopyableValue
							value={container.id.slice(0, SHORT_ID_LENGTH)}
							copyValue={container.id}
						/>
					</DetailRow>
					<DetailRow label={m.monitoringRestartPolicy()}>
						<span>{container.restartPolicy || "—"}</span>
					</DetailRow>
					<DetailRow label={m.monitoringNetworks()}>
						{container.networks.length === 0 ? (
							<span className="text-muted-foreground">{m.monitoringNoNetworks()}</span>
						) : (
							<ul className="flex flex-col gap-1">
								{container.networks.map((network) => (
									<li key={network.name} className="flex flex-wrap items-center gap-x-2">
										<span>{network.name}</span>
										{network.ipAddress ? (
											<span className="font-mono text-xs text-muted-foreground">
												{network.ipAddress}
											</span>
										) : null}
									</li>
								))}
							</ul>
						)}
					</DetailRow>
					<DetailRow label={m.monitoringVolumes()}>
						{container.mounts.length === 0 ? (
							<span className="text-muted-foreground">{m.monitoringNoVolumes()}</span>
						) : (
							<ul className="flex flex-col gap-1">
								{container.mounts.map((mount) => (
									<li
										key={mount.destination}
										className="flex flex-wrap items-center gap-x-2 gap-y-1"
									>
										<Badge variant="outline">{mount.type}</Badge>
										{mount.name ? <span>{mount.name}</span> : null}
										<span className="font-mono text-xs text-muted-foreground">
											{mount.destination}
										</span>
										{mount.readOnly ? (
											<span className="text-xs text-muted-foreground">
												{m.monitoringReadOnly()}
											</span>
										) : null}
									</li>
								))}
							</ul>
						)}
					</DetailRow>
				</dl>
			</CardContent>
		</Card>
	);
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
	return (
		<>
			<dt className="text-muted-foreground">{label}</dt>
			<dd className="min-w-0">{children}</dd>
		</>
	);
}

function CopyableValue({ value, copyValue }: { value: string; copyValue?: string }) {
	if (!value) {
		return <span className="text-muted-foreground">—</span>;
	}

	return (
		<span className="flex min-w-0 items-center gap-1">
			<span className="min-w-0 truncate font-mono text-xs" title={copyValue ?? value}>
				{value}
			</span>
			<CopyButton text={copyValue ?? value} title={m.copyValue()} />
		</span>
	);
}
