import { deleteApplication, HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import {
	Box,
	Clock,
	ExternalLink,
	GitBranch,
	GitCommit,
	MoreVertical,
	RefreshCw,
	RotateCcw,
	Server,
	Trash2,
} from "lucide-react";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useState } from "react";
import { useTheme } from "@/components/theme-provider";
import { highlighter } from "@/lib/highlighter";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { toast } from "sonner";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/applications/$id/")({
	component: ApplicationDetailsPage,
	head: () => ({
		meta: [
			{
				title: m.pageApplications(),
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
	subValue?: string;
	link?: string;
	isMonoValue?: boolean;
}) {
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
				{subValue && <p className="text-sm text-muted-foreground mt-1 truncate">{subValue}</p>}
			</CardContent>
		</Card>
	);
}

function ApplicationDetailsPage() {
	const { id } = Route.useParams();
	const navigate = useNavigate();
	const { theme } = useTheme();

	const { data } = useFetch<Application>("/applications/" + id);

	const [syncing, setSyncing] = useState(false);

	const handleSync = async () => {
		setSyncing(true);
		await new Promise((resolve) => setTimeout(resolve, 2000));
		setSyncing(false);
	};

	const html = highlighter.codeToHtml(data?.composeFile ?? "", {
		lang: "yaml",
		theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
	});

	async function deleteApp() {
		try {
			await deleteApplication(id);
			toast.success(m.toastApplicationDeleted({ name: data?.name ?? "" }));
			await navigate({ to: "/applications" });
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.toastDeleteApplicationFailed());
		}
	}
	return (
		<div className="p-6 space-y-6">
			<Breadcrumb>
				<BreadcrumbList>
					<BreadcrumbItem>
						<BreadcrumbLink asChild>
							<Link to="/applications">{m.pageApplications()}</Link>
						</BreadcrumbLink>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>{data?.name}</BreadcrumbPage>
					</BreadcrumbItem>
				</BreadcrumbList>
			</Breadcrumb>

			<div className="flex flex-col lg:flex-row lg:items-start justify-between gap-4">
				<div className="flex items-start gap-4">
					<div className="h-14 w-14 rounded-xl bg-primary/10 flex items-center justify-center">
						<Box className="h-7 w-7 text-primary" />
					</div>
					<div>
						<div className="flex items-center gap-3">
							<h1 className="text-2xl font-bold">{data?.name}</h1>
							<StatusBadge status={data?.syncStatus ?? SyncStatus.Unknown} type="sync" />
							<StatusBadge status={data?.healthStatus ?? HealthStatus.Unknown} type="health" />
						</div>
					</div>
				</div>
				<div className="flex items-center gap-2">
					<Button variant="outline" onClick={handleSync} disabled={syncing}>
						<RefreshCw className={`mr-2 h-4 w-4 ${syncing ? "animate-spin" : ""}`} />
						{syncing ? m.syncing() : m.sync()}
					</Button>

					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="outline" size="icon">
								<MoreVertical className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-full">
							<DropdownMenuItem>
								<RotateCcw className="mr-2 h-4 w-4" />
								{m.rollback()}
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<UpsertApplicationDialog application={data ?? null} asDropdownItem />
							<DropdownMenuSeparator />
							<ConfirmationDialog
								onConfirm={async () => await deleteApp()}
								triggerProps={{ variant: "destructive" }}
								asDropdownItem
								triggerText={
									<>
										<Trash2 className="mr-2 h-4 w-4" />
										{m.delete()}
									</>
								}
								description={m.deleteApplicationDescription()}
							></ConfirmationDialog>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<InfoCard
					icon={<GitBranch className="h-4 w-4" />}
					label={m.applicationInfoRepository()}
					value={data?.repositoryName ?? ""}
					subValue={data?.branch}
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
					subValue={m.applicationInfoAutoSyncEnabled()}
				/>
			</div>

			<Tabs defaultValue="manifest" className="space-y-4">
				<TabsList className="bg-muted">
					<TabsTrigger value="manifest">{m.manifest()}</TabsTrigger>
					<TabsTrigger value="events">{m.events()}</TabsTrigger>
				</TabsList>

				<TabsContent value="manifest" className="space-y-4">
					<div className="dark:bg-[#121212] border border-border rounded-lg p-4">
						<div className="text-sm font-mono text-muted-foreground overflow-x-auto">
							<div dangerouslySetInnerHTML={{ __html: html }} />
						</div>
					</div>
				</TabsContent>

				<TabsContent value="events" className="space-y-4">
					{m.comingSoon()}
				</TabsContent>
			</Tabs>
		</div>
	);
}
