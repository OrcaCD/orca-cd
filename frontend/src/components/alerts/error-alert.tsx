import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertTriangleIcon } from "lucide-react";

export default function ErrorAlert({
	title,
	description,
}: {
	title?: string | undefined;
	description: string | undefined;
}) {
	const desc = description
		? description.at(0)?.toUpperCase() + description.slice(1)
		: "An error occurred.";

	return (
		<Alert className="mt-2 border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950 dark:text-red-50">
			<AlertTriangleIcon />
			<AlertTitle>{title || "Error"}</AlertTitle>
			<AlertDescription className="text-foreground text-wrap">{desc}</AlertDescription>
		</Alert>
	);
}
