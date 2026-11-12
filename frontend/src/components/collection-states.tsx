import type { ComponentProps } from "react";
import { Card } from "@/components/ui/card";
import { Skeleton as BaseSkeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export { EmptyState } from "./empty-state";

// The default muted fill nearly vanishes on cards in the dark theme, a foreground tint reads in both.
function Skeleton({ className, ...props }: ComponentProps<typeof BaseSkeleton>) {
	return (
		<BaseSkeleton
			className={cn("bg-foreground/8 motion-reduce:animate-none", className)}
			{...props}
		/>
	);
}

/** Loading placeholder shaped like a `ResourceCard`, so the grid keeps its layout once data arrives. */
type ResourceKind = "application" | "agent" | "repository" | "notification";

function ResourceCardSkeleton({ kind }: { kind: ResourceKind }) {
	return (
		<Card className="gap-0 py-0">
			<div className="flex items-center gap-3 px-4 pt-4">
				<Skeleton className="size-10 shrink-0 rounded-lg" />
				<div className="flex min-w-0 flex-1 flex-col gap-1.5">
					<Skeleton className="h-4 w-2/5" />
					<Skeleton className="h-3 w-3/5" />
				</div>
			</div>
			{kind === "application" ? (
				<div className="px-4 pt-5 pb-4">
					<div className="flex h-3 items-center gap-1.5">
						<Skeleton className="size-3 rounded-full" />
						<Skeleton className="h-px flex-1 rounded-none" />
						<Skeleton className="size-3 rounded-full" />
						<Skeleton className="h-px flex-1 rounded-none" />
						<Skeleton className="size-3 rounded-full" />
					</div>
					<div className="mt-2.5 grid grid-cols-3 gap-3">
						{[0, 1, 2].map((index) => (
							<div
								key={index}
								className={cn(
									"flex flex-col gap-1.5",
									index === 1 && "items-center",
									index === 2 && "items-end",
								)}
							>
								<Skeleton className="mt-0.5 h-2.5 w-12" />
								<Skeleton className="h-4 w-16" />
							</div>
						))}
					</div>
				</div>
			) : (
				<div className="flex flex-col gap-3 px-4 pt-5 pb-4">
					{kind === "repository" ? (
						<div className="flex flex-col gap-3 rounded-lg border p-3">
							<div className="flex items-center gap-2">
								<Skeleton className="size-4 rounded-full" />
								<Skeleton className="h-4 w-24" />
								<Skeleton className="ml-auto h-3 w-16" />
							</div>
							<div className="flex items-center justify-between border-t pt-3">
								<Skeleton className="h-3 w-28" />
								<Skeleton className="h-6 w-14" />
							</div>
						</div>
					) : (
						<>
							{kind === "agent" ? <Skeleton className="h-2.5 w-20" /> : null}
							{Array.from({ length: kind === "agent" ? 2 : 3 }, (_, index) => (
								<div key={index} className="flex h-5 items-center gap-2.5">
									<Skeleton className="size-4 rounded-sm" />
									<Skeleton className={cn("h-3", index % 2 ? "w-1/3" : "w-2/5")} />
									<Skeleton className="ml-auto h-3 w-14" />
								</div>
							))}
						</>
					)}
				</div>
			)}
			<div className="flex min-h-10 items-center justify-between gap-3 border-t bg-muted/40 px-4 py-2">
				<Skeleton className="h-3 w-24" />
				<Skeleton className="h-3 w-16" />
			</div>
		</Card>
	);
}

export function ResourceGridSkeleton({
	label,
	count = 3,
	kind = "application",
	className,
}: {
	/** Announced to screen readers while loading. */
	label: string;
	count?: number;
	kind?: ResourceKind;
	className?: string;
}) {
	return (
		<div role="status" className={cn("grid gap-4", className)}>
			<span className="sr-only">{label}</span>
			{Array.from({ length: count }, (_, index) => (
				<ResourceCardSkeleton key={index} kind={kind} />
			))}
		</div>
	);
}

// Column widths of the table skeleton; header and rows share them so the columns line up.
const tableColumns = ["w-40", "w-28", "w-32", "w-24"];

export function ResourceTableSkeleton({ label, rows = 4 }: { label: string; rows?: number }) {
	return (
		<div role="status" className="overflow-hidden rounded-xl border bg-card">
			<span className="sr-only">{label}</span>
			{Array.from({ length: rows + 1 }, (_, row) => (
				<div
					key={row}
					className={cn(
						"flex items-center gap-6 border-b px-4 last:border-b-0",
						row === 0 ? "h-10" : "h-12",
					)}
				>
					{tableColumns.map((width, column) => (
						<div key={width} className={cn("shrink-0", width, column > 1 && "max-sm:hidden")}>
							<Skeleton
								className={cn(row === 0 ? "h-3 w-1/2" : row % 2 ? "h-3.5 w-4/5" : "h-3.5 w-3/5")}
							/>
						</div>
					))}
				</div>
			))}
		</div>
	);
}

/** Placeholder for pages made of stacked cards, such as settings sections. */
export function CardStackSkeleton({
	label,
	heights,
	className,
}: {
	/** Announced to screen readers; leave it out on all but one of several placeholders. */
	label?: string;
	/** Height class of each placeholder card, in order. */
	heights: string[];
	className?: string;
}) {
	return (
		<div
			role={label ? "status" : undefined}
			aria-hidden={label ? undefined : true}
			className={cn("grid gap-6", className)}
		>
			{label ? <span className="sr-only">{label}</span> : null}
			{heights.map((height, index) => (
				<Skeleton key={index} className={cn("w-full rounded-xl", height)} />
			))}
		</div>
	);
}
