import { RotateCcw } from "lucide-react";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import type { ApplicationEvent } from "@/lib/application-events";
import { rollbackApplication } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";

// RollbackButton rolls the application back to the deployment of an event.
// It is disabled while a deployment or rollback of the application runs.
export function RollbackButton({
	applicationId,
	event,
	deploying,
}: {
	applicationId: string;
	event: ApplicationEvent;
	deploying: boolean;
}) {
	const commit = event.commitHash?.slice(0, 7) ?? "";
	const label = (
		<>
			<RotateCcw />
			{m.rollBack()}
		</>
	);

	if (deploying) {
		return (
			<Button variant="outline" size="sm" disabled title={m.rollbackDeploymentInProgress()}>
				{label}
			</Button>
		);
	}

	const handleRollback = async () => {
		if (!event.deploymentId) {
			return;
		}
		try {
			await rollbackApplication(applicationId, event.deploymentId);
			toast.add({ type: "success", title: m.rollbackStarted() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedRollbackApplication(),
			});
		}
	};

	return (
		<ConfirmationDialog
			onConfirm={handleRollback}
			title={m.rollbackConfirmTitle({ commit })}
			description={m.rollbackConfirmDescription()}
			confirmText={m.rollBack()}
			triggerText={label}
			triggerProps={{ variant: "outline", size: "sm" }}
		/>
	);
}
