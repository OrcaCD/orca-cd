import { type Column, type RowData } from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown, EyeOff } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { m } from "@/lib/paraglide/messages";
import { dataTableFeatures } from "./table-features";

interface DataTableColumnHeaderProps<
	TData extends RowData,
	TValue,
> extends React.HTMLAttributes<HTMLDivElement> {
	column: Column<typeof dataTableFeatures, TData, TValue>;
	title: string;
}

export function DataTableColumnHeader<TData extends RowData, TValue>({
	column,
	title,
	className,
}: DataTableColumnHeaderProps<TData, TValue>) {
	if (!column.getCanSort()) {
		return <div className={cn(className)}>{title}</div>;
	}

	return (
		<div className={cn("flex items-center gap-2", className)}>
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button variant="ghost" size="sm" className="-ml-1 h-8 data-[state=open]:bg-accent">
							<span className="font-semibold">{title}</span>
							{column.getIsSorted() === "desc" ? (
								<ArrowDown />
							) : column.getIsSorted() === "asc" ? (
								<ArrowUp />
							) : (
								<ChevronsUpDown />
							)}
						</Button>
					}
				></DropdownMenuTrigger>
				<DropdownMenuContent align="start">
					<DropdownMenuGroup>
						<DropdownMenuItem onClick={() => column.toggleSorting(false)}>
							<ArrowUp />
							{m.sortAscending()}
						</DropdownMenuItem>
						<DropdownMenuItem onClick={() => column.toggleSorting(true)}>
							<ArrowDown />
							{m.sortDescending()}
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem onClick={() => column.toggleVisibility(false)}>
							<EyeOff />
							{m.hideColumn()}
						</DropdownMenuItem>
					</DropdownMenuGroup>
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	);
}
