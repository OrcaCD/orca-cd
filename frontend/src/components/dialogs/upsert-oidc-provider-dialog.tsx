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
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { createOIDCProvider, updateOIDCProvider, type OIDCProviderDetail } from "@/lib/oidc";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { m } from "@/lib/paraglide/messages";

const providerSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationProviderNameRequired())
		.max(100, m.validationProviderNameMaxLength()),
	issuerUrl: z.url({ error: m.validationIssuerUrlInvalid(), protocol: /^https?$/ }).trim(),
	clientId: z.string().trim().min(1, m.validationClientIdRequired()),
	clientSecret: z.string().trim(),
	scopes: z.string().trim(),
	enabled: z.boolean(),
	requireVerifiedEmail: z.boolean(),
	autoSignup: z.boolean(),
});

export default function UpsertOIDCProviderDialog({
	provider,
	asDropdownItem = false,
}: {
	provider: OIDCProviderDetail | null;
	asDropdownItem?: boolean;
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
						message: m.validationClientSecretRequired(),
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
					toast.success(m.providerUpdated());
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
					toast.success(m.providerCreated());
				}
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveProvider());
			} finally {
				setIsSubmitting(false);
			}
		},
	});
	return (
		<Dialog open={open} onOpenChange={(open) => setOpen(open)}>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
						<Pencil className="h-4 w-4" />
						{m.edit()}
					</DropdownMenuItem>
				) : isEditing ? (
					<Button variant="ghost" size="icon">
						<Pencil className="h-4 w-4" />
					</Button>
				) : (
					<Button>
						<Plus className="h-4 w-4" />
						{m.addProvider()}
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? m.editProvider() : m.addProvider()}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing ? m.editProviderDescription() : m.addProviderDescription()}
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
										<Label htmlFor={field.name}>{m.displayName()}</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.displayOIDCNamePlaceholder()}
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
										<Label htmlFor={field.name}>{m.issuerUrl()}</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.issuerUrlPlaceholder()}
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
										<Label htmlFor={field.name}>{m.clientId()}</Label>
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
											{m.clientSecret()}
											{isEditing && (
												<span className="text-muted-foreground font-normal">
													{" "}
													{m.leaveBlankKeepCurrent()}
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
										{m.additionalScopes()}{" "}
										<span className="text-muted-foreground font-normal">{m.optional()}</span>
									</Label>
									<Input
										id={field.name}
										value={field.state.value}
										onBlur={field.handleBlur}
										onChange={(e) => field.handleChange(e.target.value)}
										placeholder={m.additionalScopesPlaceholder()}
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
										<Label htmlFor={field.name}>{m.enabled()}</Label>
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
										<Label htmlFor={field.name}>{m.requireVerifiedEmails()}</Label>
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
										<Label htmlFor={field.name}>{m.allowAutoSignup()}</Label>
									</div>
								</Field>
							)}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting
									? m.savingDots()
									: isEditing
										? m.updateProvider()
										: m.createProvider()}
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
								{m.cancel()}
							</Button>
						</div>
					</FieldGroup>
				</form>
			</DialogContent>
		</Dialog>
	);
}
