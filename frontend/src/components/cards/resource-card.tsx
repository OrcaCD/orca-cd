import type { ReactNode } from "react";
import { CircleAlert } from "lucide-react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

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
	unknown: "border-zinc-500",
	info: "border-primary bg-card",
	off: "border-zinc-500 bg-zinc-500/25",
};

const segmentClass: Record<FlowState, string> = {
	ok: "border-emerald-500/60",
	progress: "",
	warning: "border-amber-500/60",
	error: "border-red-500/60",
	unknown: "border-dashed border-zinc-400 dark:border-zinc-500/70",
	info: "border-foreground/20",
	off: "border-dashed border-zinc-400 dark:border-zinc-500/70",
};

const valueClass: Record<FlowState, string> = {
	ok: "",
	progress: "text-blue-600 dark:text-blue-400",
	warning: "text-amber-700 dark:text-amber-400",
	error: "text-red-600 dark:text-red-400",
	unknown: "text-muted-foreground",
	info: "",
	off: "text-muted-foreground",
};

const brokenStates = new Set<FlowState>(["error", "unknown", "off"]);

function FlowSegment({ state, interrupted }: { state: FlowState; interrupted: boolean }) {
	if (interrupted) {
		return (
			<span className="mx-1.5 flex-1 border-t border-dashed border-zinc-400 dark:border-zinc-500/50" />
		);
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
export function ResourceFlow({ stages }: { stages: [FlowStage, FlowStage, FlowStage] }) {
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
						<dt className="font-mono text-[0.625rem] font-medium tracking-[0.12em] text-muted-foreground uppercase">
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

export function CardError({ message, className }: { message: string; className?: string }) {
	return (
		<p
			className={cn(
				"flex items-start gap-1.5 text-xs leading-4 text-red-600 dark:text-red-400",
				className,
			)}
			title={message}
		>
			<CircleAlert className="mt-px size-3.5 shrink-0" />
			<span className="line-clamp-2 wrap-break-word">{message}</span>
		</p>
	);
}

interface ResourceCardProps {
	icon: ReactNode;
	title: string;
	subtitle?: ReactNode;
	monoSubtitle?: boolean;
	action?: ReactNode;
	children: ReactNode;
	footer: ReactNode;
	className?: string;
}

/** Shared shell of the resource cards: identity header, resource specific body and a footer bar. */
export function ResourceCard({
	icon,
	title,
	subtitle,
	monoSubtitle = false,
	action,
	children,
	footer,
	className,
}: ResourceCardProps) {
	return (
		<Card
			className={cn(
				"h-full gap-0 py-0 transition-[box-shadow] duration-200 hover:ring-primary/40 hover:shadow-[0_12px_32px_-16px_oklch(0.3_0.08_250/35%)]",
				className,
			)}
		>
			<div className="flex items-center gap-3 px-4 pt-4">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted ring-1 ring-foreground/5 ring-inset">
					{icon}
				</div>
				<div className="min-w-0 flex-1">
					<h3
						className="truncate text-[0.9375rem] leading-snug font-semibold tracking-tight"
						title={title}
					>
						{title}
					</h3>
					{subtitle ? (
						<div
							className={cn(
								"mt-0.5 truncate text-xs text-muted-foreground",
								monoSubtitle && "font-mono text-[0.6875rem]",
							)}
						>
							{subtitle}
						</div>
					) : null}
				</div>
				{action ? <div className="-mr-1 shrink-0 self-start">{action}</div> : null}
			</div>

			<div className="flex-1 px-4 pt-5 pb-4">{children}</div>

			<div className="flex min-h-10 items-center justify-between gap-3 border-t px-4 py-2 text-xs text-muted-foreground dark:bg-muted/40">
				{footer}
			</div>
		</Card>
	);
}
