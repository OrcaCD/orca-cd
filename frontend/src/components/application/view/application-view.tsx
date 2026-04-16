import type { Application } from "@/lib/applications";
import { Box, GitBranch, GitCommit } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { ActionsMenu } from "@/components/application/view/actions-menu";
import { StatusBadge } from "@/components/status-badge";

type Props = {
	viewMode: "grid" | "list";
	apps: Application[];
};

export function ApplicationView({ viewMode, apps }: Props) {
	return viewMode === "grid" ? (
		<div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
			{apps.map((app) => (
				<Link
					key={app.id}
					to="/applications/$id"
					params={{ id: app.id }}
					className="group bg-card border border-border rounded-lg p-4 hover:border-primary/50 transition-colors"
				>
					<div className="flex items-start justify-between">
						<div className="flex items-center gap-3">
							<div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
								<Box className="h-5 w-5 text-primary" />
							</div>
							<div>
								<h3 className="font-medium group-hover:text-primary transition-colors">
									{app.name}
								</h3>
							</div>
						</div>
						<ActionsMenu />
					</div>

					<div className="flex gap-2 mt-4">
						<StatusBadge status={app.syncStatus} />
						<StatusBadge status={app.healthStatus} />
					</div>

					<div className="mt-4 pt-4 border-t border-border space-y-2">
						<div className="flex items-center gap-2 text-sm text-muted-foreground">
							<GitBranch className="h-4 w-4" />
							<span className="truncate">{app.repo}</span>
						</div>
						<div className="flex items-center justify-between text-sm">
							<div className="flex items-center gap-2 text-muted-foreground">
								<GitCommit className="h-4 w-4" />
								<span>{app.commit}</span>
							</div>
							<span className="text-muted-foreground">{app.lastSync}</span>
						</div>
					</div>
				</Link>
			))}
		</div>
	) : (
		<div className="bg-card border border-border rounded-lg overflow-hidden">
			<table className="w-full">
				<thead>
					<tr className="border-b border-border">
						<th className="text-left p-4 text-sm font-medium text-muted-foreground">Name</th>
						<th className="text-left p-4 text-sm font-medium text-muted-foreground hidden sm:table-cell">
							Project
						</th>
						<th className="text-left p-4 text-sm font-medium text-muted-foreground">Status</th>
						<th className="text-left p-4 text-sm font-medium text-muted-foreground hidden md:table-cell">
							Repository
						</th>
						<th className="text-left p-4 text-sm font-medium text-muted-foreground hidden lg:table-cell">
							Last Sync
						</th>
						<th className="p-4"></th>
					</tr>
				</thead>
				<tbody>
					{apps.map((app) => (
						<tr key={app.id} className="border-b border-border last:border-0 hover:bg-muted/50">
							<td className="p-4">
								<Link
									to="/applications/$id"
									params={{ id: app.id }}
									className="font-medium hover:text-primary"
								>
									{app.name}
								</Link>
							</td>
							<td className="p-4">
								<div className="flex gap-2 flex-wrap">
									<StatusBadge status={app.syncStatus} />
									<StatusBadge status={app.healthStatus} />
								</div>
							</td>
							<td className="p-4 text-muted-foreground hidden md:table-cell">{app.repo}</td>
							<td className="p-4 text-muted-foreground hidden lg:table-cell">{app.lastSync}</td>
							<td className="p-4">
								<ActionsMenu />
							</td>
						</tr>
					))}
				</tbody>
			</table>
		</div>
	);
}
