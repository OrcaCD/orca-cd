import {
	type ColumnDef,
	type ColumnVisibilityState,
	type RowData,
	type SortingState,
	useTable,
} from "@tanstack/react-table";

import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { useState } from "react";
import { DataTableViewOptions } from "../data-table-view-options";
import { m } from "@/lib/paraglide/messages";
import { dataTableFeatures } from "../table-features";

interface AgentsDataTable<TData extends RowData> {
	columns: ColumnDef<typeof dataTableFeatures, TData>[];
	data: TData[];
}

export function AgentsDataTable<TData extends RowData>({ columns, data }: AgentsDataTable<TData>) {
	const [sorting, setSorting] = useState<SortingState>([]);
	const [columnVisibility, setColumnVisibility] = useState<ColumnVisibilityState>({
		createdAt: false,
		updatedAt: false,
	});

	const table = useTable({
		features: dataTableFeatures,
		data,
		columns,
		onSortingChange: setSorting,
		onColumnVisibilityChange: setColumnVisibility,
		state: {
			sorting,
			columnVisibility,
		},
	});

	return (
		<div>
			<div className="overflow-hidden rounded-md border">
				<Table>
					<TableHeader>
						{table.getHeaderGroups().map((headerGroup) => (
							<TableRow key={headerGroup.id}>
								{headerGroup.headers.map((header) => {
									return (
										<TableHead key={header.id}>
											{header.isPlaceholder ? null : <table.FlexRender header={header} />}
										</TableHead>
									);
								})}
							</TableRow>
						))}
					</TableHeader>
					<TableBody>
						{table.getRowModel().rows?.length ? (
							table.getRowModel().rows.map((row) => (
								<TableRow key={row.id} data-state={row.getIsSelected() && "selected"}>
									{row.getVisibleCells().map((cell) => (
										<TableCell key={cell.id} className="px-4">
											<table.FlexRender cell={cell} />
										</TableCell>
									))}
								</TableRow>
							))
						) : (
							<TableRow>
								<TableCell colSpan={columns.length} className="h-24 text-center">
									{m.noAgentsFound()}.
								</TableCell>
							</TableRow>
						)}
					</TableBody>
				</Table>
			</div>
			<div className="flex items-center pt-6">
				<div className="text-sm ml-1 text-muted-foreground">
					{m.totalAgentsCount({ count: data.length })}
				</div>
				<DataTableViewOptions table={table} />
			</div>
		</div>
	);
}
