import {
	type ColumnDef,
	type ColumnFiltersState,
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
import { Input } from "@/components/ui/input";
import { DataTableViewOptions } from "../data-table-view-options";
import { Search } from "lucide-react";
import { toSearchableText } from "@/lib/utils";
import { m } from "@/lib/paraglide/messages";
import UpsertUserDialog from "@/components/dialogs/upsert-user-dialog";
import { dataTableFeatures } from "../table-features";

interface RepositoryDataTable<TData extends RowData> {
	columns: ColumnDef<typeof dataTableFeatures, TData>[];
	data: TData[];
}

export function UserDataTable<TData extends RowData>({
	columns,
	data,
}: RepositoryDataTable<TData>) {
	const [sorting, setSorting] = useState<SortingState>([]);
	const [globalFilter, setGlobalFilter] = useState("");
	const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
	const [columnVisibility, setColumnVisibility] = useState<ColumnVisibilityState>({});

	const table = useTable({
		features: dataTableFeatures,
		data,
		columns,
		onSortingChange: setSorting,
		onGlobalFilterChange: setGlobalFilter,
		getColumnCanGlobalFilter: () => true,
		globalFilterFn: (row, columnId, filterValue) => {
			const query = String(filterValue).trim().toLowerCase();

			if (!query) {
				return true;
			}

			return toSearchableText(row.getValue(columnId)).includes(query);
		},
		onColumnFiltersChange: setColumnFilters,
		onColumnVisibilityChange: setColumnVisibility,
		state: {
			sorting,
			globalFilter,
			columnFilters,
			columnVisibility,
		},
	});

	return (
		<div>
			<div className="flex items-center pb-6">
				<div className="relative w-md">
					<Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
					<Input
						placeholder={m.searchUsers()}
						className="pl-9 bg-muted border-border"
						value={globalFilter}
						onChange={(event) => setGlobalFilter(event.target.value)}
					/>
				</div>

				<DataTableViewOptions table={table} />

				<div className="mx-2"></div>

				<UpsertUserDialog user={null} />
			</div>
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
									{m.noResults()}
								</TableCell>
							</TableRow>
						)}
					</TableBody>
				</Table>
			</div>
		</div>
	);
}
