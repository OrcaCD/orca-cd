// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import useSWR from "swr";
import { toast } from "sonner";
import { Copy, Trash2 } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import fetcher, { API_BASE } from "@/lib/api";
import { deleteOIDCProvider, type OIDCProviderDetail } from "@/lib/oidc";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import UpsertOIDCProviderDialog from "@/components/dialogs/upsert-oidc-provider-dialog";

export const Route = createFileRoute("/_authenticated/admin/oidc-providers")({
	component: OIDCProvidersPage,
});

function OIDCProvidersPage() {
	const {
		data: providers,
		mutate,
		isLoading,
	} = useSWR<OIDCProviderDetail[]>(`${API_BASE}/admin/oidc-providers`, fetcher);

	async function handleDelete(provider: OIDCProviderDetail) {
		try {
			await deleteOIDCProvider(provider.id);
			toast.success("Provider deleted");
			await mutate();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "Failed to delete provider");
		}
	}

	async function handleSave() {
		await mutate();
	}

	async function handleCopyCallbackUrl(callbackUrl: string) {
		try {
			await navigator.clipboard.writeText(callbackUrl);
			toast.success("Callback URL copied");
		} catch {
			toast.error("Failed to copy callback URL");
		}
	}

	return (
		<div className="flex flex-col gap-6">
			<div className="flex items-center justify-between">
				<div>
					<h1 className="text-2xl font-bold">SSO Providers</h1>
					<p className="text-muted-foreground text-sm">
						Configure OpenID Connect providers for single sign-on.
					</p>
				</div>

				<UpsertOIDCProviderDialog provider={null} onSave={handleSave} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">Loading providers...</p>}

			{providers && providers.length === 0 && (
				<Card>
					<CardContent className="py-8 text-center text-muted-foreground">
						No SSO providers configured yet. Click "Add Provider" to get started.
					</CardContent>
				</Card>
			)}

			{providers &&
				providers.map((provider) => (
					<Card key={provider.id}>
						<CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
							<div>
								<CardTitle className="text-lg">{provider.name}</CardTitle>
								<CardDescription>{provider.issuerUrl}</CardDescription>
							</div>
							<div className="flex items-center gap-2">
								<span
									className={`inline-flex items-center rounded-full px-2 py-1 text-xs font-medium ${
										provider.enabled
											? "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300"
											: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400"
									}`}
								>
									{provider.enabled ? "Enabled" : "Disabled"}
								</span>
								<UpsertOIDCProviderDialog provider={provider} onSave={handleSave} />
								<ConfirmationDialog
									onConfirm={() => handleDelete(provider)}
									triggerText={<Trash2 className="h-4 w-4" />}
									triggerProps={{ variant: "ghost", size: "icon" }}
								/>
							</div>
						</CardHeader>
						<CardContent>
							<div className="text-sm text-muted-foreground">
								&middot; <span className="font-medium">Client Id:</span> {provider.clientId}
								{provider.scopes && (
									<>
										{" "}
										&middot; <span className="font-medium">Extra scopes:</span> {provider.scopes}
									</>
								)}
								<br />
								&middot; <span className="font-medium">Verified email:</span>{" "}
								{provider.requireVerifiedEmail ? "Required" : "Not required"} <br /> &middot;{" "}
								<span className="font-medium">Auto signup:</span>{" "}
								{provider.autoSignup ? "Allowed" : "Disabled"}
							</div>
							<div className="mt-2 text-sm text-muted-foreground">
								<div className="flex items-center gap-2">
									<span className="font-medium">Callback URL:</span>
									<code className="bg-muted rounded px-1 py-0.5 font-mono text-xs break-all">
										{provider.callbackUrl}
									</code>
									<Button
										type="button"
										variant="ghost"
										size="icon"
										onClick={() => handleCopyCallbackUrl(provider.callbackUrl)}
										className="h-7 w-7 shrink-0"
										aria-label="Copy callback URL"
										title="Copy callback URL"
									>
										<Copy className="h-3.5 w-3.5" />
									</Button>
								</div>
							</div>
						</CardContent>
					</Card>
				))}
		</div>
	);
}
