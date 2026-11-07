import { FlaskConicalIcon } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { m } from "@/lib/paraglide/messages";

export default function DemoBanner() {
	const { auth } = useAuth();

	if (!auth.profile?.demo) {
		return null;
	}

	return (
		<div className="flex items-center justify-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-1.5 text-center text-[0.8125rem] text-amber-900 dark:border-amber-400/15 dark:bg-[oklch(0.23_0.04_68)] dark:text-amber-100">
			<FlaskConicalIcon className="size-3.5 shrink-0" />
			<span>{m.demoBannerMessage()}</span>
		</div>
	);
}
