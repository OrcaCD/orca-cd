import {
	columnVisibilityFeature,
	createExpandedRowModel,
	rowExpandingFeature,
	rowSelectionFeature,
	tableFeatures,
} from "@tanstack/react-table";

export const applicationEventsTableFeatures = tableFeatures({
	columnVisibilityFeature,
	rowExpandingFeature,
	rowSelectionFeature,
	expandedRowModel: createExpandedRowModel(),
});
