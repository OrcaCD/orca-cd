// oxlint-disable react/no-children-prop
import { Pencil, Plus, ShieldCheck, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
	ResponsiveDialogTrigger,
} from "@/components/responsive-dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { createUser, updateUser, type UserDetail } from "@/lib/users";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "@/components/ui/toast";
import { z } from "zod";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldError,
	FieldGroup,
	FieldLabel,
	FieldTitle,
} from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";
import { Checkbox } from "../ui/checkbox";
import { Badge } from "../ui/badge";
import CopyValueDialog from "./copy-value-dialog";
import { m } from "@/lib/paraglide/messages";
import { useFetch } from "@/lib/api";
import { roleDescription, roleName, type Role } from "@/lib/roles";

const baseSchema = z.object({
	name: z.string().trim().min(3, m.validationNameMinLength()).max(64, m.validationNameMaxLength()),
	email: z.email(m.validationMustBeValidEmailAddress()).trim(),
	roleIds: z.array(z.string()),
	resetPassword: z.boolean(),
});

function manualRoleIds(user: UserDetail | null, roles: Role[] | undefined): string[] {
	if (user) {
		return user.roles.filter((role) => role.sources.includes("manual")).map((role) => role.id);
	}
	// New users get the default role, like users created by an OIDC login
	const defaultRole = roles?.find((role) => role.builtIn === "user");
	return defaultRole ? [defaultRole.id] : [];
}

export default function UpsertUserDialog({
	user,
	asDropdownItem = false,
	disabled = false,
}: {
	user: UserDetail | null;
	asDropdownItem?: boolean;
	disabled?: boolean;
}) {
	const isEditing = !!user;
	const [open, setOpen] = useState(false);
	// The profile of OIDC users is managed by their provider, only their roles can change
	const isManaged = isEditing && !user.providers.includes("password");
	const { data: roles } = useFetch<Role[]>("/admin/roles");
	const oidcRoleIds = new Set(
		user?.roles.filter((role) => role.sources.includes("oidc")).map((role) => role.id) ?? [],
	);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [generatedPassword, setGeneratedPassword] = useState<string | null>(null);
	const [isGeneratedPasswordOpen, setIsGeneratedPasswordOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: user?.name ?? "",
			email: user?.email ?? "",
			roleIds: manualRoleIds(user, roles),
			resetPassword: false,
		},
		validators: {
			onSubmit: baseSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				let generated: string | null = null;

				if (isEditing && user) {
					const response = await updateUser(user.id, {
						name: value.name,
						email: value.email,
						roleIds: value.roleIds,
						resetPassword: value.resetPassword,
					});
					generated = response.generatedPassword ?? null;
					toast.add({
						type: "success",
						title: value.resetPassword ? m.userUpdatedAndPasswordReset() : m.userUpdated(),
					});
				} else {
					const response = await createUser({
						name: value.name,
						email: value.email,
						roleIds: value.roleIds,
					});
					generated = response.generatedPassword;
					toast.add({ type: "success", title: m.userCreated() });
				}

				if (generated) {
					setGeneratedPassword(generated);
					setIsGeneratedPasswordOpen(true);
				}

				setOpen(false);
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedSaveUser(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<>
			<ResponsiveDialog open={open} onOpenChange={(nextOpen) => setOpen(nextOpen)}>
				<ResponsiveDialogTrigger
					nativeButton={!asDropdownItem}
					render={
						asDropdownItem ? (
							<DropdownMenuItem disabled={disabled}>
								<Pencil className="h-4 w-4" />
								{m.edit()}
							</DropdownMenuItem>
						) : isEditing ? (
							<Button variant="ghost" size="icon" disabled={disabled}>
								<Pencil className="h-4 w-4" />
							</Button>
						) : (
							<Button disabled={disabled}>
								<Plus className="mr-2 h-4 w-4" />
								{m.addUser()}
							</Button>
						)
					}
				></ResponsiveDialogTrigger>
				<ResponsiveDialogContent className="sm:max-w-106.25">
					<ResponsiveDialogHeader>
						<ResponsiveDialogTitle>{isEditing ? m.editUser() : m.addUser()}</ResponsiveDialogTitle>
						<ResponsiveDialogDescription className="py-2">
							{isEditing ? m.editUserDescription() : m.addUserDescription()}
						</ResponsiveDialogDescription>
					</ResponsiveDialogHeader>

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
												placeholder={m.fullNamePlaceholder()}
												disabled={isManaged}
												autoFocus={!isManaged}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<form.Field
								name="email"
								children={(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.email()}</Label>
											<Input
												id={field.name}
												type="email"
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												placeholder={m.emailPlaceholder()}
												disabled={isManaged}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<form.Field
								name="roleIds"
								children={(field) => (
									<Field>
										<Label>{m.roles()}</Label>
										<FieldDescription>{m.userRolesDescription()}</FieldDescription>
										<div className="flex max-h-64 flex-col gap-2 overflow-y-auto">
											{roles?.map((role) => {
												const fromOIDC = oidcRoleIds.has(role.id);
												const checked = fromOIDC || field.state.value.includes(role.id);
												return (
													<FieldLabel key={role.id} htmlFor={`user-role-${role.id}`}>
														<Field orientation="horizontal" data-disabled={fromOIDC || undefined}>
															<Checkbox
																id={`user-role-${role.id}`}
																checked={checked}
																disabled={fromOIDC}
																onCheckedChange={(next) =>
																	field.handleChange(
																		next
																			? [...field.state.value, role.id]
																			: field.state.value.filter((id) => id !== role.id),
																	)
																}
															/>
															<FieldContent>
																<FieldTitle>
																	{role.builtIn === "admin" && <ShieldCheck className="size-4" />}
																	{roleName(role)}
																	{fromOIDC && (
																		<Badge variant="outline">
																			<Users />
																			{m.oidcGroup()}
																		</Badge>
																	)}
																</FieldTitle>
																{roleDescription(role) && (
																	<FieldDescription>{roleDescription(role)}</FieldDescription>
																)}
															</FieldContent>
														</Field>
													</FieldLabel>
												);
											})}
										</div>
									</Field>
								)}
							/>
							{isEditing && !isManaged && (
								<form.Field
									name="resetPassword"
									children={(field) => (
										<Field>
											<div className="flex items-start gap-2">
												<Checkbox
													id={field.name}
													checked={field.state.value}
													onCheckedChange={(checked) => field.handleChange(checked === true)}
													className="h-4 w-4 rounded border-gray-300"
												/>
												<div className="space-y-1">
													<Label htmlFor={field.name}>{m.resetPassword()}</Label>
													<p className="text-muted-foreground text-xs">
														{m.resetPasswordDescription()}
													</p>
												</div>
											</div>
										</Field>
									)}
								/>
							)}

							<div className="flex gap-2 pt-2">
								<Button type="submit" disabled={isSubmitting}>
									{isSubmitting ? m.savingDots() : isEditing ? m.updateUser() : m.createUser()}
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
				</ResponsiveDialogContent>
			</ResponsiveDialog>

			<CopyValueDialog
				open={isGeneratedPasswordOpen}
				onOpenChange={(nextOpen) => {
					setIsGeneratedPasswordOpen(nextOpen);
					if (!nextOpen) {
						setGeneratedPassword(null);
					}
				}}
				title={m.generatedPassword()}
				description={m.copyPasswordNow()}
				label={m.password()}
				value={generatedPassword ?? ""}
				inputId="generated-password"
				copyTitle={m.copyGeneratedPassword()}
			/>
		</>
	);
}
