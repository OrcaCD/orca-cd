import { HealthStatus, SyncStatus, type Application } from "@/lib/applications";
import { createFileRoute, Link } from "@tanstack/react-router";
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
	Server,
	Settings,
} from "lucide-react";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useState } from "react";
import { useTheme } from "@/components/theme-provider";
import { highlighter } from "@/lib/highlighter";

export const Route = createFileRoute("/_authenticated/applications/$id/")({
	component: ApplicationDetailsPage,
	head: () => ({
		meta: [
			{
				title: "Applications",
			},
		],
	}),
});

const mockApp: Application = {
	id: "1",
	name: "api-gateway",
	syncStatus: SyncStatus.Synced,
	healthStatus: HealthStatus.Healthy,
	repo: "github.com/org/api-gateway",
	branch: "main",
	commit: "a3f2b1c",
	commitMessage: "fix: update rate limiting configuration",
	lastSync: "2 minutes ago",
	path: "/docker-compose.yml",
	agent: "prod-server-01",
};

function InfoCard({
	icon,
	label,
	value,
	subValue,
	isLink,
}: {
	icon: React.ReactNode;
	label: string;
	value: string;
	subValue?: string;
	isLink?: boolean;
}) {
	return (
		<div className="bg-card border border-border rounded-lg p-4">
			<div className="flex items-center gap-2 text-muted-foreground mb-2">
				{icon}
				<span className="text-sm">{label}</span>
			</div>
			<div className="font-medium truncate">
				{isLink ? (
					<a href="#" className="hover:text-primary flex items-center gap-1">
						{value} <ExternalLink className="h-3 w-3" />
					</a>
				) : (
					<span className={label.includes("Commit") ? "font-mono" : ""}>{value}</span>
				)}
			</div>
			{subValue && <p className="text-sm text-muted-foreground mt-1 truncate">{subValue}</p>}
		</div>
	);
}

const mockManifest = `version: "3.8"
services:
  api-gateway:
    image: org/api-gateway:v2.1.0
    ports:
      - "8080:80"
    environment:
      - NODE_ENV=production
    depends_on:
      - redis-cache

  redis-cache:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

  nginx-proxy:
    image: nginx:alpine
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf

volumes:
  redis-data:`;

function ApplicationDetailsPage() {
	const { id } = Route.useParams();
	const { theme } = useTheme();

	const [syncing, setSyncing] = useState(false);

	const handleSync = async () => {
		setSyncing(true);
		await new Promise((resolve) => setTimeout(resolve, 2000));
		setSyncing(false);
	};

	const html = highlighter.codeToHtml(mockManifest, {
		lang: "yaml",
		theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
	});
	return (
		<div className="p-6 space-y-6">
			<Breadcrumb>
				<BreadcrumbList>
					<BreadcrumbItem>
						<BreadcrumbLink asChild>
							<Link to="/applications">Applications</Link>
						</BreadcrumbLink>
					</BreadcrumbItem>
					<BreadcrumbSeparator />
					<BreadcrumbItem>
						<BreadcrumbPage>{mockApp.name}</BreadcrumbPage>
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
							<h1 className="text-2xl font-bold">{mockApp.name}</h1>
							<StatusBadge status={mockApp.syncStatus} type="sync" />
							<StatusBadge status={mockApp.healthStatus} type="health" />
						</div>
					</div>
				</div>
				<div className="flex items-center gap-2">
					<Button variant="outline" onClick={handleSync} disabled={syncing}>
						<RefreshCw className={`mr-2 h-4 w-4 ${syncing ? "animate-spin" : ""}`} />
						{syncing ? "Syncing..." : "Sync"}
					</Button>
					<Button variant="outline" asChild>
						<Link to="/applications/$id/settings" params={{ id: id }}>
							<Settings className="mr-2 h-4 w-4" />
							Settings
						</Link>
					</Button>
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="outline" size="icon">
								<MoreVertical className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuItem>Hard Refresh</DropdownMenuItem>
							<DropdownMenuItem>Rollback</DropdownMenuItem>
							<DropdownMenuItem className="text-destructive">Delete</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<InfoCard
					icon={<GitBranch className="h-4 w-4" />}
					label="Repository"
					value={mockApp.repo}
					subValue={mockApp.branch}
					isLink
				/>
				<InfoCard
					icon={<GitCommit className="h-4 w-4" />}
					label="Latest Commit"
					value={mockApp.commit}
					subValue={mockApp.commitMessage}
				/>
				<InfoCard
					icon={<Server className="h-4 w-4" />}
					label="Target Host"
					value={mockApp.agent}
					subValue={mockApp.path}
				/>
				<InfoCard
					icon={<Clock className="h-4 w-4" />}
					label="Last Sync"
					value={mockApp.lastSync}
					subValue="Auto-sync enabled"
				/>
			</div>

			<Tabs defaultValue="manifest" className="space-y-4">
				<TabsList className="bg-muted">
					<TabsTrigger value="manifest">Manifest</TabsTrigger>
				</TabsList>

				<TabsContent value="manifest" className="space-y-4">
					<div className="dark:bg-[#121212] border border-border rounded-lg p-4">
						<div className="text-sm font-mono text-muted-foreground overflow-x-auto">
							<div dangerouslySetInnerHTML={{ __html: html }} />
						</div>
					</div>
				</TabsContent>
			</Tabs>
		</div>
	);
}
