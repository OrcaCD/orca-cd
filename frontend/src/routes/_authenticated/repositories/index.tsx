import { columns } from "@/components/tables/repositories/columns";
import { RepositoryDataTable } from "@/components/tables/repositories/data-table";
import UpsertRepositoryDialog from "@/components/dialogs/upsert-repository";
import type { Repository } from "@/lib/repsitories";
import { createFileRoute } from "@tanstack/react-router";
import { useFetch } from "@/lib/api";

export const Route = createFileRoute("/_authenticated/repositories/")({
	component: RepositoriesPage,
	head: () => ({
		meta: [
			{
				title: "Repositories",
			},
		],
	}),
});

function RepositoriesPage() {
	const { data: repos, isLoading } = useFetch<Repository[]>("/repositories");

	return (
		<div className="p-6 space-y-6">
			<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div>
					<h1 className="text-2xl font-bold">Repositories</h1>
					<p className="text-muted-foreground text-sm mt-1">Manage connected Git repositories</p>
				</div>
				<UpsertRepositoryDialog />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">Loading repositories...</p>}

			{repos && <RepositoryDataTable columns={columns} data={repos} />}
		</div>
	);
}
