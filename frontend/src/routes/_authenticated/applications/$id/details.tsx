import { deployApplication, HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute } from "@tanstack/react-router";
import {
	ChevronDown,
	Clock,
	ExternalLink,
	GitBranch,
	GitCommit,
	GitPullRequestArrow,
	Rocket,
	Server,
} from "lucide-react";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useMemo, useState } from "react";
import { useTheme } from "@/components/theme-provider";
import { highlighter } from "@/lib/highlighter";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import { toast } from "@/components/ui/toast";
import { m } from "@/lib/paraglide/messages";
import { transformerNotationDiff, transformerRenderWhitespace } from "@shikijs/transformers";
import { diffArrays } from "diff";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import ErrorAlert from "@/components/alerts/error-alert";
import { getSyncTypeLabel } from "@/components/badges/repository-sync-type-badge";
import { ApplicationEventSource } from "@/lib/application-events";
import { syncRepository } from "@/lib/repositories";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export const Route = createFileRoute("/_authenticated/applications/$id/details")({
	component: ApplicationDetailsPage,
	head: () => ({
		meta: [
			{
				title: `${m.pageApplications()} - ${m.details()}`,
			},
		],
	}),
});

function InfoCard({
	icon,
	label,
	value,
	subValue,
	link,
	isMonoValue,
}: {
	icon: React.ReactNode;
	label: string;
	value: string;
	subValue?: string | string[];
	link?: string;
	isMonoValue?: boolean;
}) {
	const subValues = [subValue ?? []].flat().filter(Boolean);

	return (
		<Card>
			<CardHeader>
				<CardTitle>
					<div className="flex items-center gap-2 text-muted-foreground mb-2">
						{icon}
						<span className="text-sm">{label}</span>
					</div>
				</CardTitle>
			</CardHeader>
			<CardContent>
				<div className="font-medium truncate">
					{link ? (
						<a
							href={link}
							target="_blank"
							rel="noopener noreferrer"
							className="hover:text-primary flex items-center gap-1"
						>
							{value} <ExternalLink className="h-3 w-3" />
						</a>
					) : (
						<span className={isMonoValue ? "font-mono" : ""}>{value}</span>
					)}
				</div>
				{subValues.map((line) => (
					<p key={line} className="text-sm text-muted-foreground mt-1 truncate">
						{line}
					</p>
				))}
			</CardContent>
		</Card>
	);
}

const lastSyncSourceLabels: Record<ApplicationEventSource, () => string> = {
	[ApplicationEventSource.Manual]: () => m.applicationInfoLastSyncSourceManual(),
	[ApplicationEventSource.ApplicationCreated]: () =>
		m.applicationInfoLastSyncSourceApplicationCreated(),
	[ApplicationEventSource.RepositoryPolling]: () =>
		m.applicationInfoLastSyncSourceRepositoryPolling(),
	[ApplicationEventSource.RepositoryWebhook]: () =>
		m.applicationInfoLastSyncSourceRepositoryWebhook(),
	[ApplicationEventSource.GitHubActions]: () => m.applicationInfoLastSyncSourceGitHubActions(),
	[ApplicationEventSource.ImagePolling]: () => m.applicationInfoLastSyncSourceImagePolling(),
	[ApplicationEventSource.ImageWebhook]: () => m.applicationInfoLastSyncSourceImageWebhook(),
};

function getImageSyncTriggers(app?: Application): string | undefined {
	if (!app) {
		return undefined;
	}

	const imageTriggers: string[] = [];
	if (app.imagePollEnabled) {
		imageTriggers.push(
			m.applicationInfoSyncImagePolling({ seconds: app.imagePollIntervalSeconds }),
		);
	}
	if (app.imageWebhookEnabled) {
		imageTriggers.push(m.webhook());
	}
	if (imageTriggers.length === 0) {
		return undefined;
	}

	return m.applicationInfoSyncImages({ type: imageTriggers.join(", ") });
}

function withDiffMarker(line: string, marker: "++" | "--"): string {
	if (line.length === 0) {
		return line;
	}

	return `${line} # [!code ${marker}]`;
}

function splitComposeLines(composeFile: string): string[] {
	if (composeFile.length === 0) {
		return [];
	}

	const lines = composeFile.split("\n");
	if (lines.at(-1) === "") {
		lines.pop();
	}

	return lines;
}

function buildComposeDiff(previousComposeFile: string, composeFile: string): string {
	const previousLines = splitComposeLines(previousComposeFile);
	const currentLines = splitComposeLines(composeFile);

	const lines: string[] = [];

	for (const change of diffArrays(previousLines, currentLines)) {
		if (change.added) {
			lines.push(...change.value.map((line) => withDiffMarker(line, "++")));
			continue;
		}

		if (change.removed) {
			lines.push(...change.value.map((line) => withDiffMarker(line, "--")));
			continue;
		}

		lines.push(...change.value);
	}

	return lines.join("\n");
}

function ApplicationDetailsPage() {
	const { id } = Route.useParams();
	const { theme } = useTheme();

	const { data } = useFetch<Application>("/applications/" + id);

	const [deploying, setDeploying] = useState(false);

	const handleDeploy = async () => {
		setDeploying(true);
		try {
			await deployApplication(id);
			toast.add({ type: "success", title: m.deploymentStarted() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeployApplication(),
			});
		} finally {
			setDeploying(false);
		}
	};

	const [syncingRepository, setSyncingRepository] = useState(false);

	const handleSyncRepository = async () => {
		if (!data) {
			return;
		}
		setSyncingRepository(true);
		try {
			await syncRepository(data.repositoryId);
			toast.add({ type: "success", title: m.syncTriggered() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedTriggerSync(),
			});
		} finally {
			setSyncingRepository(false);
		}
	};

	const deploymentInProgress =
		deploying || syncingRepository || data?.syncStatus === SyncStatus.Syncing;

	const manifestHtml = useMemo(() => {
		return highlighter.codeToHtml(data?.composeFile ?? "", {
			lang: "yaml",
			theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
			transformers: [transformerRenderWhitespace()],
		});
	}, [data?.composeFile, theme]);

	const diffHtml = useMemo(() => {
		const composeDiff = buildComposeDiff(data?.previousComposeFile ?? "", data?.composeFile ?? "");
		return highlighter.codeToHtml(composeDiff, {
			lang: "yaml",
			theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
			transformers: [transformerNotationDiff(), transformerRenderWhitespace()],
		});
	}, [data?.previousComposeFile, data?.composeFile, theme]);

	return (
		<div className="space-y-6">
			{data?.lastSyncError && (
				<ErrorAlert title={m.syncFailed()} description={data.lastSyncError} />
			)}
			<div className="flex flex-col lg:flex-row lg:items-start justify-between gap-4">
				<div className="flex min-w-0 items-start gap-4">
					<div className="h-14 w-14 shrink-0 rounded-xl bg-primary/10 flex items-center justify-center">
						<StaticLucideIcon name={data?.icon} className="h-7 w-7 text-primary" />
					</div>
					<div className="min-w-0 space-y-2">
						<h1 className="text-2xl font-bold wrap-break-word">{data?.name}</h1>
						<div className="flex flex-wrap items-center gap-2">
							<ApplicationStatusBadge status={data?.syncStatus ?? SyncStatus.Unknown} type="sync" />
							<ApplicationStatusBadge
								status={data?.healthStatus ?? HealthStatus.Unknown}
								type="health"
							/>
						</div>
					</div>
				</div>
				<ButtonGroup>
					<Tooltip>
						<TooltipTrigger
							render={
								<Button variant="outline" onClick={handleDeploy} disabled={deploymentInProgress}>
									<Rocket
										className={`mr-2 h-4 w-4 ${deploymentInProgress ? "animate-spin" : ""}`}
									/>
									{deploymentInProgress ? m.deploying() : m.deploy()}
								</Button>
							}
						/>
						<TooltipContent>{m.deployCurrentVersionDescription()}</TooltipContent>
					</Tooltip>
					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button
									variant="outline"
									size="icon"
									aria-label={m.deployOptions()}
									disabled={deploymentInProgress || !data}
								/>
							}
						>
							<ChevronDown className="h-4 w-4" />
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-72">
							<DropdownMenuItem
								onClick={handleSyncRepository}
								className="items-start cursor-pointer"
							>
								<GitPullRequestArrow className="mt-0.5 h-4 w-4" />
								<div className="flex flex-col gap-0.5">
									<span>{m.syncRepository()}</span>
									<span className="text-xs text-muted-foreground">
										{m.syncRepositoryDescription()}
									</span>
								</div>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</ButtonGroup>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<InfoCard
					icon={<GitBranch className="h-4 w-4" />}
					label={m.applicationInfoRepository()}
					value={data?.repositoryName ?? ""}
					subValue={[
						data?.branch ?? "",
						data
							? m.applicationInfoRepositorySync({ type: getSyncTypeLabel(data.repositorySyncType) })
							: "",
					]}
					link={data?.repositoryUrl}
				/>
				<InfoCard
					icon={<GitCommit className="h-4 w-4" />}
					label={m.applicationInfoLatestCommit()}
					value={data?.commit?.slice(0, 7) ?? ""}
					subValue={data?.commitMessage}
					isMonoValue
				/>
				<InfoCard
					icon={<Server className="h-4 w-4" />}
					label={m.applicationInfoTargetHost()}
					value={data?.agentName ?? ""}
					subValue={data?.path}
				/>
				<InfoCard
					icon={<Clock className="h-4 w-4" />}
					label={m.applicationInfoLastSync()}
					value={data?.lastSyncedAt ? new Date(data.lastSyncedAt).toLocaleString() : m.never()}
					subValue={[
						data?.lastSyncSource ? (lastSyncSourceLabels[data.lastSyncSource]?.() ?? "") : "",
						getImageSyncTriggers(data) ?? "",
					]}
				/>
			</div>

			<Tabs defaultValue="manifest" className="space-y-4">
				<TabsList className="bg-muted">
					<TabsTrigger value="manifest">{m.manifest()}</TabsTrigger>
					<TabsTrigger value="diff">Diff</TabsTrigger>
				</TabsList>

				<TabsContent value="manifest" className="space-y-4">
					<div className="dark:bg-[#121212] border border-border rounded-lg p-4">
						<div className="text-sm font-mono text-muted-foreground overflow-x-auto">
							<div dangerouslySetInnerHTML={{ __html: manifestHtml }} />
						</div>
					</div>
				</TabsContent>

				<TabsContent value="diff" className="space-y-4">
					<div className="dark:bg-[#121212] border border-border rounded-lg p-4">
						<div className="text-sm font-mono text-muted-foreground overflow-x-auto">
							<div dangerouslySetInnerHTML={{ __html: diffHtml }} />
						</div>
					</div>
				</TabsContent>
			</Tabs>
		</div>
	);
}
