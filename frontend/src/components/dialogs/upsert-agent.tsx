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
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "@/components/ui/toast";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "../ui/field";
import { Label } from "../ui/label";
import { Input } from "../ui/input";
import { createAgent, updateAgent, type Agent, type AgentWithTokenResponse } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import LucideIconPicker, { type LucideIconName } from "@/components/lucide-icon-picker";
import AgentSetupDialog from "./agent-setup-dialog";

const agentSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationAgentNameRequired())
		.max(128, m.validationAgentNameMaxLength()),
	icon: z.string(),
});

const defaultAgentIcon: LucideIconName = "server";

export default function UpsertAgentDialog({
	agent,
	asDropdownItem = false,
}: {
	agent: Agent | null;
	asDropdownItem?: boolean;
}) {
	const isEditing = !!agent;
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [open, setOpen] = useState(false);
	const [createdAgent, setCreatedAgent] = useState<AgentWithTokenResponse | null>(null);
	const [isAgentSetupOpen, setIsAgentSetupOpen] = useState(false);

	const form = useForm({
		defaultValues: {
			name: agent?.name ?? "",
			icon: agent?.icon ?? defaultAgentIcon,
		},
		validators: {
			onSubmit: agentSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				if (isEditing && agent) {
					await updateAgent(agent.id, {
						name: value.name,
						icon: value.icon,
					});
					toast.add({ type: "success", title: m.agentUpdated() });
				} else {
					const response = await createAgent({
						name: value.name,
						icon: value.icon,
					});
					setCreatedAgent(response);
					setIsAgentSetupOpen(true);
				}
				setOpen(false);
				form.reset();
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedSaveAgent(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	return (
		<>
			<Dialog open={open} onOpenChange={(open) => setOpen(open)}>
				<DialogTrigger
					nativeButton={!asDropdownItem}
					render={
						asDropdownItem ? (
							<DropdownMenuItem>
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
								{m.addAgent()}
							</Button>
						)
					}
				></DialogTrigger>
				<DialogContent className="sm:max-w-106.25">
					<DialogHeader>
						<DialogTitle className="flex items-center gap-2">
							{isEditing ? m.editAgent() : m.addAgent()}
						</DialogTitle>
						<DialogDescription className="py-2">
							{isEditing ? m.editAgentDescription() : m.addAgentDescription()}
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
												placeholder={m.agentNamePlaceholder()}
												autoFocus
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							/>

							<form.Field
								name="icon"
								children={(field) => (
									<Field>
										<Label>{m.icon()}</Label>
										<LucideIconPicker
											value={field.state.value as LucideIconName}
											onValueChange={field.handleChange}
											placeholder={m.selectIcon()}
											emptyMessage={m.noIconsFound()}
										/>
									</Field>
								)}
							/>

							<div className="flex gap-2 pt-2">
								<Button type="submit" disabled={isSubmitting}>
									{isSubmitting ? m.savingDots() : isEditing ? m.update() : m.addAgent()}
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

			<AgentSetupDialog
				agent={createdAgent}
				open={isAgentSetupOpen}
				onOpenChange={setIsAgentSetupOpen}
				onOpenChangeComplete={(nextOpen) => {
					// Keep the token in memory only while the dialog is shown.
					if (!nextOpen) {
						setCreatedAgent(null);
					}
				}}
			/>
		</>
	);
}
