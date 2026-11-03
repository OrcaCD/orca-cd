import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertTriangleIcon } from "lucide-react";
import type { ReactNode } from "react";

export default function WarningAlert({
	title,
	children,
}: {
	title: string;
	children: ReactNode;
}) {
	return (
		<Alert className="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
			<AlertTriangleIcon />
			<AlertTitle>{title}</AlertTitle>
			<AlertDescription className="text-foreground text-wrap">{children}</AlertDescription>
		</Alert>
	);
}
