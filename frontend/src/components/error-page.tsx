import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
	CodeBlock,
	CodeBlockActions,
	CodeBlockContent,
	CodeBlockCopyButton,
} from "@/components/ui/code-block";
import { m } from "@/lib/paraglide/messages";

interface ErrorPageProps {
	icon: ReactNode;
	code?: string;
	title: string;
	description: string;
	details?: string;
	onRetry?: () => void;
}

export function ErrorPage({ icon, code, title, description, details, onRetry }: ErrorPageProps) {
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const showHome = pathname !== "/" && pathname !== "/login";

	return (
		<div className="relative flex min-h-svh flex-col items-center justify-center overflow-hidden px-6 py-16 text-center">
			<div
				aria-hidden
				className="pointer-events-none absolute inset-0 hidden dark:block bg-[radial-gradient(ellipse_at_center,var(--color-primary)_0%,transparent_60%)] opacity-10"
			/>
			<div className="relative flex w-full max-w-md flex-col items-center gap-6">
				<div className="text-foreground [&_svg]:size-14 [&_svg]:stroke-[1.5]">{icon}</div>
				<div className="flex flex-col gap-2">
					{code && (
						<span className="text-sm font-semibold tracking-widest text-primary">{code}</span>
					)}
					<h1 className="text-3xl font-bold tracking-tight text-balance">{title}</h1>
					<p className="text-balance text-muted-foreground">{description}</p>
				</div>
				{details && (
					<CodeBlock size="sm" className="max-h-40 text-left">
						<CodeBlockActions>
							<CodeBlockCopyButton value={details} />
						</CodeBlockActions>
						<CodeBlockContent wrap>{details}</CodeBlockContent>
					</CodeBlock>
				)}
				<div className="flex flex-wrap justify-center gap-2">
					{onRetry && (
						<Button size="lg" variant="outline" onClick={onRetry}>
							{m.tryAgain()}
						</Button>
					)}
					{showHome && (
						<Button size="lg" nativeButton={false} render={<Link to="/">{m.goHome()}</Link>} />
					)}
				</div>
			</div>
		</div>
	);
}
