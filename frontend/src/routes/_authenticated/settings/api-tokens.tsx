import { createFileRoute } from "@tanstack/react-router";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/components/ui/toast";
import { ApiTokensTable } from "@/components/api-tokens-table";
import CreateApiTokenDialog from "@/components/dialogs/create-api-token-dialog";
import { useFetch } from "@/lib/api";
import { apiTokensPath, revokeApiToken, type ApiToken } from "@/lib/api-tokens";
import { usePermissions } from "@/lib/permissions";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/settings/api-tokens")({
	component: ApiTokensSettingsPage,
	head: () => ({
		meta: [
			{
				title: m.settingsApiTokensHeadTitle(),
			},
		],
	}),
});

function ApiTokensSettingsPage() {
	const { can } = usePermissions();
	const { data: tokens, error, isLoading } = useFetch<ApiToken[]>(apiTokensPath);

	async function handleRevoke(token: ApiToken) {
		try {
			await revokeApiToken(token.id);
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
			<CardHeader className="flex flex-col gap-3 sm:grid">
				<CardTitle>{m.apiTokens()}</CardTitle>
				<CardDescription>
					{m.apiTokensDescription()}
					{!can("api_token", "create") && <> {m.apiTokensCreateNotAllowed()}</>}
				</CardDescription>
				{can("api_token", "create") && (
					<CardAction>
						<CreateApiTokenDialog />
					</CardAction>
				)}
			</CardHeader>
			<ApiTokensTable tokens={tokens} error={error} isLoading={isLoading} onRevoke={handleRevoke} />
		</Card>
	);
}
