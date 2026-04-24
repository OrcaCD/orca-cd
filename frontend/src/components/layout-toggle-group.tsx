import { LayoutGrid, List } from "lucide-react";
import { ToggleGroup, ToggleGroupItem } from "./ui/toggle-group";
import { toPreferredLayout, type PreferredLayout } from "@/lib/layout-preference";

export function LayoutToggleGroup({
	viewMode,
	setViewMode,
}: {
	viewMode: string;
	setViewMode: (layout: PreferredLayout) => void;
}) {
	return (
		<ToggleGroup
			type="single"
			variant="outline"
			value={viewMode}
			onValueChange={(value) => {
				const nextLayout = toPreferredLayout(value);
				if (nextLayout) {
					setViewMode(nextLayout);
				}
			}}
		>
			<ToggleGroupItem value="grid">
				<LayoutGrid className="h-4 w-4" />
			</ToggleGroupItem>

			<ToggleGroupItem value="table">
				<List className="h-4 w-4" />
			</ToggleGroupItem>
		</ToggleGroup>
	);
}
