// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import useSWR from "swr";
import { toast } from "sonner";
import { Copy, EllipsisVertical, ShieldCheck, Trash2, UserPlus } from "lucide-react";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
						<CardHeader>
							<CardAction className="flex items-center gap-3">
								<Badge variant={provider.enabled ? "success" : "secondary"}>
									{provider.enabled ? "Enabled" : "Disabled"}
								</Badge>

								<DropdownMenu>
									<DropdownMenuTrigger asChild>
										<Button variant="ghost" size="icon" className="h-8 w-8">
											<EllipsisVertical className="h-4 w-4" />
											<span className="sr-only">Actions</span>
										</Button>
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<UpsertOIDCProviderDialog
											provider={provider}
											onSave={handleSave}
											asDropdownItem
										/>
										<DropdownMenuSeparator />
										<ConfirmationDialog
											onConfirm={() => handleDelete(provider)}
											triggerText={
												<>
													<Trash2 className="h-4 w-4" />
													Delete
												</>
											}
											asDropdownItem
										/>
									</DropdownMenuContent>
								</DropdownMenu>
							</CardAction>
							<CardTitle className="text-lg">{provider.name}</CardTitle>
							<CardDescription className="mt-0.5">{provider.issuerUrl}</CardDescription>
						</CardHeader>
						<Separator />
						<CardContent className="pt-4">
							<div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
								<div>
									<span className="text-muted-foreground">Client ID</span>
									<p className="font-mono text-xs mt-0.5">{provider.clientId}</p>
								</div>
								{provider.scopes && (
									<div>
										<span className="text-muted-foreground">Extra Scopes</span>
										<p className="font-mono text-xs mt-0.5">{provider.scopes}</p>
									</div>
								)}
							</div>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								<Badge variant={provider.requireVerifiedEmail ? "default" : "outline"}>
									<ShieldCheck className="mr-1 h-3 w-3" />
									{provider.requireVerifiedEmail
										? "Verified email required"
										: "Unverified email allowed"}
								</Badge>
								<Badge variant={provider.autoSignup ? "default" : "outline"}>
									<UserPlus className="mr-1 h-3 w-3" />
									{provider.autoSignup ? "Auto signup" : "No auto signup"}
								</Badge>
							</div>
						</CardContent>
						<CardFooter className="bg-card flex items-center gap-2 text-sm">
							<span className="text-muted-foreground shrink-0">Callback URL</span>
							<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">
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
						</CardFooter>
					</Card>
				))}
		</div>
	);
}
