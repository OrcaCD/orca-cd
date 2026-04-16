import { General } from "@/components/application/settings/general";
import { Source } from "@/components/application/settings/source";
import { Sync } from "@/components/application/settings/sync";
import { Danger } from "@/components/application/settings/danger";

export function MainContent({ activeSection }: { activeSection: string }) {
	return (
		<div className="flex-1 space-y-6">
			<General activeSection={activeSection} />
			<Source activeSection={activeSection} />
			<Sync activeSection={activeSection} />
			<Danger activeSection={activeSection} />
		</div>
	);
}
