import { FileCode } from "lucide-react";
import { m } from "@/lib/paraglide/messages";

// ManagedSettingHint marks a setting that comes from x-orcacd in the compose
// file, which can only be changed there.
export function ManagedSettingHint() {
	return (
		<p className="flex items-center gap-1.5 text-xs text-muted-foreground">
			<FileCode className="size-3.5 shrink-0" />
			<span>
				{m.managedByComposeFile()} <code className="font-mono text-foreground">x-orcacd</code>
			</span>
		</p>
	);
}
