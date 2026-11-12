import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Content column of the top level pages. Detail pages bring their own sidebar layout. */
export function PageContainer({
	children,
	className,
}: {
	children: ReactNode;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"mx-auto flex w-full max-w-screen-2xl flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8 lg:py-7",
				className,
			)}
		>
			{children}
		</div>
	);
}

interface PageHeaderProps {
	title: ReactNode;
	description?: ReactNode;
	actions?: ReactNode;
	className?: string;
}

export function PageHeader({ title, description, actions, className }: PageHeaderProps) {
	return (
		<div
			className={cn(
				"flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between",
				className,
			)}
		>
			<div className="flex min-w-0 flex-col gap-1">
				<h1 className="text-2xl leading-8 font-semibold tracking-tight">{title}</h1>
				{description ? (
					<p className="max-w-2xl text-sm text-pretty text-muted-foreground">{description}</p>
				) : null}
			</div>
			{actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
		</div>
	);
}
