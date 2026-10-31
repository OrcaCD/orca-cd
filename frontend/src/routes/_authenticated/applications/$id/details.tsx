import { RelativeTime } from "@/components/relative-time";
import {
	deployApplication,
	HealthStatus,
	pullApplicationImages,
	SyncStatus,
	type Application,
} from "@/lib/applications";
import { createFileRoute } from "@tanstack/react-router";
import {
	ChevronDown,
	Clock,
	ExternalLink,
	FileCode,
	FolderGit2,
	GitBranch,
	GitCommit,
	GitPullRequestArrow,
	ImageDown,
	MousePointerClick,
	RefreshCw,
	Rocket,
	Server,
	Space,
	Sparkles,
	Webhook,
	Workflow,
	WrapText,
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
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import CopyButton from "@/components/copy-btn";
import { cn } from "@/lib/utils";
import { useFetch } from "@/lib/api";
import { toast } from "@/components/ui/toast";
import { m } from "@/lib/paraglide/messages";
import { diffArrays } from "diff";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import ErrorAlert from "@/components/alerts/error-alert";
import { getSyncTypeIcon, getSyncTypeLabel } from "@/components/badges/repository-sync-type-badge";
import { ApplicationEventSource } from "@/lib/application-events";
import { syncRepository } from "@/lib/repositories";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
	CodeBlock,
	CodeBlockActions,
	CodeBlockContent,
	CodeBlockCopyButton,
	CodeBlockHeader,
	CodeBlockLine,
	CodeBlockTitle,
} from "@/components/ui/code-block";
import { useHighlightedCode } from "@/hooks/use-highlighted-code";

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
	action,
	loading,
	children,
}: {
	icon: React.ReactNode;
	label: string;
	action?: React.ReactNode;
	loading?: boolean;
	children: React.ReactNode;
}) {
	return (
		<Card className="gap-3">
			<CardHeader className="min-h-7 items-center">
				<CardTitle className="flex items-center gap-2 text-sm font-normal text-muted-foreground">
					{icon}
					{label}
				</CardTitle>
				{action && !loading ? <CardAction className="row-span-1 -my-1">{action}</CardAction> : null}
			</CardHeader>
			<CardContent className="min-w-0 space-y-2">
				{loading ? (
					<>
						<Skeleton className="h-6 w-3/4" />
						<Skeleton className="h-4 w-1/2" />
						<Skeleton className="h-4 w-2/3" />
					</>
				) : (
					children
				)}
			</CardContent>
		</Card>
	);
}

function InfoValue({ className, ...props }: React.ComponentProps<"div">) {
	return <div className={cn("truncate text-base font-semibold", className)} {...props} />;
}

function InfoMeta({
	icon,
	title,
	children,
}: {
	icon: React.ReactNode;
	title?: string;
	children: React.ReactNode;
}) {
	return (
		<div className="flex min-w-0 items-center gap-2 text-sm text-muted-foreground" title={title}>
			<span className="shrink-0 [&_svg]:size-3.5">{icon}</span>
			<span className="truncate">{children}</span>
		</div>
	);
}

const lastSyncSourceIcons: Record<ApplicationEventSource, React.ReactNode> = {
	[ApplicationEventSource.Manual]: <MousePointerClick />,
	[ApplicationEventSource.ApplicationCreated]: <Sparkles />,
	[ApplicationEventSource.RepositoryPolling]: <RefreshCw />,
	[ApplicationEventSource.RepositoryWebhook]: <Webhook />,
	[ApplicationEventSource.GitHubActions]: <Workflow />,
	[ApplicationEventSource.ImagePolling]: <RefreshCw />,
	[ApplicationEventSource.ImageWebhook]: <Webhook />,
};

function ComposeFileView({
	path,
	copyValue,
	children,
}: {
	path?: string;
	copyValue?: string;
	children: React.ReactNode;
}) {
	const [wrap, setWrap] = useState(false);
	const [showWhitespace, setShowWhitespace] = useState(false);

	return (
		<CodeBlock data-show-whitespace={showWhitespace || undefined}>
			<CodeBlockHeader>
				{path && (
					<CodeBlockTitle>
						<FileCode />
						{path}
					</CodeBlockTitle>
				)}
				<CodeBlockActions>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={m.showWhitespace()}
						aria-pressed={showWhitespace}
						className="aria-pressed:bg-muted aria-pressed:text-foreground"
						onClick={() => setShowWhitespace(!showWhitespace)}
					>
						<Space />
					</Button>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={m.wrapLines()}
						aria-pressed={wrap}
						className="aria-pressed:bg-muted aria-pressed:text-foreground"
						onClick={() => setWrap(!wrap)}
					>
						<WrapText />
					</Button>
					{copyValue && <CodeBlockCopyButton value={copyValue} />}
				</CodeBlockActions>
			</CodeBlockHeader>
			<CodeBlockContent lineNumbers wrap={wrap}>
				{children}
			</CodeBlockContent>
		</CodeBlock>
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

interface ComposeDiffLine {
	variant: "default" | "added" | "removed";
	// Index into the previous lines for removed lines, into the current lines otherwise.
	index: number;
}

function buildComposeDiff(
	previousComposeFile: string,
	composeFile: string,
): { lines: ComposeDiffLine[]; additions: number; deletions: number } {
	const lines: ComposeDiffLine[] = [];
	let previousIndex = 0;
	let currentIndex = 0;

	for (const change of diffArrays(
		splitComposeLines(previousComposeFile),
		splitComposeLines(composeFile),
	)) {
		for (let i = 0; i < change.value.length; i++) {
			if (change.removed) {
				lines.push({ variant: "removed", index: previousIndex++ });
			} else {
				lines.push({ variant: change.added ? "added" : "default", index: currentIndex++ });
				if (!change.added) {
					previousIndex++;
				}
			}
		}
	}

	return {
		lines,
		additions: lines.filter((line) => line.variant === "added").length,
		deletions: lines.filter((line) => line.variant === "removed").length,
	};
}

function ApplicationDetailsPage() {
	const { id } = Route.useParams();

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

	const [pullingImages, setPullingImages] = useState(false);

	const handlePullImages = async () => {
		setPullingImages(true);
		try {
			await pullApplicationImages(id);
			toast.add({ type: "success", title: m.imagePullStarted() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedPullImages(),
			});
		} finally {
			setPullingImages(false);
		}
	};

	const imageSyncTriggers = getImageSyncTriggers(data);

	const deploymentInProgress =
		deploying || syncingRepository || pullingImages || data?.syncStatus === SyncStatus.Syncing;

	const composeFile = data?.composeFile ?? "";
	const previousComposeFile = data?.previousComposeFile ?? "";
	const composeLines = useHighlightedCode(composeFile, "yaml");
	const previousComposeLines = useHighlightedCode(previousComposeFile, "yaml");

	const composeDiff = useMemo(
		() => buildComposeDiff(previousComposeFile, composeFile),
		[previousComposeFile, composeFile],
	);

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
									<span className="mr-2 inline-flex overflow-hidden">
										<Rocket
											className={`h-4 w-4 ${deploymentInProgress ? "motion-safe:animate-rocket-launch" : ""}`}
										/>
									</span>
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
							<DropdownMenuItem onClick={handlePullImages} className="items-start cursor-pointer">
								<ImageDown className="mt-0.5 h-4 w-4" />
								<div className="flex flex-col gap-0.5">
									<span>{m.pullImages()}</span>
									<span className="text-xs text-muted-foreground">{m.pullImagesDescription()}</span>
								</div>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</ButtonGroup>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<InfoCard
					icon={<FolderGit2 className="h-4 w-4" />}
					label={m.applicationInfoRepository()}
					loading={!data}
					action={
						<Button
							variant="ghost"
							size="icon-sm"
							className="text-muted-foreground hover:text-foreground"
							aria-label={m.openRepository()}
							title={m.openRepository()}
							nativeButton={false}
							render={<a href={data?.repositoryUrl} target="_blank" rel="noopener noreferrer" />}
						>
							<ExternalLink />
						</Button>
					}
				>
					<InfoValue title={data?.repositoryName}>
						<a
							href={data?.repositoryUrl}
							target="_blank"
							rel="noopener noreferrer"
							className="hover:text-primary"
						>
							{data?.repositoryName}
						</a>
					</InfoValue>
					<InfoMeta icon={<GitBranch />} title={data?.branch}>
						<span className="font-mono">{data?.branch}</span>
					</InfoMeta>
					{data ? (
						<InfoMeta icon={getSyncTypeIcon(data.repositorySyncType, "")}>
							{m.applicationInfoRepositorySync({ type: getSyncTypeLabel(data.repositorySyncType) })}
						</InfoMeta>
					) : null}
					{imageSyncTriggers ? <InfoMeta icon={<ImageDown />}>{imageSyncTriggers}</InfoMeta> : null}
				</InfoCard>
				<InfoCard
					icon={<GitCommit className="h-4 w-4" />}
					label={m.applicationInfoLatestCommit()}
					loading={!data}
					action={data?.commit ? <CopyButton text={data.commit} /> : null}
				>
					<InfoValue className="font-mono" title={data?.commit}>
						{data?.commit?.slice(0, 7) || "—"}
					</InfoValue>
					{data?.commitMessage ? (
						<p className="line-clamp-2 text-sm text-muted-foreground" title={data.commitMessage}>
							{data.commitMessage}
						</p>
					) : null}
				</InfoCard>
				<InfoCard
					icon={<Server className="h-4 w-4" />}
					label={m.applicationInfoTargetHost()}
					loading={!data}
				>
					<InfoValue title={data?.agentName}>{data?.agentName}</InfoValue>
				</InfoCard>
				<InfoCard
					icon={<Clock className="h-4 w-4" />}
					label={m.applicationInfoLastSync()}
					loading={!data}
				>
					{data?.lastSyncedAt ? (
						<InfoValue>
							<RelativeTime value={data.lastSyncedAt} />
						</InfoValue>
					) : (
						<InfoValue>{m.never()}</InfoValue>
					)}
					{data?.lastSyncSource ? (
						<InfoMeta icon={lastSyncSourceIcons[data.lastSyncSource]}>
							{lastSyncSourceLabels[data.lastSyncSource]?.()}
						</InfoMeta>
					) : null}
				</InfoCard>
			</div>

			<Tabs defaultValue="manifest" className="space-y-4">
				<TabsList className="bg-muted">
					<TabsTrigger value="manifest">{m.manifest()}</TabsTrigger>
					<TabsTrigger value="diff">
						Diff
						{(composeDiff.additions > 0 || composeDiff.deletions > 0) && (
							<span className="font-mono text-xs">
								<span className="text-green-600 dark:text-green-400">+{composeDiff.additions}</span>{" "}
								<span className="text-red-600 dark:text-red-400">-{composeDiff.deletions}</span>
							</span>
						)}
					</TabsTrigger>
				</TabsList>

				<TabsContent value="manifest" className="space-y-4">
					<ComposeFileView path={data?.path} copyValue={composeFile}>
						{composeLines.map((line, index) => (
							<CodeBlockLine key={index}>{line}</CodeBlockLine>
						))}
					</ComposeFileView>
				</TabsContent>

				<TabsContent value="diff" className="space-y-4">
					<ComposeFileView path={data?.path}>
						{composeDiff.lines.map(({ variant, index }, i) => (
							<CodeBlockLine
								key={i}
								variant={variant}
								className={
									variant === "added"
										? "bg-green-500/15 after:text-green-600 dark:after:text-green-400"
										: undefined
								}
							>
								{variant === "removed" ? previousComposeLines[index] : composeLines[index]}
							</CodeBlockLine>
						))}
					</ComposeFileView>
				</TabsContent>
			</Tabs>
		</div>
	);
}
