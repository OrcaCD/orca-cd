import { createFileRoute } from "@tanstack/react-router";
import { FileCode, LoaderCircle, TriangleAlert } from "lucide-react";
import { useMemo, useState } from "react";
import { EnvFileCard } from "@/components/applications/env/env-file-card";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import {
	CodeBlock,
	CodeBlockActions,
	CodeBlockContent,
	CodeBlockCopyButton,
	CodeBlockHeader,
	CodeBlockLine,
	CodeBlockTitle,
} from "@/components/ui/code-block";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/toast";
import { useHighlightedCode } from "@/hooks/use-highlighted-code";
import { useFetch } from "@/lib/api";
import type { Application } from "@/lib/applications";
import {
	buildEnvFileExample,
	buildEnvRows,
	buildEnvUpdate,
	countEnvDrafts,
	isEnvConflict,
	isEnvSavedButRedeployFailed,
	setEnvDraft,
	updateApplicationEnv,
	type EnvDrafts,
	type EnvListResponse,
} from "@/lib/application-env";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/applications/$id/settings/environment")({
	component: EnvironmentPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.envTitle()}`,
			},
		],
	}),
});

function NoEnvFilesCard({ id }: { id: string }) {
	const { data: application } = useFetch<Application>(`/applications/${id}`);
	const composeFile = application?.composeFile ?? "";
	const example = useMemo(() => buildEnvFileExample(composeFile), [composeFile]);
	const content = example.lines.join("\n");
	const lines = useHighlightedCode(content, "yaml");

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.envNoFilesTitle()}</CardTitle>
				<CardDescription>{m.envNoFilesDescription()}</CardDescription>
			</CardHeader>
			<Separator />
			<CardContent>
				<CodeBlock>
					<CodeBlockHeader>
						<CodeBlockTitle>
							<FileCode />
							{application?.path || "compose.yaml"}
						</CodeBlockTitle>
						<CodeBlockActions>
							<CodeBlockCopyButton value={`${content}\n`} />
						</CodeBlockActions>
					</CodeBlockHeader>
					<CodeBlockContent>
						{lines.map((line, index) => {
							const isAdded = index === example.addedIndex;
							return (
								<CodeBlockLine
									key={index}
									variant={isAdded ? "added" : "default"}
									className={
										isAdded
											? "bg-green-500/15 after:text-green-600 dark:after:text-green-400"
											: undefined
									}
								>
									{line}
								</CodeBlockLine>
							);
						})}
					</CodeBlockContent>
				</CodeBlock>
			</CardContent>
		</Card>
	);
}

function EnvSkeleton() {
	return (
		<Card aria-busy>
			<CardHeader>
				<Skeleton className="h-5 w-32" />
				<Skeleton className="h-4 w-56" />
			</CardHeader>
			<Separator />
			<div className="flex flex-col gap-3 px-(--card-spacing)">
				{[0, 1, 2].map((row) => (
					<div key={row} className="flex items-center gap-4">
						<Skeleton className="h-8 w-40" />
						<Skeleton className="h-8 flex-1" />
						<Skeleton className="size-8" />
					</div>
				))}
			</div>
		</Card>
	);
}

function EnvironmentPage() {
	const { id } = Route.useParams();
	// Revalidating while the user edits would only hand out revisions that conflict with the form state
	const {
		data,
		error,
		isLoading,
		isValidating,
		mutate: reload,
	} = useFetch<EnvListResponse>(`/applications/${id}/env`, {
		revalidateOnFocus: false,
		revalidateOnReconnect: false,
		shouldRetryOnError: false,
	});

	const [drafts, setDrafts] = useState<EnvDrafts>({});
	const [isSaving, setIsSaving] = useState(false);
	const [conflictOpen, setConflictOpen] = useState(false);

	const isBusy = isSaving || isValidating;
	const files = useMemo(() => data?.files ?? [], [data]);
	const changeCount = countEnvDrafts(drafts);
	const rowsByPath = useMemo(
		() => new Map(files.map((file) => [file.path, buildEnvRows(file, drafts[file.path])])),
		[files, drafts],
	);
	const hasInvalidValue = [...rowsByPath.values()].some((rows) =>
		rows.some((row) => row.valueError !== null),
	);

	async function handleReload() {
		setDrafts({});
		setConflictOpen(false);
		await reload();
	}

	async function handleSave() {
		setIsSaving(true);
		try {
			await updateApplicationEnv(id, buildEnvUpdate(files, drafts));
			toast.add({ type: "success", title: m.envSavedToast() });
			await handleReload();
		} catch (err) {
			if (isEnvConflict(err)) {
				setConflictOpen(true);
			} else {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.envSaveFailed(),
				});
				// The values were written, so the local edits are obsolete
				if (isEnvSavedButRedeployFailed(err)) {
					await handleReload();
				}
			}
		} finally {
			setIsSaving(false);
		}
	}

	return (
		<div className="flex flex-col gap-6">
			{isLoading ? (
				<EnvSkeleton />
			) : error ? (
				<Alert
					variant="destructive"
					className="items-center *:[svg]:row-span-1 *:[svg]:translate-y-0"
				>
					<TriangleAlert />
					<AlertDescription className="flex flex-wrap items-center justify-between gap-2">
						<span>{error instanceof Error ? error.message : m.envLoadFailed()}</span>
						<Button variant="outline" size="sm" onClick={handleReload} disabled={isValidating}>
							{m.envReload()}
						</Button>
					</AlertDescription>
				</Alert>
			) : files.length === 0 ? (
				<NoEnvFilesCard id={id} />
			) : (
				<>
					<div
						aria-busy={isBusy}
						className={cn(
							"flex flex-col gap-6 transition-opacity",
							isBusy && "pointer-events-none opacity-60",
						)}
					>
						{files.map((file) => (
							<EnvFileCard
								key={file.path}
								file={file}
								rows={rowsByPath.get(file.path) ?? []}
								disabled={isBusy}
								onDraftChange={(key, entry, draft) =>
									setDrafts((current) => setEnvDraft(current, file.path, key, entry, draft))
								}
							/>
						))}
					</div>

					<div className="flex flex-wrap items-center justify-end gap-3">
						{changeCount > 0 && (
							<span className="text-sm text-muted-foreground">
								{m.envUnsavedChanges({ count: changeCount })}
							</span>
						)}
						<Button variant="outline" onClick={handleReload} disabled={isBusy || changeCount === 0}>
							{m.envDiscardChanges()}
						</Button>
						<Button onClick={handleSave} disabled={isBusy || changeCount === 0 || hasInvalidValue}>
							{isSaving && <LoaderCircle className="motion-safe:animate-spin" />}
							{isSaving ? m.envSavingAndRedeploying() : m.envSaveAndRedeploy()}
						</Button>
					</div>
				</>
			)}

			<Dialog
				open={conflictOpen}
				onOpenChange={(open) => {
					// Dismissing must not keep the stale revisions
					if (!open) {
						void handleReload();
					}
				}}
			>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle className="flex items-center gap-2">
							<TriangleAlert className="size-5 text-destructive" />
							{m.envConflictTitle()}
						</DialogTitle>
						<DialogDescription>{m.envConflictDescription()}</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button onClick={handleReload}>{m.envReload()}</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
