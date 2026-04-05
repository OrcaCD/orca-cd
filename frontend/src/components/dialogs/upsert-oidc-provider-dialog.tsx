// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { createOIDCProvider, updateOIDCProvider, type OIDCProviderDetail } from "@/lib/oidc";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";
import { Checkbox } from "../ui/checkbox";

const providerSchema = z.object({
	name: z.string().min(1, "Name is required").max(100, "Name must be at most 100 characters"),
	issuerUrl: z.httpUrl("Must be a valid URL"),
	clientId: z.string().min(1, "Client Id is required"),
	clientSecret: z.string(),
	scopes: z.string(),
	enabled: z.boolean(),
	requireVerifiedEmail: z.boolean(),
	autoSignup: z.boolean(),
});

export default function UpsertOIDCProviderDialog({
	provider,
	onSave,
}: {
	provider: OIDCProviderDetail | null;
	onSave: () => void;
}) {
	const isEditing = !!provider;
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [open, setOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: provider?.name ?? "",
			issuerUrl: provider?.issuerUrl ?? "",
			clientId: provider?.clientId ?? "",
			clientSecret: "",
			scopes: provider?.scopes ?? "",
			enabled: provider?.enabled ?? true,
			requireVerifiedEmail: provider?.requireVerifiedEmail ?? false,
			autoSignup: provider?.autoSignup ?? true,
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
						requireVerifiedEmail: value.requireVerifiedEmail,
						autoSignup: value.autoSignup,
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
						requireVerifiedEmail: value.requireVerifiedEmail,
						autoSignup: value.autoSignup,
					});
					toast.success("Provider created");
				}
				onSave();
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save provider");
			} finally {
				setIsSubmitting(false);
			}
		},
	});
	return (
		<Dialog open={open} onOpenChange={(open) => setOpen(open)}>
			<DialogTrigger asChild>
				{isEditing ? (
					<Button variant="ghost" size="icon">
						<Pencil className="h-4 w-4" />
					</Button>
				) : (
					<Button>
						<Plus className="mr-2 h-4 w-4" />
						Add Provider
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? "Edit Provider" : "Add Provider"}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing
							? "Update the OIDC provider configuration."
							: "Configure a new OpenID Connect provider."}
					</DialogDescription>
				</DialogHeader>

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
										<Label htmlFor={field.name}>Client Id</Label>
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
										<Checkbox
											id={field.name}
											checked={field.state.value}
											onCheckedChange={(checked) => field.handleChange(checked === true)}
											className="h-4 w-4 rounded border-gray-300"
										/>
										<Label htmlFor={field.name}>Enabled</Label>
									</div>
								</Field>
							)}
						/>
						<form.Field
							name="requireVerifiedEmail"
							children={(field) => (
								<Field>
									<div className="flex items-center gap-2">
										<Checkbox
											id={field.name}
											checked={field.state.value}
											onCheckedChange={(checked) => field.handleChange(checked === true)}
											className="h-4 w-4 rounded border-gray-300"
										/>
										<Label htmlFor={field.name}>Require verified emails</Label>
									</div>
								</Field>
							)}
						/>
						<form.Field
							name="autoSignup"
							children={(field) => (
								<Field>
									<div className="flex items-center gap-2">
										<Checkbox
											id={field.name}
											checked={field.state.value}
											onCheckedChange={(checked) => field.handleChange(checked === true)}
											className="h-4 w-4 rounded border-gray-300"
										/>
										<Label htmlFor={field.name}>Allow auto signup</Label>
									</div>
								</Field>
							)}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? "Saving..." : isEditing ? "Update Provider" : "Create Provider"}
							</Button>
							<Button
								type="button"
								variant="outline"
								onClick={() => {
									setOpen(false);
									form.reset();
								}}
								disabled={isSubmitting}
							>
								Cancel
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
