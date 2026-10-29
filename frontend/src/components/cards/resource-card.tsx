import type { ReactNode } from "react";
import { CircleAlert } from "lucide-react";
import { Card } from "@/components/ui/card";
import { m } from "@/lib/paraglide/messages";
import { cn, formatRelativeTime } from "@/lib/utils";

export type FlowState = "ok" | "progress" | "warning" | "error" | "unknown" | "info" | "off";

export interface FlowStage {
	label: string;
	value: ReactNode;
	state: FlowState;
	title?: string;
	mono?: boolean;
}

const nodeClass: Record<FlowState, string> = {
	ok: "border-emerald-500 bg-emerald-500",
	progress: "border-blue-500 bg-blue-500/25 motion-safe:animate-pulse",
	warning: "border-amber-500 bg-amber-500",
	error: "border-red-500 bg-red-500 ring-4 ring-red-500/15",
	unknown: "border-dashed border-zinc-400 dark:border-zinc-500",
	info: "border-primary bg-card",
	off: "border-zinc-400 bg-zinc-400/30 dark:border-zinc-500",
};

const segmentClass: Record<FlowState, string> = {
	ok: "border-emerald-500/60",
	progress: "",
	warning: "border-amber-500/60",
	error: "border-red-500/60",
	unknown: "border-dashed border-zinc-400/60",
	info: "border-foreground/15",
	off: "border-dashed border-zinc-400/60",
};

const valueClass: Record<FlowState, string> = {
	ok: "",
	progress: "text-blue-600 dark:text-blue-400",
	warning: "text-amber-600 dark:text-amber-400",
	error: "text-red-600 dark:text-red-400",
	unknown: "text-muted-foreground",
	info: "",
	off: "text-muted-foreground",
};

const brokenStates = new Set<FlowState>(["error", "unknown", "off"]);

function FlowSegment({ state, interrupted }: { state: FlowState; interrupted: boolean }) {
	if (interrupted) {
		return <span className="mx-1.5 flex-1 border-t border-dashed border-zinc-400/40" />;
	}
	if (state === "progress") {
		return (
			<span className="mx-1.5 h-0.5 flex-1 bg-[repeating-linear-gradient(90deg,var(--color-blue-500)_0_4px,transparent_4px_8px)] motion-safe:animate-flow-dash" />
		);
	}
	return <span className={cn("mx-1.5 flex-1 border-t", segmentClass[state])} />;
}

/**
 * Shows the stages a resource passes through (e.g. revision → sync → health). Each segment takes
 * the state of the stage it leads to; once a stage is broken or unknown, everything after it is
 * drawn as interrupted, so the point where the flow stops is visible at a glance.
 */
function ResourceFlow({ stages }: { stages: [FlowStage, FlowStage, FlowStage] }) {
	const firstBroken = stages.findIndex((stage) => brokenStates.has(stage.state));

	return (
		<div>
			<div className="flex h-3 items-center" aria-hidden="true">
				{stages.map((stage, index) => (
					<div key={stage.label} className={cn("flex items-center", index > 0 && "flex-1")}>
						{index > 0 ? (
							<FlowSegment
								state={stage.state}
								interrupted={firstBroken !== -1 && index > firstBroken}
							/>
						) : null}
						<span className={cn("size-3 shrink-0 rounded-full border-2", nodeClass[stage.state])} />
					</div>
				))}
			</div>
			<dl className="mt-2.5 grid grid-cols-3 gap-3">
				{stages.map((stage, index) => (
					<div
						key={stage.label}
						className={cn(
							"min-w-0",
							index === 1 && "text-center",
							index === stages.length - 1 && "text-right",
						)}
					>
						<dt className="text-[0.6875rem] font-medium tracking-wider text-muted-foreground uppercase">
							{stage.label}
						</dt>
						<dd
							className={cn(
								"mt-0.5 truncate leading-5 font-medium",
								stage.mono && "font-mono",
								valueClass[stage.state],
							)}
							title={stage.title ?? (typeof stage.value === "string" ? stage.value : undefined)}
						>
							{stage.value}
						</dd>
					</div>
				))}
			</dl>
		</div>
	);
}

interface ResourceCardProps {
	icon: ReactNode;
	title: string;
	subtitle?: ReactNode;
	monoSubtitle?: boolean;
	action?: ReactNode;
	stages: [FlowStage, FlowStage, FlowStage];
	error?: string | null;
	meta?: ReactNode;
	time?: string | null;
	className?: string;
}

export function ResourceCard({
	icon,
	title,
	subtitle,
	monoSubtitle = false,
	action,
	stages,
	error,
	meta,
	time,
	className,
}: ResourceCardProps) {
	return (
		<Card className={cn("h-full gap-0 py-0 transition-shadow hover:ring-primary/50", className)}>
			<div className="flex items-center gap-3 px-4 pt-4">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted/60">
					{icon}
				</div>
				<div className="min-w-0 flex-1">
					<h3 className="truncate text-base leading-snug font-medium" title={title}>
						{title}
					</h3>
					{subtitle ? (
						<div
							className={cn("truncate text-xs text-muted-foreground", monoSubtitle && "font-mono")}
						>
							{subtitle}
						</div>
					) : null}
				</div>
				{action ? <div className="-mr-1 shrink-0 self-start">{action}</div> : null}
			</div>

			<div className="px-4 pt-5 pb-4">
				<ResourceFlow stages={stages} />
				{error ? (
					<p
						className="mt-3 flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400"
						title={error}
					>
						<CircleAlert className="size-3.5 shrink-0" />
						<span className="truncate">{error}</span>
					</p>
				) : null}
			</div>

			<div className="mt-auto flex min-h-10 items-center justify-between gap-3 border-t bg-muted/30 px-4 py-2 text-xs text-muted-foreground">
				<div className="flex min-w-0 items-center gap-1.5">{meta}</div>
				{time !== undefined ? (
					<span
						className="shrink-0 tabular-nums"
						title={time ? new Date(time).toLocaleString() : undefined}
					>
						{time ? formatRelativeTime(time) : m.never()}
					</span>
				) : null}
			</div>
		</Card>
	);
}
