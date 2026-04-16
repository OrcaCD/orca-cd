import { Plus } from "lucide-react";
import { Button } from "../ui/button";
import type { Application } from "@/lib/applications";

export default function UpsertApplicationDialog({
	application,
	asDropdownItem = false,
}: {
	application: Application | null;
	asDropdownItem?: boolean;
}) {
	return (
		<Button>
			<Plus className="h-4 w-4" />
			New Application
		</Button>
		// oxlint-disable-next-line no-warning-comments
		// TODO: Implement dialog form for creating/editing applications
	);
}
