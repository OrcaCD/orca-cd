// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
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
import { createAgent, updateAgent, type Agent } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";
import LucideIconPicker, { type LucideIconName } from "@/components/lucide-icon-picker";
import CopyButton from "@/components/copy-btn";
import { useHighlightedCode } from "@/hooks/use-highlighted-code";
import {
	CodeBlock,
	CodeBlockContent,
	CodeBlockCopyButton,
	CodeBlockLine,
} from "@/components/ui/code-block";

function buildAgentComposeFile(hubUrl: string, authToken: string): string {
	return `services:
  agent:
    image: ghcr.io/orcacd/agent:latest
    restart: unless-stopped
    environment:
      HUB_URL: ${JSON.stringify(hubUrl)}
      AUTH_TOKEN: ${JSON.stringify(authToken)}
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    use_api_socket: true
    volumes:
      - ./deployments:/deployments
`;
}

function AgentSetupDialog({
	open,
	onOpenChange,
	hubUrl,
	onHubUrlChange,
	authToken,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	hubUrl: string;
	onHubUrlChange: (hubUrl: string) => void;
	authToken: string;
}) {
	const composeFile = buildAgentComposeFile(hubUrl, authToken);
	const composeLines = useHighlightedCode(composeFile, "yaml");

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-2xl">
				<DialogHeader className="shrink-0">
					<DialogTitle>{m.agentSetupTitle()}</DialogTitle>
					<DialogDescription>{m.agentSetupDescription()}</DialogDescription>
				</DialogHeader>

				<div className="shrink-0 space-y-2">
					<Label htmlFor="agent-hub-url">{m.hubUrl()}</Label>
					<Input
						id="agent-hub-url"
						type="url"
						value={hubUrl}
						onChange={(event) => onHubUrlChange(event.target.value)}
					/>
					<p className="text-xs text-muted-foreground">{m.agentHubUrlDescription()}</p>
				</div>

				<div className="shrink-0 space-y-2">
					<Label htmlFor="agent-auth-token">{m.authToken()}</Label>
					<div className="flex items-center gap-2">
						<Input id="agent-auth-token" value={authToken} readOnly className="font-mono" />
						<CopyButton text={authToken} title={m.copyAgentAuthToken()} />
					</div>
					<p className="text-xs text-muted-foreground">{m.copyTokenNow()}</p>
				</div>

				<div className="flex min-h-0 flex-1 flex-col gap-2">
					<div className="flex items-center justify-between gap-2">
						<p className="text-sm font-medium">{m.agentComposeFile()}</p>
						<CodeBlockCopyButton value={composeFile} />
					</div>
					<CodeBlock className="min-h-0 flex-1">
						<CodeBlockContent className="min-h-0 flex-1" lineNumbers>
							{composeLines.map((line, index) => (
								<CodeBlockLine key={index}>{line}</CodeBlockLine>
							))}
						</CodeBlockContent>
					</CodeBlock>
				</div>

				<DialogFooter className="shrink-0">
					<Button type="button" onClick={() => onOpenChange(false)}>
						{m.done()}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

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
	const [authToken, setAuthToken] = useState<string | null>(null);
	const [isAgentSetupOpen, setIsAgentSetupOpen] = useState(false);
	const [hubUrl, setHubUrl] = useState("");

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
					if (response.authToken) {
						setAuthToken(response.authToken);
						setHubUrl(window.location.origin);
						setIsAgentSetupOpen(true);
					}
					toast.add({ type: "success", title: m.agentConnected() });
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
				open={isAgentSetupOpen}
				onOpenChange={(nextOpen) => {
					setIsAgentSetupOpen(nextOpen);
					if (!nextOpen) {
						setAuthToken(null);
						setHubUrl("");
					}
				}}
				hubUrl={hubUrl}
				onHubUrlChange={setHubUrl}
				authToken={authToken ?? ""}
			/>
		</>
	);
}
