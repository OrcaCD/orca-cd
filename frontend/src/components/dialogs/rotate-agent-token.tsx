import { KeyRound } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { rotateAgentToken, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import ConfirmationDialog from "./confirm-dialog";
import CopyValueDialog from "./copy-value-dialog";

export default function RotateAgentTokenDialog({ agent }: { agent: Agent }) {
	const [authToken, setAuthToken] = useState<string | null>(null);
	const [isAuthTokenOpen, setIsAuthTokenOpen] = useState(false);

	async function handleRotateToken() {
		try {
			const response = await rotateAgentToken(agent.id);
			setAuthToken(response.authToken);
			setIsAuthTokenOpen(true);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedRotateAgentToken());
		}
	}

	return (
		<>
			<ConfirmationDialog
				onConfirm={handleRotateToken}
				title={m.rotateAgentTokenTitle()}
				description={m.rotateAgentTokenDescription({ name: agent.name })}
				confirmText={m.rotateToken()}
				triggerText={
					<>
						<KeyRound className="h-4 w-4" />
						{m.rotateToken()}
					</>
				}
				asDropdownItem
				dropdownItemVariant="default"
			/>

			<CopyValueDialog
				open={isAuthTokenOpen}
				onOpenChange={(nextOpen) => {
					setIsAuthTokenOpen(nextOpen);
					if (!nextOpen) {
						setAuthToken(null);
					}
				}}
				title={m.agentTokenRotated()}
				description={m.copyTokenNow()}
				label={m.authToken()}
				value={authToken ?? ""}
				inputId={`agent-auth-token-${agent.id}`}
				copyTitle={m.copyAgentAuthToken()}
			/>
		</>
	);
}
