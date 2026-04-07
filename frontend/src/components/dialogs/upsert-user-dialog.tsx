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

const baseSchema = z.object({
	name: z.string().min(3, "Name must be at least 3 characters").max(64),
	email: z.email("Must be a valid email address"),
	role: z.enum(["admin", "user"]),
	password: z.string(),
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

	const form = useForm({
		defaultValues: {
			name: user?.name ?? "",
			email: user?.email ?? "",
			role: (user?.role as "admin" | "user") ?? "user",
			password: "",
		},
		validators: {
			onSubmit: isEditing
				? baseSchema.refine((d) => !d.password || d.password.length >= 8, {
						message: "Password must be at least 8 characters",
						path: ["password"],
					})
				: baseSchema.refine((d) => d.password.length >= 8, {
						message: "Password must be at least 8 characters",
						path: ["password"],
					}),
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				if (isEditing && user) {
					await updateUser(user.id, {
						name: value.name,
						email: value.email,
						role: value.role,
						password: value.password || undefined,
					});
					toast.success("User updated");
				} else {
					await createUser({
						name: value.name,
						email: value.email,
						role: value.role,
						password: value.password,
					});
					toast.success("User created");
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
		<Dialog open={open} onOpenChange={(open) => setOpen(open)}>
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
							? "Update user details and permissions."
							: "Create a new local user account."}
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
							name="password"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>
											Password
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
											autoComplete="new-password"
											placeholder={isEditing ? "••••••••" : "Min. 8 characters"}
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
	);
}
