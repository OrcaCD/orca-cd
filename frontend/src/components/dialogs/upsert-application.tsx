import { Plus } from "lucide-react";
import { Button } from "../ui/button";

export default function UpsertApplicationDialog() {
	return (
		<Button>
			<Plus className="h-4 w-4" />
			New Application
		</Button>
	);
}
