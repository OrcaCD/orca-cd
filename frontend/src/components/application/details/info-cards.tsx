import type { Application } from "@/lib/applications";
import { Clock, ExternalLink, GitBranch, GitCommit, Server } from "lucide-react";

interface InfoCardProps {
	icon: React.ReactNode;
	label: string;
	value: string;
	subValue?: string;
	isLink?: boolean;
}

function InfoCard({ icon, label, value, subValue, isLink }: InfoCardProps) {
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

export function InfoCards({ app }: { app: Application }) {
	return (
		<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
			<InfoCard
				icon={<GitBranch className="h-4 w-4" />}
				label="Repository"
				value={app.repo}
				subValue={app.branch}
				isLink
			/>
			<InfoCard
				icon={<GitCommit className="h-4 w-4" />}
				label="Latest Commit"
				value={app.commit}
				subValue={app.commitMessage}
			/>
			<InfoCard
				icon={<Server className="h-4 w-4" />}
				label="Target Host"
				value={app.agent}
				subValue={app.path}
			/>
			<InfoCard
				icon={<Clock className="h-4 w-4" />}
				label="Last Sync"
				value={app.lastSync}
				subValue="Auto-sync enabled"
			/>
		</div>
	);
}
