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
import { createRepository, updateRepository, type Repository } from "@/lib/repsitories";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";

const repositorySchema = z.object({
	name: z.string().min(1, "Name is required").max(100, "Name must be at most 100 characters"),
	url: z.url({ error: "Repository URL must be a valid URL", protocol: /^https?$/ }),
});

export default function UpsertRepositoryDialog({
	repository,
	asDropdownItem = false,
}: {
	repository: Repository | null;
	asDropdownItem?: boolean;
}) {
	const isEditing = !!repository;
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [open, setOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: repository?.name ?? "",
			url: repository?.url ?? "",
		},
		validators: {
			onSubmit: repositorySchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				if (isEditing && repository) {
					await updateRepository(repository.id, {
						name: value.name,
						url: value.url,
					});
					toast.success("Repository updated");
				} else {
					await createRepository({
						name: value.name,
						url: value.url,
					});
					toast.success("Repository connected");
				}
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save repository");
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
						Edit
					</DropdownMenuItem>
				) : isEditing ? (
					<Button variant="ghost" size="icon">
						<Pencil className="h-4 w-4" />
					</Button>
				) : (
					<Button>
						<Plus className="mr-2 h-4 w-4" />
						Connect Repository
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? "Edit Repository" : "Connect Repository"}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing
							? "Update the repository configuration."
							: "Connect a Git repository to start tracking deployments."}
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
											placeholder='e.g. "org/api-gateway"'
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>
						<form.Field
							name="url"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Repository URL</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder="https://github.com/org/repo"
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting
									? "Saving..."
									: isEditing
										? "Update Repository"
										: "Connect Repository"}
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
