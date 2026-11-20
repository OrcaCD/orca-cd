import { Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { CardContent } from "@/components/ui/card";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { CardStackSkeleton } from "@/components/collection-states";
import ErrorAlert from "@/components/alerts/error-alert";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { RelativeTime } from "@/components/relative-time";
import { useNow } from "@/hooks/use-now";
import { apiTokenScopeLabel, type ApiToken } from "@/lib/api-tokens";
import { useAuth } from "@/lib/auth";
import { m } from "@/lib/paraglide/messages";

// The body of a card listing API tokens. The rows span the whole card and the
// outer cells line up with the card header.
export function ApiTokensTable({
	tokens,
	error,
	isLoading,
	onRevoke,
	showOwner = false,
}: {
	tokens: ApiToken[] | undefined;
	error: unknown;
	isLoading: boolean;
	onRevoke: (token: ApiToken) => void;
	showOwner?: boolean;
}) {
	if (isLoading || error || tokens?.length === 0) {
		return (
			<CardContent>
				{isLoading && <CardStackSkeleton label={m.loadingApiTokens()} heights={["h-24"]} />}
				{error instanceof Error && (
					<ErrorAlert title={m.apiTokensLoadFailed()} description={error.message} />
				)}
				{tokens?.length === 0 && (
					<p className="py-6 text-center text-muted-foreground">{m.noApiTokens()}</p>
				)}
			</CardContent>
		);
	}
	if (!tokens) {
		return null;
	}

	return (
		<Table>
			<TableHeader>
				<TableRow>
					<TableHead className="pl-(--card-spacing)">{m.name()}</TableHead>
					{showOwner && <TableHead>{m.apiTokenOwner()}</TableHead>}
					<TableHead className="w-full">{m.apiTokenScopes()}</TableHead>
					<TableHead>{m.apiTokenExpires()}</TableHead>
					<TableHead>{m.apiTokenLastUsed()}</TableHead>
					<TableHead className="pr-(--card-spacing)">
						<span className="sr-only">{m.actions()}</span>
					</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				{tokens.map((token) => (
					<ApiTokenRow
						key={token.id}
						token={token}
						showOwner={showOwner}
						onRevoke={() => onRevoke(token)}
					/>
				))}
			</TableBody>
		</Table>
	);
}

function ApiTokenRow({
	token,
	showOwner,
	onRevoke,
}: {
	token: ApiToken;
	showOwner: boolean;
	onRevoke: () => void;
}) {
	const now = useNow();
	const isExpired = new Date(token.expiresAt).getTime() <= now;

	return (
		<TableRow>
			<TableCell className="pl-(--card-spacing)">
				<div className="flex items-center gap-2 font-medium">
					{token.name}
					{token.oauth && (
						<Badge variant="secondary" title={m.apiTokenConnectedAppDescription()}>
							{m.apiTokenConnectedApp()}
						</Badge>
					)}
				</div>
				<code className="font-mono text-xs text-muted-foreground">{token.prefix}…</code>
			</TableCell>
			{showOwner && (
				<TableCell>
					<ApiTokenOwnerCell token={token} />
				</TableCell>
			)}
			<TableCell className="whitespace-normal">
				<div className="flex flex-wrap gap-1">
					{token.scopes.map((scope) => (
						<Badge key={scope} variant={scope === "admin" ? "default" : "outline"}>
							{apiTokenScopeLabel(scope)}
						</Badge>
					))}
				</div>
			</TableCell>
			<TableCell>
				{isExpired ? (
					<Badge variant="destructive">{m.apiTokenExpired()}</Badge>
				) : (
					<RelativeTime value={token.expiresAt} future />
				)}
			</TableCell>
			<TableCell>
				<RelativeTime value={token.lastUsedAt} fallback={m.never()} />
			</TableCell>
			<TableCell className="pr-(--card-spacing) text-right">
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

function ApiTokenOwnerCell({ token }: { token: ApiToken }) {
	const { auth } = useAuth();
	if (!token.owner) {
		return <span className="text-muted-foreground">-</span>;
	}

	return (
		<>
			<div>
				{token.owner.name}
				{token.owner.id === auth.profile?.id && (
					<Badge variant="outline" className="ml-2">
						{m.you()}
					</Badge>
				)}
			</div>
			<span className="text-xs text-muted-foreground">{token.owner.email}</span>
		</>
	);
}
