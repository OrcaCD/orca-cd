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
import { m } from "@/lib/paraglide/messages";

const applicationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationApplicationNameRequired())
		.max(128, m.validationApplicationNameMaxLength()),
	repositoryId: z.string().min(1, m.validationRepositoryRequired()),
	agentId: z.string().min(1, m.validationAgentRequired()),
	branch: z
		.string()
		.trim()
		.min(1, m.validationBranchRequired())
		.max(256, m.validationBranchMaxLength()),
	path: z.string().trim().min(1, m.validationPathRequired()).max(512, m.validationPathMaxLength()),
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
					toast.success(m.applicationUpdated());
				} else {
					await createApplication(value);
					toast.success(m.applicationCreated());
				}
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveApplication());
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
						{m.newApplication()}
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? m.editApplication() : m.addApplication()}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing ? m.editApplicationDescription() : m.addApplicationDescription()}
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
										<Label htmlFor={field.name}>{m.name()}</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.applicationNamePlaceholder()}
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
											<FieldLabel htmlFor="repository-select">{m.repository()}</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={field.handleChange}
										>
											<SelectTrigger
												id="repository-select"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder={m.selectRepository()} />
											</SelectTrigger>
											<SelectContent position="item-aligned">
												{isReposLoading ? (
													<div className="p-2">{m.loadingDots()}</div>
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
											<FieldLabel htmlFor="agent-select">{m.columnAgent()}</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={field.handleChange}
										>
											<SelectTrigger
												id="agent-select"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder={m.selectAgent()} />
											</SelectTrigger>
											<SelectContent position="item-aligned">
												{isAgentsLoading ? (
													<div className="p-2">{m.loadingDots()}</div>
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
										<Label htmlFor={field.name}>{m.branch()}</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.branchPlaceholder()}
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
										<Label htmlFor={field.name}>{m.path()}</Label>
										<Input
											id={field.name}
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.pathPlaceholder()}
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting
									? m.savingDots()
									: isEditing
										? m.updateApplication()
										: m.addApplication()}
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
