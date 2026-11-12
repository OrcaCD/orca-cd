import type { ReactNode } from "react";
import { SearchX, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
	title: string;
	description: string;
	/** Icon of the resource. Searches that match nothing always show a search icon. */
	icon?: LucideIcon;
	/** `empty` when there is nothing yet, `noMatches` when a search or filter hides everything. */
	variant?: "empty" | "noMatches";
	action?: ReactNode;
	className?: string;
}

/**
 * Placeholder of a resource list without entries. A list that is actually empty invites to create
 * the first entry, a search without matches offers to reset it.
 */
export function EmptyState({
	title,
	description,
	icon,
	variant = "empty",
	action,
	className,
}: EmptyStateProps) {
	const Icon = variant === "noMatches" || !icon ? SearchX : icon;

	return (
		<div
			className={cn(
				"flex flex-col items-center rounded-xl border px-6 py-12 text-center sm:py-14",
				variant === "empty" ? "border-dashed bg-card/40" : "bg-card/60",
				className,
			)}
		>
			<div
				className={cn(
					"flex size-11 items-center justify-center rounded-xl ring-1 ring-inset",
					variant === "empty"
						? "bg-primary/10 text-primary ring-primary/20"
						: "bg-muted text-muted-foreground ring-foreground/5",
				)}
			>
				<Icon className="size-5" aria-hidden="true" />
			</div>
			<h2 className="mt-4 text-[0.9375rem] font-semibold tracking-tight">{title}</h2>
			<p className="mt-1 max-w-sm text-sm text-pretty text-muted-foreground">{description}</p>
			{action ? <div className="mt-5 flex flex-wrap justify-center gap-2">{action}</div> : null}
		</div>
	);
}
