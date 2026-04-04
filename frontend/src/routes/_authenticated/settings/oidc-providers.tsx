// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import useSWR from "swr";
import { toast } from "sonner";
import { Plus, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import fetcher, { API_BASE } from "@/lib/api";
import {
	createOIDCProvider,
	deleteOIDCProvider,
	updateOIDCProvider,
	type OIDCProviderDetail,
} from "@/lib/oidc";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";

export const Route = createFileRoute("/_authenticated/settings/oidc-providers")({
	component: OIDCProvidersPage,
});

const providerSchema = z.object({
	name: z.string().min(1, "Name is required").max(100, "Name must be at most 100 characters"),
	issuerUrl: z.string().url("Must be a valid URL"),
	clientId: z.string().min(1, "Client ID is required"),
	clientSecret: z.string(),
	scopes: z.string(),
	enabled: z.boolean(),
});

function OIDCProvidersPage() {
	const {
		data: providers,
		mutate,
		isLoading,
	} = useSWR<OIDCProviderDetail[]>(`${API_BASE}/admin/oidc-providers`, fetcher);
	const [editingProvider, setEditingProvider] = useState<OIDCProviderDetail | null>(null);
	const [showForm, setShowForm] = useState(false);

	function handleEdit(provider: OIDCProviderDetail) {
		setEditingProvider(provider);
		setShowForm(true);
	}

	function handleAdd() {
		setEditingProvider(null);
		setShowForm(true);
	}

	function handleCancel() {
		setShowForm(false);
		setEditingProvider(null);
	}

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
		setShowForm(false);
		setEditingProvider(null);
		await mutate();
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
				{!showForm && (
					<Button onClick={handleAdd}>
						<Plus className="mr-2 h-4 w-4" />
						Add Provider
					</Button>
				)}
			</div>

			{showForm && (
				<ProviderForm provider={editingProvider} onSave={handleSave} onCancel={handleCancel} />
			)}

			{isLoading && <p className="text-muted-foreground text-sm">Loading providers...</p>}

			{providers && providers.length === 0 && !showForm && (
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
								<Button variant="ghost" size="icon" onClick={() => handleEdit(provider)}>
									<Pencil className="h-4 w-4" />
								</Button>
								<ConfirmationDialog
									onConfirm={() => handleDelete(provider)}
									triggerText={<Trash2 className="h-4 w-4" />}
									triggerProps={{ variant: "ghost", size: "icon" }}
								/>
							</div>
						</CardHeader>
						<CardContent>
							<div className="text-sm text-muted-foreground">
								<span className="font-medium">Client ID:</span> {provider.clientId}
								{provider.scopes && (
									<>
										{" "}
										&middot; <span className="font-medium">Extra scopes:</span> {provider.scopes}
									</>
								)}
							</div>
						</CardContent>
					</Card>
				))}
		</div>
	);
}

function ProviderForm({
	provider,
	onSave,
	onCancel,
}: {
	provider: OIDCProviderDetail | null;
	onSave: () => void;
	onCancel: () => void;
}) {
	const isEditing = !!provider;
	const [isSubmitting, setIsSubmitting] = useState(false);

	const form = useForm({
		defaultValues: {
			name: provider?.name ?? "",
			issuerUrl: provider?.issuerUrl ?? "",
			clientId: provider?.clientId ?? "",
			clientSecret: "",
			scopes: provider?.scopes ?? "",
			enabled: provider?.enabled ?? true,
		},
		validators: {
			onSubmit: isEditing
				? providerSchema
				: providerSchema.refine((d) => !!d.clientSecret, {
						message: "Client secret is required",
						path: ["clientSecret"],
					}),
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				if (isEditing && provider) {
					await updateOIDCProvider(provider.id, {
						name: value.name,
						issuerUrl: value.issuerUrl,
						clientId: value.clientId,
						clientSecret: value.clientSecret || undefined,
						scopes: value.scopes,
						enabled: value.enabled,
					});
					toast.success("Provider updated");
				} else {
					await createOIDCProvider({
						name: value.name,
						issuerUrl: value.issuerUrl,
						clientId: value.clientId,
						clientSecret: value.clientSecret!,
						scopes: value.scopes,
						enabled: value.enabled,
					});
					toast.success("Provider created");
				}
				onSave();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save provider");
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<Card>
			<CardHeader>
				<CardTitle>{isEditing ? "Edit Provider" : "Add Provider"}</CardTitle>
				<CardDescription>
					{isEditing
						? "Update the OIDC provider configuration."
						: "Configure a new OpenID Connect provider."}
				</CardDescription>
			</CardHeader>
			<CardContent>
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<FieldGroup>
						<form.Field
							name="name"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Display Name</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder='e.g. "Google" or "Corporate SSO"'
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="issuerUrl"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Issuer URL</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder="https://accounts.google.com"
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="clientId"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Client ID</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="clientSecret"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>
											Client Secret
											{isEditing && (
												<span className="text-muted-foreground font-normal">
													{" "}
													(leave blank to keep current)
												</span>
											)}
										</Label>
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											autoComplete="off"
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="scopes"
							children={(field) => (
								<Field>
									<Label htmlFor={field.name}>
										Additional Scopes{" "}
										<span className="text-muted-foreground font-normal">(optional)</span>
									</Label>
									<Input
										id={field.name}
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(e) => field.handleChange(e.target.value)}
										placeholder="e.g. groups,offline_access"
									/>
								</Field>
							)}
						/>
						<form.Field
							name="enabled"
							children={(field) => (
								<Field>
									<div className="flex items-center gap-2">
										<input
											id={field.name}
											type="checkbox"
											checked={field.state.value}
											onChange={(e) => field.handleChange(e.target.checked)}
											className="h-4 w-4 rounded border-gray-300"
										/>
										<Label htmlFor={field.name}>Enabled</Label>
									</div>
								</Field>
							)}
						/>
						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? "Saving..." : isEditing ? "Update Provider" : "Create Provider"}
							</Button>
							<Button type="button" variant="outline" onClick={onCancel}>
								Cancel
							</Button>
						</div>
					</FieldGroup>
				</form>
			</CardContent>
		</Card>
	);
}
