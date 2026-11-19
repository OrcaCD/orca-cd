// oxlint-disable react/no-children-prop
import { Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
	ResponsiveDialogTrigger,
} from "@/components/responsive-dialog";
import { toast } from "@/components/ui/toast";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldError,
	FieldGroup,
	FieldLabel,
	FieldLegend,
	FieldSet,
	FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import CopyValueDialog from "./copy-value-dialog";
import {
	apiTokenScope,
	createApiToken,
	grantableApiTokenScopes,
	type ApiTokenScope,
} from "@/lib/api-tokens";
import { useAuth } from "@/lib/auth";
import { usePermissions } from "@/lib/permissions";
import { actionLabels, resourceLabels } from "@/lib/role-draft";
import { m } from "@/lib/paraglide/messages";

const expiryOptions = [
	{ value: "7", days: 7, label: () => m.apiTokenExpiryDays({ days: 7 }) },
	{ value: "30", days: 30, label: () => m.apiTokenExpiryDays({ days: 30 }) },
	{ value: "90", days: 90, label: () => m.apiTokenExpiryDays({ days: 90 }) },
	{ value: "365", days: 365, label: () => m.apiTokenExpiryOneYear() },
];

const apiTokenSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationApiTokenNameRequired())
		.max(64, m.validationApiTokenNameMaxLength()),
	expiryDays: z.string(),
	scopes: z.array(z.string()).min(1, m.validationApiTokenScopesRequired()),
});

export default function CreateApiTokenDialog() {
	const [open, setOpen] = useState(false);
	const [createdToken, setCreatedToken] = useState<string | null>(null);

	return (
		<>
			<ResponsiveDialog open={open} onOpenChange={setOpen}>
				<ResponsiveDialogTrigger
					render={
						<Button>
							<Plus className="mr-2 h-4 w-4" />
							{m.createApiToken()}
						</Button>
					}
				></ResponsiveDialogTrigger>
				<ResponsiveDialogContent className="sm:max-w-lg">
					<ResponsiveDialogHeader>
						<ResponsiveDialogTitle>{m.createApiToken()}</ResponsiveDialogTitle>
						<ResponsiveDialogDescription className="py-2">
							{m.createApiTokenDescription()}
						</ResponsiveDialogDescription>
					</ResponsiveDialogHeader>
					<ApiTokenForm
						onClose={() => setOpen(false)}
						onCreated={(token) => setCreatedToken(token)}
					/>
				</ResponsiveDialogContent>
			</ResponsiveDialog>

			<CopyValueDialog
				open={createdToken !== null}
				onOpenChange={(nextOpen) => {
					if (!nextOpen) {
						setCreatedToken(null);
					}
				}}
				title={m.apiTokenCreated()}
				description={m.apiTokenCopyNow()}
				label={m.apiToken()}
				value={createdToken ?? ""}
				inputId="created-api-token"
				copyTitle={m.copyApiToken()}
			/>
		</>
	);
}

function ApiTokenForm({
	onClose,
	onCreated,
}: {
	onClose: () => void;
	onCreated: (token: string) => void;
}) {
	const { auth } = useAuth();
	const permissions = usePermissions();
	const isAdmin = auth.profile?.isAdmin ?? false;
	const grantable = grantableApiTokenScopes(permissions);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const form = useForm({
		defaultValues: {
			name: "",
			expiryDays: "30",
			scopes: [] as string[],
		},
		validators: { onSubmit: apiTokenSchema },
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				const expiresAt = new Date();
				expiresAt.setDate(expiresAt.getDate() + Number(value.expiryDays));
				const created = await createApiToken({
					name: value.name.trim(),
					scopes: value.scopes as ApiTokenScope[],
					expiresAt: expiresAt.toISOString(),
				});
				toast.add({ type: "success", title: m.apiTokenCreated() });
				onCreated(created.token);
				onClose();
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedCreateApiToken(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
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
								<Label htmlFor={field.name}>{m.name()}</Label>
								<Input
									id={field.name}
									value={field.state.value}
									onBlur={field.handleBlur}
									onChange={(e) => field.handleChange(e.target.value)}
									placeholder={m.apiTokenNamePlaceholder()}
									autoFocus
								/>
								{isInvalid && <FieldError errors={field.state.meta.errors} />}
							</Field>
						);
					}}
				/>
				<form.Field
					name="expiryDays"
					children={(field) => (
						<Field>
							<Label htmlFor={field.name}>{m.apiTokenExpiry()}</Label>
							<Select
								value={field.state.value}
								onValueChange={(value) => field.handleChange(value ?? "30")}
								items={expiryOptions.map((option) => ({
									label: option.label(),
									value: option.value,
								}))}
							>
								<SelectTrigger id={field.name}>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{expiryOptions.map((option) => (
										<SelectItem key={option.value} value={option.value}>
											{option.label()}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</Field>
					)}
				/>
				<form.Field
					name="scopes"
					children={(field) => {
						const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
						const toggle = (scope: ApiTokenScope, checked: boolean) =>
							field.handleChange(
								checked
									? [...field.state.value, scope]
									: field.state.value.filter((s) => s !== scope),
							);
						return (
							<FieldSet data-invalid={isInvalid}>
								<FieldLegend variant="label">{m.apiTokenScopes()}</FieldLegend>
								<FieldDescription>{m.apiTokenScopesDescription()}</FieldDescription>
								<div className="flex max-h-72 flex-col gap-4 overflow-y-auto">
									{isAdmin && (
										<FieldLabel htmlFor="api-token-scope-admin">
											<Field orientation="horizontal">
												<Checkbox
													id="api-token-scope-admin"
													checked={field.state.value.includes("admin")}
													onCheckedChange={(checked) => toggle("admin", checked === true)}
												/>
												<FieldContent>
													<FieldTitle>{m.apiTokenScopeAdmin()}</FieldTitle>
													<FieldDescription>{m.apiTokenScopeAdminDescription()}</FieldDescription>
												</FieldContent>
											</Field>
										</FieldLabel>
									)}
									{grantable.map(({ resource, actions }) => (
										<div key={resource} className="flex flex-col gap-2">
											<span className="text-sm font-medium">{resourceLabels[resource]()}</span>
											<div className="flex flex-wrap gap-x-4 gap-y-2">
												{actions.map((action) => {
													const scope = apiTokenScope(resource, action);
													const id = `api-token-scope-${resource}-${action}`;
													return (
														<Field key={scope} orientation="horizontal" className="w-auto">
															<Checkbox
																id={id}
																checked={field.state.value.includes(scope)}
																onCheckedChange={(checked) => toggle(scope, checked === true)}
															/>
															<Label htmlFor={id} className="font-normal">
																{actionLabels[action]()}
															</Label>
														</Field>
													);
												})}
											</div>
										</div>
									))}
								</div>
								{isInvalid && <FieldError errors={field.state.meta.errors} />}
							</FieldSet>
						);
					}}
				/>

				<div className="flex gap-2 pt-2">
					<Button type="submit" disabled={isSubmitting}>
						{isSubmitting ? m.creatingDots() : m.createApiToken()}
					</Button>
					<Button type="button" variant="outline" onClick={onClose} disabled={isSubmitting}>
						{m.cancel()}
					</Button>
				</div>
			</FieldGroup>
		</form>
	);
}
