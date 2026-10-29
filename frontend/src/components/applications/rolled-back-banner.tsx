import { RotateCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import {
	deployApplication,
	resumeApplicationAutoSync,
	SyncStatus,
	type Application,
} from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

function shortCommit(commit: string | null | undefined): string {
	return commit ? commit.slice(0, 7) : "—";
}

// RolledBackBanner explains that an application runs an earlier deployment
// than its commit in Git, and offers to deploy the commit again.
export function RolledBackBanner({ application }: { application: Application }) {
	const [deploying, setDeploying] = useState(false);
	const [resuming, setResuming] = useState(false);

	if (!application.rolledBackAt) {
		return null;
	}

	const desired = shortCommit(application.commit);
	const deploymentInProgress = deploying || application.syncStatus === SyncStatus.Syncing;

	const handleRetry = async () => {
		setDeploying(true);
		try {
			await deployApplication(application.id);
			toast.add({ type: "success", title: m.deploymentStarted() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeployApplication(),
			});
		} finally {
			setDeploying(false);
		}
	};

	const handleResume = async () => {
		setResuming(true);
		try {
			await resumeApplicationAutoSync(application.id);
			toast.add({ type: "success", title: m.autoSyncResumed() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedResumeAutoSync(),
			});
		} finally {
			setResuming(false);
		}
	};

	return (
		<div
			role="status"
			className="flex flex-col gap-4 rounded-xl border border-amber-500/40 bg-amber-500/5 p-4 sm:flex-row sm:items-center sm:justify-between"
		>
			<div className="min-w-0 space-y-1">
				<p className="flex items-center gap-2 font-semibold text-amber-500">
					<TriangleAlert className="size-4 shrink-0" />
					{m.rolledBackTitle()}
				</p>
				<p className="text-sm text-muted-foreground">
					{m.rolledBackRunning({
						running: shortCommit(application.rolledBackCommit),
						desired,
					})}{" "}
					{application.autoSyncPaused
						? m.rolledBackAutoSyncPaused()
						: m.rolledBackAutoSyncSkips({ desired })}
				</p>
				{application.lastSyncError ? (
					<p
						className="line-clamp-2 text-xs text-muted-foreground"
						title={application.lastSyncError}
					>
						{m.rolledBackReason({ reason: application.lastSyncError })}
					</p>
				) : null}
			</div>
			<div className="flex shrink-0 flex-wrap gap-2">
				<Button variant="outline" disabled={deploymentInProgress} onClick={handleRetry}>
					<RotateCw className={cn(deploymentInProgress && "animate-spin")} />
					{m.retryDeploy()}
				</Button>
				{application.autoSyncPaused ? (
					<Button variant="outline" disabled={resuming} onClick={handleResume}>
						{m.resumeAutoSync()}
					</Button>
				) : null}
			</div>
		</div>
	);
}
