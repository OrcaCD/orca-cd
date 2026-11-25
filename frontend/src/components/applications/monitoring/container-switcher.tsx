import { containerStateDotClass } from "@/components/applications/monitoring/container-state";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { containerKey, isScaled, type ContainerInfo } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

export function containerLabel(container: ContainerInfo, containers: ContainerInfo[]): string {
	return isScaled(container, containers)
		? m.monitoringReplica({ service: container.service, number: container.containerNumber })
		: container.service;
}

export function ContainerSwitcher({
	containers,
	selected,
	onSelect,
}: {
	containers: ContainerInfo[];
	selected: ContainerInfo;
	onSelect: (key: string) => void;
}) {
	return (
		<Tabs
			value={containerKey(selected, containers)}
			onValueChange={(value) => onSelect(String(value))}
			className="max-w-full overflow-x-auto"
		>
			<TabsList
				aria-label={m.monitoringContainerSwitcher()}
				className="group-data-horizontal/tabs:h-10"
			>
				{containers.map((container) => (
					<TabsTrigger
						key={container.id}
						value={containerKey(container, containers)}
						className="gap-2 px-3.5"
					>
						<span
							aria-hidden="true"
							className={cn("size-2 rounded-full", containerStateDotClass(container.state))}
						/>
						{containerLabel(container, containers)}
					</TabsTrigger>
				))}
			</TabsList>
		</Tabs>
	);
}
