import { createFileRoute } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { toast } from "@/components/ui/toast";
import { CardStackSkeleton } from "@/components/collection-states";
import ErrorAlert from "@/components/alerts/error-alert";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import CreateApiTokenDialog from "@/components/dialogs/create-api-token-dialog";
import { RelativeTime } from "@/components/relative-time";
import { useNow } from "@/hooks/use-now";
import { useFetch } from "@/lib/api";
import {
	apiTokensPath,
	parseApiTokenScope,
	revokeApiToken,
	type ApiToken,
	type ApiTokenScope,
} from "@/lib/api-tokens";
import { usePermissions } from "@/lib/permissions";
import { actionLabels, resourceLabels } from "@/lib/role-draft";
import { m } from "@/lib/paraglide/messages";
import { getLocale } from "@/lib/paraglide/runtime";

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

function scopeLabel(scope: ApiTokenScope): string {
	const permission = parseApiTokenScope(scope);
	if (!permission) {
		return m.apiTokenScopeAdmin();
	}
	return `${resourceLabels[permission.resource]()}: ${actionLabels[permission.action]()}`;
}

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
			<CardHeader>
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
			<CardContent>
				{isLoading && <CardStackSkeleton label={m.loadingApiTokens()} heights={["h-24"]} />}
				{error && (
					<ErrorAlert title={m.apiTokensLoadFailed()} description={(error as Error).message} />
				)}
				{tokens && tokens.length === 0 && (
					<p className="py-6 text-center text-sm text-muted-foreground">{m.noApiTokens()}</p>
				)}
				{tokens && tokens.length > 0 && (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{m.name()}</TableHead>
								<TableHead>{m.apiTokenScopes()}</TableHead>
								<TableHead>{m.apiTokenExpires()}</TableHead>
								<TableHead>{m.apiTokenLastUsed()}</TableHead>
								<TableHead>
									<span className="sr-only">{m.actions()}</span>
								</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{tokens.map((token) => (
								<ApiTokenRow key={token.id} token={token} onRevoke={() => handleRevoke(token)} />
							))}
						</TableBody>
					</Table>
				)}
			</CardContent>
		</Card>
	);
}

function ApiTokenRow({ token, onRevoke }: { token: ApiToken; onRevoke: () => void }) {
	const expiresAt = new Date(token.expiresAt);
	const now = useNow();
	const isExpired = expiresAt.getTime() <= now;

	return (
		<TableRow>
			<TableCell>
				<div className="font-medium">{token.name}</div>
				<code className="font-mono text-xs text-muted-foreground">{token.prefix}…</code>
			</TableCell>
			<TableCell className="whitespace-normal">
				<div className="flex flex-wrap gap-1">
					{token.scopes.map((scope) => (
						<Badge key={scope} variant={scope === "admin" ? "default" : "outline"}>
							{scopeLabel(scope)}
						</Badge>
					))}
				</div>
			</TableCell>
			<TableCell>
				{isExpired ? (
					<Badge variant="destructive">{m.apiTokenExpired()}</Badge>
				) : (
					expiresAt.toLocaleDateString(getLocale())
				)}
			</TableCell>
			<TableCell>
				<RelativeTime value={token.lastUsedAt} fallback={m.never()} />
			</TableCell>
			<TableCell className="text-right">
				<ConfirmationDialog
					onConfirm={onRevoke}
					title={m.revokeApiTokenConfirmTitle({ name: token.name })}
					description={m.revokeApiTokenConfirmDescription()}
					confirmText={m.revoke()}
					triggerText={
						<>
							<Trash2 className="h-4 w-4" />
							<span className="sr-only">{m.revoke()}</span>
						</>
					}
					triggerProps={{ variant: "ghost", size: "icon" }}
				/>
			</TableCell>
		</TableRow>
	);
}
