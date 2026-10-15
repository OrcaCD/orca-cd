import { Link } from "@tanstack/react-router";
import { ArrowRight, RotateCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { toast } from "@/components/ui/toast";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Item,
	ItemActions,
	ItemContent,
	ItemDescription,
	ItemGroup,
	ItemMedia,
	ItemTitle,
} from "@/components/ui/item";
import { deployApplication } from "@/lib/applications";
import { AgentStatus } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";
import { fleetCategoryConfig, type AttentionItem } from "@/lib/fleet";

const maxVisibleItems = 5;

export function AttentionList({ items }: { items: AttentionItem[] }) {
	if (items.length === 0) {
		return null;
	}

	return (
		<Card className="bg-amber-500/5 ring-amber-500/30">
			<CardHeader>
				<CardTitle className="flex items-center gap-2 text-amber-400">
					<TriangleAlert className="size-4" />
					{m.applicationsAttentionTitle({ count: items.length })}
				</CardTitle>
			</CardHeader>
			<CardContent>
				<ItemGroup className="gap-1">
					{items.slice(0, maxVisibleItems).map((item) => (
						<AttentionRow key={item.app.id} item={item} />
					))}
				</ItemGroup>
				{items.length > maxVisibleItems ? (
					<p className="mt-2 px-3 text-xs text-muted-foreground">
						{m.applicationsAttentionMore({ count: items.length - maxVisibleItems })}
					</p>
				) : null}
			</CardContent>
		</Card>
	);
}

function AttentionRow({ item }: { item: AttentionItem }) {
	const { app, category, reason, agentStatus } = item;
	const [deploying, setDeploying] = useState(false);
	const config = fleetCategoryConfig[category];

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
		<Item size="sm" className="hover:bg-muted/40">
			<ItemMedia className="size-8 rounded-md bg-primary/10">
				<StaticLucideIcon name={app.icon} className="size-4 text-primary" />
			</ItemMedia>
			<ItemContent className="min-w-0">
				<ItemTitle>
					{app.name}
					<span className="flex items-center gap-1.5 text-xs font-normal text-muted-foreground">
						<span className={cn("size-1.5 rounded-full", config.dotClass)} />
						{config.label()}
					</span>
				</ItemTitle>
				<ItemDescription className="truncate" title={reason}>
					{reason}
				</ItemDescription>
			</ItemContent>
			<ItemActions>
				{app.lastSyncError && agentStatus === AgentStatus.Online ? (
					<Button variant="outline" size="sm" disabled={deploying} onClick={handleRetry}>
						<RotateCw className={cn(deploying && "animate-spin")} />
						{m.applicationsAttentionRetry()}
					</Button>
				) : null}
				<Button
					variant="ghost"
					size="sm"
					nativeButton={false}
					render={<Link to="/applications/$id" params={{ id: app.id }} />}
				>
					{m.details()}
					<ArrowRight />
				</Button>
			</ItemActions>
		</Item>
	);
}
