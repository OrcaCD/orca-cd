import { Box, MoreVertical, RefreshCw, Settings } from "lucide-react";
import { StatusBadge } from "@/components/status-badge";
import type { Application } from "@/lib/applications";
import { Button } from "@/components/ui/button";
import { Link } from "@tanstack/react-router";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useState } from "react";

interface Props {
	app: Application;
	id: string;
}

export function Header({ app, id }: Props) {
	const [syncing, setSyncing] = useState(false);

	const handleSync = async () => {
		setSyncing(true);
		await new Promise((resolve) => setTimeout(resolve, 2000));
		setSyncing(false);
	};
	return (
		<div className="flex flex-col lg:flex-row lg:items-start justify-between gap-4">
			<div className="flex items-start gap-4">
				<div className="h-14 w-14 rounded-xl bg-primary/10 flex items-center justify-center">
					<Box className="h-7 w-7 text-primary" />
				</div>
				<div>
					<div className="flex items-center gap-3">
						<h1 className="text-2xl font-bold">{app.name}</h1>
						<StatusBadge status={app.syncStatus} />
						<StatusBadge status={app.healthStatus} />
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
	);
}
