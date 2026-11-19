import { createFileRoute } from "@tanstack/react-router";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/components/ui/toast";
import { ApiTokensTable } from "@/components/api-tokens-table";
import { useFetch } from "@/lib/api";
import { adminApiTokensPath, adminRevokeApiToken, type ApiToken } from "@/lib/api-tokens";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/admin/api-tokens")({
	component: AdminApiTokensPage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.apiTokens()}`,
			},
		],
	}),
});

function AdminApiTokensPage() {
	const { data: tokens, error, isLoading } = useFetch<ApiToken[]>(adminApiTokensPath);

	async function handleRevoke(token: ApiToken) {
		try {
			await adminRevokeApiToken(token.id);
			toast.add({ type: "success", title: m.apiTokenRevoked() });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedRevokeApiToken(),
			});
		}
	}

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.apiTokens()}</CardTitle>
				<CardDescription>{m.adminApiTokensDescription()}</CardDescription>
			</CardHeader>
			<ApiTokensTable
				tokens={tokens}
				error={error}
				isLoading={isLoading}
				onRevoke={handleRevoke}
				showOwner
			/>
		</Card>
	);
}
