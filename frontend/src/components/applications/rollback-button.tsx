import { RotateCcw } from "lucide-react";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { toast } from "@/components/ui/toast";
import type { ApplicationEvent } from "@/lib/application-events";
import { rollbackApplication } from "@/lib/applications";
import { m } from "@/lib/paraglide/messages";

// RollbackButton rolls the application back to the deployment of an event.
export function RollbackButton({
	applicationId,
	event,
}: {
	applicationId: string;
	event: ApplicationEvent;
}) {
	const commit = event.commitHash?.slice(0, 7) ?? "";

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
			triggerText={
				<>
					<RotateCcw />
					{m.rollBack()}
				</>
			}
			triggerProps={{ variant: "outline", size: "sm" }}
		/>
	);
}
