import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { CheckCircle2Icon } from "lucide-react";

export default function SuccessAlert({
	title,
	description,
}: {
	title?: string | undefined;
	description: string | undefined;
}) {
	const desc = description
		? description.at(0)?.toUpperCase() + description.slice(1)
		: "Operation successful.";

	return (
		<Alert className="mt-2 border-green-200 bg-green-50 text-green-900 dark:border-green-900 dark:bg-green-950 dark:text-green-50">
			<CheckCircle2Icon />
			<AlertTitle>{title || "Success"}</AlertTitle>
			<AlertDescription className="text-foreground text-wrap">{desc}</AlertDescription>
		</Alert>
	);
}
