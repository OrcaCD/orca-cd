import { createFileRoute, redirect } from "@tanstack/react-router";
import { PlugZap } from "lucide-react";
import { useState } from "react";
import { AuthErrorBanner, AuthLayout, authControlClass } from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldLabel,
	FieldLegend,
	FieldSet,
	FieldTitle,
} from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import { useFetch } from "@/lib/api";
import { apiTokenScopeLabel, type ApiTokenScope } from "@/lib/api-tokens";
import { useAuth } from "@/lib/auth";
import {
	answerOAuthAuthorizationRequest,
	oauthErrorRedirect,
	rememberLoginRedirect,
	type OAuthAuthorizationRequest,
} from "@/lib/oauth";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

// The consent page of the hub's OAuth server, where the user lets an MCP
// client like Claude Code use OrcaCD.
export const Route = createFileRoute("/oauth/authorize")({
	beforeLoad: ({ context }) => {
		if (!context.auth.isAuthenticated) {
			rememberLoginRedirect(window.location.pathname + window.location.search);
			throw redirect({ to: "/login" });
		}
	},
	component: OAuthAuthorizePage,
	head: () => ({
		meta: [
			{
				title: m.oauthAuthorizeHeadTitle(),
			},
		],
	}),
});

// The hub checked the redirect when the client registered. This only keeps a
// page that runs code from being opened, should that check ever miss one.
function leaveTo(url: string) {
	const protocol = new URL(url).protocol;
	if (["javascript:", "data:", "vbscript:"].includes(protocol)) {
		return;
	}
	window.location.assign(url);
}

function OAuthAuthorizePage() {
	// Read once and passed on as it is, the router would parse and reformat it
	const [query] = useState(() => window.location.search.slice(1));
	const { data, error } = useFetch<OAuthAuthorizationRequest>(`/oauth/authorize?${query}`, {
		revalidateOnFocus: false,
		revalidateOnReconnect: false,
		shouldRetryOnError: false,
	});
	return (
		<AuthLayout>
			{error ? (
				<div className="flex flex-col gap-4">
					<h1 className="text-3xl font-semibold tracking-tight">{m.oauthAuthorizeFailed()}</h1>
					<AuthErrorBanner
						message={error instanceof Error ? error.message : m.oauthAuthorizeFailed()}
					/>
					<BackToAppButton redirectUrl={oauthErrorRedirect(error)} />
				</div>
			) : data ? (
				<ConsentForm query={query} request={data} />
			) : (
				<div className="flex flex-col gap-4" aria-busy="true">
					<Skeleton className="h-10 w-3/4" />
					<Skeleton className="h-24 w-full" />
					<Skeleton className="h-40 w-full" />
				</div>
			)}
		</AuthLayout>
	);
}

function ConsentForm({ query, request }: { query: string; request: OAuthAuthorizationRequest }) {
	const { auth } = useAuth();
	const [scopes, setScopes] = useState<ApiTokenScope[]>(() =>
		request.scopes.filter(({ grantable }) => grantable).map(({ scope }) => scope),
	);
	const [submitting, setSubmitting] = useState<"approve" | "deny" | null>(null);
	const [leaving, setLeaving] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);
	const [errorRedirect, setErrorRedirect] = useState<string | null>(null);
	const redirect = new URL(request.redirectUri);

	async function answer(approve: boolean) {
		setSubmitting(approve ? "approve" : "deny");
		setSubmitError(null);
		setErrorRedirect(null);
		try {
			const url = await answerOAuthAuthorizationRequest(query, approve, approve ? scopes : []);
			setLeaving(true);
			leaveTo(url);
		} catch (err) {
			setSubmitError(err instanceof Error ? err.message : m.oauthAuthorizeFailed());
			setErrorRedirect(oauthErrorRedirect(err));
		} finally {
			setSubmitting(null);
		}
	}

	if (leaving) {
		return (
			<div className="flex flex-col gap-2">
				<h1 className="text-3xl font-semibold tracking-tight">
					{m.oauthReturningTo({ clientName: request.clientName })}
				</h1>
				<p className="text-sm text-muted-foreground">{m.oauthReturningDescription()}</p>
			</div>
		);
	}

	return (
		<div className="flex flex-col gap-6">
			<div className="flex flex-col gap-2">
				<PlugZap className="size-8 text-muted-foreground" aria-hidden="true" />
				<h1 className="text-3xl font-semibold tracking-tight text-balance">
					{m.oauthAuthorizeTitle({ clientName: request.clientName })}
				</h1>
				<p className="text-sm text-muted-foreground">
					{m.oauthAuthorizeDescription({
						name: auth.profile?.name ?? "",
						email: auth.profile?.email ?? "",
					})}
				</p>
			</div>

			<div className="flex flex-col gap-1 rounded-md border bg-muted/40 px-3 py-2 text-sm">
				{request.clientHost && (
					<div>
						<span className="text-muted-foreground">{m.oauthClientPublishedBy()} </span>
						<code className="font-mono break-all">{request.clientHost}</code>
					</div>
				)}
				<div>
					<span className="text-muted-foreground">{m.oauthRedirectsTo()} </span>
					<code className="font-mono break-all">{`${redirect.protocol}//${redirect.host}`}</code>
				</div>
			</div>

			<p className="text-sm text-muted-foreground">{consentWarning(request, redirect)}</p>

			{!request.canAuthorize && <AuthErrorBanner message={m.apiTokensCreateNotAllowed()} />}

			<FieldSet>
				<FieldLegend variant="label">{m.oauthRequestedScopes()}</FieldLegend>
				<div className="flex flex-col gap-2">
					{request.scopes.map(({ scope, grantable }) => {
						const id = `oauth-scope-${scope}`;
						return (
							<FieldLabel key={scope} htmlFor={id}>
								<Field orientation="horizontal" data-disabled={!grantable || !request.canAuthorize}>
									<Checkbox
										id={id}
										checked={scopes.includes(scope)}
										disabled={!grantable || !request.canAuthorize}
										onCheckedChange={(checked) =>
											setScopes((current) =>
												checked ? [...current, scope] : current.filter((s) => s !== scope),
											)
										}
									/>
									<FieldContent>
										<FieldTitle>{apiTokenScopeLabel(scope)}</FieldTitle>
										{!grantable && <FieldDescription>{m.oauthScopeNotAllowed()}</FieldDescription>}
									</FieldContent>
								</Field>
							</FieldLabel>
						);
					})}
				</div>
			</FieldSet>

			<p className="text-sm text-muted-foreground">{m.oauthRevokeHint()}</p>

			{submitError && <AuthErrorBanner message={submitError} />}
			<BackToAppButton redirectUrl={errorRedirect} />

			<div className="flex flex-col gap-2 sm:flex-row">
				<Button
					className={cn(authControlClass, "sm:flex-1")}
					disabled={submitting !== null || !request.canAuthorize || scopes.length === 0}
					onClick={() => answer(true)}
				>
					{submitting === "approve" ? m.oauthAllowing() : m.oauthAllow()}
				</Button>
				<Button
					variant="outline"
					className={cn(authControlClass, "sm:flex-1")}
					disabled={submitting !== null}
					onClick={() => answer(false)}
				>
					{m.oauthDeny()}
				</Button>
			</div>
		</div>
	);
}

// What the user should check before allowing the app. A published name only
// says which app it is when the answer goes to that app's own site: on this
// computer, any program can receive it.
function consentWarning(request: OAuthAuthorizationRequest, redirect: URL): string {
	if (!request.clientHost) {
		return m.oauthAuthorizeWarning();
	}
	// The hub only allows http on loopback addresses, and other schemes open
	// an app on this computer.
	return redirect.protocol === "https:"
		? m.oauthAuthorizeWarningPublished({ host: request.clientHost })
		: m.oauthAuthorizeWarningLocal({ clientName: request.clientName, host: request.clientHost });
}

// Sends the user back to the app after an error. It takes a click, as anyone
// can register an app, so a link to the consent page must not redirect to it
// on its own (RFC 9700 section 4.11.2).
function BackToAppButton({ redirectUrl }: { redirectUrl: string | null }) {
	if (!redirectUrl) {
		return null;
	}
	const url = new URL(redirectUrl);
	return (
		<Button
			variant="outline"
			className={cn(authControlClass, "w-full")}
			onClick={() => leaveTo(redirectUrl)}
		>
			{m.oauthBackToApp({ host: `${url.protocol}//${url.host}` })}
		</Button>
	);
}
