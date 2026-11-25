import { Braces, Download, LogsIcon, ShieldAlert } from "lucide-react";
import { useState } from "react";
import { InspectPanel } from "@/components/applications/monitoring/inspect-panel";
import { LogsPanel } from "@/components/applications/monitoring/logs-panel";
import { SearchField } from "@/components/search-field";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { API_BASE } from "@/lib/api";
import type { ContainerInfo } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";
import { usePermissions } from "@/lib/permissions";

type PanelTab = "logs" | "inspect";

export function ContainerPanel({
	applicationId,
	container,
}: {
	applicationId: string;
	container: ContainerInfo;
}) {
	const { can } = usePermissions();
	const [tab, setTab] = useState<PanelTab>("logs");
	const [filter, setFilter] = useState("");
	const [follow, setFollow] = useState(true);
	const [followedId, setFollowedId] = useState(container.id);
	const debouncedFilter = useDebouncedValue(filter);

	if (followedId !== container.id) {
		setFollowedId(container.id);
		setFollow(true);
	}

	if (!can("application", "inspect", applicationId)) {
		return (
			<Card>
				<CardContent className="flex items-start gap-3 text-sm text-muted-foreground">
					<ShieldAlert className="mt-0.5 size-4 shrink-0" />
					<p>{m.monitoringPanelNoPermission()}</p>
				</CardContent>
			</Card>
		);
	}

	const downloadUrl = `${API_BASE}/applications/${applicationId}/containers/${container.id}/logs/download`;

	return (
		<Card>
			<Tabs value={tab} onValueChange={(value) => setTab(value as PanelTab)}>
				<CardHeader className="flex flex-wrap items-center justify-between gap-3">
					<TabsList className="group-data-horizontal/tabs:h-9">
						<TabsTrigger value="logs" className="px-3">
							<LogsIcon />
							{m.monitoringLogs()}
						</TabsTrigger>
						<TabsTrigger value="inspect" className="px-3">
							<Braces />
							{m.monitoringInspect()}
						</TabsTrigger>
					</TabsList>
					{tab === "logs" ? (
						<div className="flex flex-wrap items-center gap-3">
							<SearchField
								value={filter}
								onChange={setFilter}
								label={m.monitoringLogsFilter()}
								placeholder={m.monitoringLogsFilter()}
								className="w-56"
							/>
							<label className="flex items-center gap-2 text-sm">
								<Switch
									checked={follow}
									onCheckedChange={setFollow}
									aria-label={m.monitoringLogsFollow()}
								/>
								{m.monitoringLogsFollow()}
							</label>
							<a href={downloadUrl} download className={buttonVariants({ variant: "outline" })}>
								<Download />
								{m.monitoringLogsDownload()}
							</a>
						</div>
					) : null}
				</CardHeader>
				<CardContent>
					<TabsContent value="logs">
						{container.logDriver === "none" ? (
							<p className="text-sm text-muted-foreground">{m.monitoringLogsDriverNone()}</p>
						) : (
							<LogsPanel
								applicationId={applicationId}
								containerId={container.id}
								runId={container.startedAt ?? ""}
								filter={debouncedFilter}
								follow={follow}
								onFollowChange={setFollow}
							/>
						)}
					</TabsContent>
					<TabsContent value="inspect">
						<InspectPanel applicationId={applicationId} container={container} />
					</TabsContent>
				</CardContent>
			</Tabs>
		</Card>
	);
}
