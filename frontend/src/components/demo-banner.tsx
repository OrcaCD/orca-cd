import { FlaskConicalIcon } from "lucide-react";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";

export default function DemoBanner() {
	const { data } = useFetch<{ demo?: boolean }>("/auth/setup", {
		revalidateOnFocus: false,
	});

	if (!data?.demo) {
		return null;
	}

	return (
		<div className="flex items-center justify-center gap-2 border-b border-amber-200 bg-amber-50 px-4 py-2 text-center text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50">
			<FlaskConicalIcon className="size-4 shrink-0" />
			<span>{m.demoBannerMessage()}</span>
		</div>
	);
}
