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
import { createUser, updateUser, type UserDetail } from "@/lib/users";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldContent, FieldError, FieldGroup, FieldLabel, FieldTitle } from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";
import { RadioGroup, RadioGroupItem } from "../ui/radio-group";
import { Checkbox } from "../ui/checkbox";
import CopyValueDialog from "./copy-value-dialog";

const baseSchema = z.object({
	name: z.string().trim().min(3, "Name must be at least 3 characters").max(64),
	email: z.email("Must be a valid email address").trim(),
	role: z.enum(["admin", "user"]),
	resetPassword: z.boolean(),
});

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
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [open, setOpen] = useState(false);
	const [generatedPassword, setGeneratedPassword] = useState<string | null>(null);
	const [isGeneratedPasswordOpen, setIsGeneratedPasswordOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: user?.name ?? "",
			email: user?.email ?? "",
			role: (user?.role as "admin" | "user") ?? "user",
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
						role: value.role,
						resetPassword: value.resetPassword,
					});
					generated = response.generatedPassword ?? null;
					toast.success(value.resetPassword ? "User updated and password reset" : "User updated");
				} else {
					const response = await createUser({
						name: value.name,
						email: value.email,
						role: value.role,
					});
					generated = response.generatedPassword;
					toast.success("User created");
				}

				if (generated) {
					setGeneratedPassword(generated);
					setIsGeneratedPasswordOpen(true);
				}

				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save user");
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<>
			<Dialog open={open} onOpenChange={(nextOpen) => setOpen(nextOpen)}>
				<DialogTrigger asChild>
					{asDropdownItem ? (
						<DropdownMenuItem onSelect={(e) => e.preventDefault()} disabled={disabled}>
							<Pencil className="h-4 w-4" />
							Edit
						</DropdownMenuItem>
					) : isEditing ? (
						<Button variant="ghost" size="icon" disabled={disabled}>
							<Pencil className="h-4 w-4" />
						</Button>
					) : (
						<Button disabled={disabled}>
							<Plus className="mr-2 h-4 w-4" />
							Add User
						</Button>
					)}
				</DialogTrigger>
				<DialogContent className="sm:max-w-106.25">
					<DialogHeader>
						<DialogTitle>{isEditing ? "Edit User" : "Add User"}</DialogTitle>
						<DialogDescription className="py-2">
							{isEditing
								? "Update user details and permissions. Optionally reset the password."
								: "Create a new local user account. A temporary password will be generated."}
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
											<Label htmlFor={field.name}>Name</Label>
											<Input
												id={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												placeholder="Full name"
												autoFocus
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
											<Label htmlFor={field.name}>Email</Label>
											<Input
												id={field.name}
												type="email"
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => field.handleChange(e.target.value)}
												placeholder="user@example.com"
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>
							<form.Field
								name="role"
								children={(field) => (
									<Field>
										<Label htmlFor={field.name}>Role</Label>
										<div className="flex gap-4">
											<RadioGroup
												name={field.name}
												value={field.state.value}
												onValueChange={(value) => field.handleChange(value as "admin" | "user")}
											>
												<FieldLabel>
													<Field
														orientation="horizontal"
														data-invalid={field.state.meta.isTouched && !field.state.meta.isValid}
													>
														<FieldContent>
															<FieldTitle>Admin</FieldTitle>
														</FieldContent>
														<RadioGroupItem
															aria-invalid={field.state.meta.isTouched && !field.state.meta.isValid}
															value="admin"
														/>
													</Field>
												</FieldLabel>
												<FieldLabel>
													<Field
														orientation="horizontal"
														data-invalid={field.state.meta.isTouched && !field.state.meta.isValid}
													>
														<FieldContent>
															<FieldTitle>User</FieldTitle>
														</FieldContent>
														<RadioGroupItem
															aria-invalid={field.state.meta.isTouched && !field.state.meta.isValid}
															value="user"
														/>
													</Field>
												</FieldLabel>
											</RadioGroup>
										</div>
									</Field>
								)}
							/>
							{isEditing && (
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
													<Label htmlFor={field.name}>Reset password</Label>
													<p className="text-muted-foreground text-xs">
														Generate a new password and require the user to change it after sign-in.
													</p>
												</div>
											</div>
										</Field>
									)}
								/>
							)}

							<div className="flex gap-2 pt-2">
								<Button type="submit" disabled={isSubmitting}>
									{isSubmitting ? "Saving..." : isEditing ? "Update User" : "Create User"}
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

			<CopyValueDialog
				open={isGeneratedPasswordOpen}
				onOpenChange={(nextOpen) => {
					setIsGeneratedPasswordOpen(nextOpen);
					if (!nextOpen) {
						setGeneratedPassword(null);
					}
				}}
				title="Generated Password"
				description="Copy this password now. It will not be shown again."
				label="Password"
				value={generatedPassword ?? ""}
				inputId="generated-password"
				copyTitle="Copy generated password"
			/>
		</>
	);
}
