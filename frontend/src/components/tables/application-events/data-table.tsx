import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { ApplicationEvent } from "@/lib/application-events";
import { m } from "@/lib/paraglide/messages";

import { type ColumnDef, useTable } from "@tanstack/react-table";
import { Fragment } from "react";
import { applicationEventsTableFeatures } from "./table-features";

interface ApplicationEventsDataTableProps {
	columns: ColumnDef<typeof applicationEventsTableFeatures, ApplicationEvent>[];
	data: ApplicationEvent[];
}

export function ApplicationEventsDataTable({ columns, data }: ApplicationEventsDataTableProps) {
	const table = useTable({
		features: applicationEventsTableFeatures,
		data,
		columns,
		getRowCanExpand: (row) => Boolean(row.original.errorMessage),
	});

	return (
		<div className="overflow-hidden rounded-md border">
			<Table>
				<TableHeader>
					{table.getHeaderGroups().map((headerGroup) => (
						<TableRow key={headerGroup.id}>
							{headerGroup.headers.map((header) => (
								<TableHead key={header.id} className="px-4">
									{header.isPlaceholder ? null : <table.FlexRender header={header} />}
								</TableHead>
							))}
						</TableRow>
					))}
				</TableHeader>
				<TableBody>
					{table.getRowModel().rows?.length ? (
						table.getRowModel().rows.map((row) => (
							<Fragment key={row.id}>
								<TableRow data-state={row.getIsSelected() && "selected"}>
									{row.getVisibleCells().map((cell) => (
										<TableCell key={cell.id} className="px-4">
											<table.FlexRender cell={cell} />
										</TableCell>
									))}
								</TableRow>
								{row.getIsExpanded() && (
									<TableRow>
										<TableCell colSpan={row.getVisibleCells().length} className="px-4 py-3">
											<Alert variant="destructive">
												<AlertTitle>{m.applicationHistoryErrorDetails()}</AlertTitle>
												<AlertDescription className="font-mono text-xs whitespace-pre-wrap break-all">
													{row.original.errorMessage}
												</AlertDescription>
											</Alert>
										</TableCell>
									</TableRow>
								)}
							</Fragment>
						))
					) : (
						<TableRow>
							<TableCell colSpan={columns.length} className="h-24 text-center">
								{m.applicationHistoryEmpty()}
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>
		</div>
	);
}
