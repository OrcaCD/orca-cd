import { ListTree, Table2 } from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { toHistoryLayout, type HistoryLayout } from "@/lib/history-layout-preference";
import { m } from "@/lib/paraglide/messages";

export function HistoryLayoutToggle({
	layout,
	setLayout,
}: {
	layout: HistoryLayout;
	setLayout: (layout: HistoryLayout) => void;
}) {
	return (
		<ToggleGroup
			variant="outline"
			spacing={0}
			value={[layout]}
			onValueChange={(value) => {
				const nextLayout = toHistoryLayout(value[0]);
				if (nextLayout) {
					setLayout(nextLayout);
				}
			}}
		>
			<ToggleGroupItem value="timeline" aria-label={m.applicationHistoryViewTimeline()}>
				<ListTree className="h-4 w-4" />
			</ToggleGroupItem>

			<ToggleGroupItem value="table" aria-label={m.applicationHistoryViewTable()}>
				<Table2 className="h-4 w-4" />
			</ToggleGroupItem>
		</ToggleGroup>
	);
}
