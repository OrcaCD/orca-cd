// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { Button } from "../ui/button";
import z from "zod";
import { createApplication, updateApplication, type Application } from "@/lib/applications";
import { useState } from "react";
import { toast } from "sonner";
import { useForm } from "@tanstack/react-form";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "../ui/dialog";
import { DropdownMenuItem } from "../ui/dropdown-menu";
import { Field, FieldContent, FieldError, FieldGroup, FieldLabel } from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";
import { useFetch } from "@/lib/api";
import type { Agent } from "@/lib/agents";
import type { Repository } from "@/lib/repsitories";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";

const applicationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, "Name is required")
		.max(128, "Name must be at most 128 characters"),
	repositoryId: z.string().min(1, "Repository is required"),
	agentId: z.string().min(1, "Agent is required"),
	branch: z
		.string()
		.trim()
		.min(1, "Branch is required")
		.max(256, "Branch must be at most 256 characters"),
	path: z
		.string()
		.trim()
		.min(1, "Path is required")
		.max(512, "Path must be at most 512 characters"),
});

export default function UpsertApplicationDialog({
	application,
	asDropdownItem = false,
}: {
	application: Application | null;
	asDropdownItem?: boolean;
}) {
	const isEditing = !!application;
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [open, setOpen] = useState(false);

	const { data: agents, isLoading: isAgentsLoading } = useFetch<Agent[]>("/agents");
	const { data: repos, isLoading: isReposLoading } = useFetch<Repository[]>("/repositories");

	const form = useForm({
		defaultValues: {
			name: application?.name ?? "",
			repositoryId: application?.repositoryId ?? "",
			agentId: application?.agentId ?? "",
			branch: application?.branch ?? "",
			path: application?.path ?? "",
		},
		validators: {
			onSubmit: applicationSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				if (isEditing && application) {
					await updateApplication(application.id, value);
					toast.success("Application updated");
				} else {
					await createApplication(value);
					toast.success("Application created");
				}
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save application");
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
						<Plus className="h-4 w-4" />
						New Application
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? "Edit Application" : "Add Application"}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing
							? "Update the application configuration."
							: "Add a new application to monitor and manage."}
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
											placeholder='e.g. "notifications-service"'
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<form.Field
							name="repositoryId"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field orientation="responsive" data-invalid={isInvalid}>
										<FieldContent>
											<FieldLabel htmlFor="form-tanstack-select-language">Repository</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={field.handleChange}
										>
											<SelectTrigger
												id="form-tanstack-select-language"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder="Select" />
											</SelectTrigger>
											<SelectContent position="item-aligned">
												{isReposLoading ? (
													<div className="p-2">Loading...</div>
												) : (
													repos?.map((repo) => (
														<SelectItem key={repo.id} value={repo.id}>
															{repo.name}
														</SelectItem>
													))
												)}
											</SelectContent>
										</Select>
									</Field>
								);
							}}
						/>

						<form.Field
							name="agentId"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field orientation="responsive" data-invalid={isInvalid}>
										<FieldContent>
											<FieldLabel htmlFor="form-tanstack-select-language">Agent</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={field.handleChange}
										>
											<SelectTrigger
												id="form-tanstack-select-language"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder="Select" />
											</SelectTrigger>
											<SelectContent position="item-aligned">
												{isAgentsLoading ? (
													<div className="p-2">Loading...</div>
												) : (
													agents?.map((agent) => (
														<SelectItem key={agent.id} value={agent.id}>
															{agent.name}
														</SelectItem>
													))
												)}
											</SelectContent>
										</Select>
									</Field>
								);
							}}
						/>

						<form.Field
							name="branch"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Branch</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder='e.g. "main"'
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<form.Field
							name="path"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>Path</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder='e.g. "/docker-compose.yml"'
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting ? "Saving..." : isEditing ? "Update Application" : "Add Application"}
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
