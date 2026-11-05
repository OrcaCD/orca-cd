import { Link } from "@tanstack/react-router";
import { ArrowRight, RotateCcw, RotateCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { toast } from "@/components/ui/toast";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { deployApplication } from "@/lib/applications";
import { AgentStatus } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import { getStatusIndicator, type AttentionItem } from "@/lib/fleet";

const maxVisibleItems = 5;

export function AttentionList({ items }: { items: AttentionItem[] }) {
	if (items.length === 0) {
		return null;
	}

	return (
		<Card className="gap-0 py-0 ring-amber-500/35 dark:ring-amber-400/25">
			<div className="flex items-center gap-2 border-b border-amber-500/20 bg-amber-500/8 px-4 py-2.5 dark:bg-amber-400/6">
				<TriangleAlert className="size-4 text-amber-600 dark:text-amber-400" />
				<h2 className="text-sm font-medium">
					{m.applicationsAttentionTitle({ count: items.length })}
				</h2>
			</div>
			<ul className="divide-y">
				{items.slice(0, maxVisibleItems).map((item) => (
					<AttentionRow key={item.app.id} item={item} />
				))}
			</ul>
			{items.length > maxVisibleItems ? (
				<p className="border-t px-4 py-2 text-xs text-muted-foreground">
					{m.applicationsAttentionMore({ count: items.length - maxVisibleItems })}
				</p>
			) : null}
		</Card>
	);
}

function AttentionRow({ item }: { item: AttentionItem }) {
	const { app, reason, agentStatus } = item;
	const [deploying, setDeploying] = useState(false);
	const status = getStatusIndicator(app);

	const handleRetry = async () => {
		setDeploying(true);
		try {
			await deployApplication(app.id);
			toast.add({
				type: "success",
				title: m.deploymentStarted(),
			});
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeployApplication(),
			});
		} finally {
			setDeploying(false);
		}
	};

	return (
		<li className="flex items-center gap-x-3 gap-y-2 px-4 py-2.5 transition-colors hover:bg-muted/40 sm:items-center sm:py-2">
			<div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted ring-1 ring-foreground/5 ring-inset">
				<StaticLucideIcon name={app.icon} className="size-4 text-primary" />
			</div>
			<div className="flex min-w-0 flex-1 flex-col gap-0.5 md:flex-row md:items-center md:gap-4">
				<div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 md:w-72 md:shrink-0">
					<span className="truncate text-sm font-medium">{app.name}</span>
					<span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
						<span className={cn("size-1.5 rounded-full", status.dotClass)} />
						{status.label()}
					</span>
					{app.rolledBackAt ? (
						<span className="flex shrink-0 items-center gap-1 text-xs text-amber-700 dark:text-amber-400">
							<RotateCcw className="size-3" />
							{m.applicationsRolledBack()}
						</span>
					) : null}
				</div>
				{/* Reasons are often raw sync errors, so small screens get them in full instead of a
				    truncated first few words. */}
				<p className="min-w-0 truncate text-sm text-muted-foreground" title={reason}>
					{reason}
				</p>
			</div>
			<div className="flex shrink-0 items-center gap-1">
				{(app.lastSyncError || app.rolledBackAt) && agentStatus === AgentStatus.Online ? (
					<Button variant="outline" size="sm" disabled={deploying} onClick={handleRetry}>
						<RotateCw className={cn(deploying && "motion-safe:animate-spin")} />
						{m.applicationsAttentionRetry()}
					</Button>
				) : null}
				<Button
					variant="ghost"
					size="sm"

					aria-label={m.details()}
					nativeButton={false}
					render={<Link to="/applications/$id" params={{ id: app.id }} />}
				>
					{m.details()}
					<ArrowRight />
				</Button>
			</div>
		</li>
	);
}
