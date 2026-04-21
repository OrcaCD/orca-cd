// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { toast } from "sonner";
import { EllipsisVertical, ShieldCheck, Trash2, UserPlus } from "lucide-react";
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
import { useFetch } from "@/lib/api";
import { deleteOIDCProvider, type OIDCProviderDetail } from "@/lib/oidc";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import UpsertOIDCProviderDialog from "@/components/dialogs/upsert-oidc-provider-dialog";
import CopyButton from "@/components/copy-btn";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/admin/oidc-providers")({
	component: OIDCProvidersPage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.adminOidcProviders()}`,
			},
		],
	}),
});

function OIDCProvidersPage() {
	const { data: providers, isLoading } = useFetch<OIDCProviderDetail[]>("/admin/oidc-providers");

	async function handleDelete(provider: OIDCProviderDetail) {
		try {
			await deleteOIDCProvider(provider.id);
			toast.success(m.providerDeleted());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.failedDeleteProvider());
		}
	}

	return (
		<div className="flex flex-col gap-6">
			<div className="flex items-center justify-between">
				<div>
					<h1 className="text-2xl font-bold">{m.adminOidcProviders()}</h1>
					<p className="text-muted-foreground text-sm">{m.adminOidcProvidersDescription()}</p>
				</div>

				<UpsertOIDCProviderDialog provider={null} />
			</div>

			{isLoading && <p className="text-muted-foreground text-sm">{m.loadingProviders()}</p>}

			{providers && providers.length === 0 && (
				<Card>
					<CardContent className="py-8 text-center text-muted-foreground">
						{m.noSsoProvidersConfigured()}
					</CardContent>
				</Card>
			)}

			{providers &&
				providers.map((provider) => (
					<Card key={provider.id}>
						<CardHeader>
							<CardAction className="flex items-center gap-3">
								<Badge variant={provider.enabled ? "success" : "secondary"}>
									{provider.enabled ? m.enabled() : m.disabled()}
								</Badge>

								<DropdownMenu>
									<DropdownMenuTrigger asChild>
										<Button variant="ghost" size="icon" className="h-8 w-8">
											<EllipsisVertical className="h-4 w-4" />
											<span className="sr-only">{m.actions()}</span>
										</Button>
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<UpsertOIDCProviderDialog provider={provider} asDropdownItem />
										<DropdownMenuSeparator />
										<ConfirmationDialog
											onConfirm={() => handleDelete(provider)}
											triggerText={
												<>
													<Trash2 className="h-4 w-4" />
													{m.delete()}
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
									<span className="text-muted-foreground">{m.clientId()}</span>
									<p className="font-mono text-xs mt-0.5">{provider.clientId}</p>
								</div>
								{provider.scopes && (
									<div>
										<span className="text-muted-foreground">{m.extraScopes()}</span>
										<p className="font-mono text-xs mt-0.5">{provider.scopes}</p>
									</div>
								)}
							</div>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								<Badge variant={provider.requireVerifiedEmail ? "default" : "outline"}>
									<ShieldCheck className="mr-1 h-3 w-3" />
									{provider.requireVerifiedEmail
										? m.verifiedEmailRequired()
										: m.unverifiedEmailAllowed()}
								</Badge>
								<Badge variant={provider.autoSignup ? "default" : "outline"}>
									<UserPlus className="mr-1 h-3 w-3" />
									{provider.autoSignup ? m.autoSignup() : m.noAutoSignup()}
								</Badge>
							</div>
						</CardContent>
						<CardFooter className="bg-card flex flex-col items-start text-sm">
							<span className="text-muted-foreground shrink-0">{m.callbackUrl()}</span>
							<div className="mt-0.5 flex items-center gap-2">
								<code className="bg-muted rounded px-1.5 py-0.5 font-mono text-xs break-all">
									{provider.callbackUrl}
								</code>
								<CopyButton text={provider.callbackUrl} title={m.copyCallbackUrl()} />
							</div>
						</CardFooter>
					</Card>
				))}
		</div>
	);
}
