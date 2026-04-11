// oxlint-disable react/no-children-prop
import { Pencil, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import {
	createRepository,
	updateRepository,
	type Repository,
	type RepositoryProvider,
} from "@/lib/repsitories";
import React, { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import {
	Field,
	FieldError,
	FieldGroup,
	FieldContent,
	FieldLabel,
	FieldTitle,
} from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { defineStepper } from "@stepperize/react";
import { useStepItemContext, type StepStatus } from "@stepperize/react/primitives";
import { cn } from "@/lib/utils";

const PROVIDERS = [
	{ id: "github", label: "GitHub", disabled: false, icon: "github" },
	{ id: "gitlab", label: "GitLab", disabled: true, icon: "gitlab" },
	{ id: "bitbucket", label: "Bitbucket", disabled: true, icon: "bitbucket" },
	{ id: "azure-devops", label: "Azure DevOps", disabled: true, icon: "azure-devops" },
	{ id: "gitea", label: "Gitea", disabled: true, icon: "gitea" },
	{ id: "generic", label: "Generic", disabled: true, icon: "git" },
] as const;

const { Stepper } = defineStepper({ id: "provider" }, { id: "repository" }, { id: "sync" });

const repositorySchema = z.object({
	url: z.url({ error: "Repository URL must be a valid URL", protocol: /^https?$/ }),
	provider: z.enum(["github", "gitlab", "generic"]),
	authToken: z.string().trim().max(1024, "Auth token must be at most 1024 characters"),
});

// Only used for ReturnType inference — never called at runtime
function useRepoForm() {
	return useForm({
		defaultValues: { url: "", provider: "github" as RepositoryProvider, authToken: "" },
		validators: { onSubmit: repositorySchema },
		// oxlint-disable-next-line no-empty-function
		onSubmit: async () => {},
	});
}
type RepoFormApi = ReturnType<typeof useRepoForm>;

const StepperTriggerWrapper = () => {
	const item = useStepItemContext();
	const isInactive = item.status === "inactive";

	return (
		<Stepper.Trigger
			render={(domProps) => (
				<Button
					className="rounded-full"
					variant={isInactive ? "secondary" : "default"}
					size="icon"
					{...domProps}
				>
					<Stepper.Indicator>{item.index + 1}</Stepper.Indicator>
				</Button>
			)}
		/>
	);
};

const StepperSeparatorWithStatus = ({
	status,
	isLast,
}: {
	status: StepStatus;
	isLast: boolean;
}) => {
	if (isLast) {
		return null;
	}
	return (
		<Stepper.Separator
			orientation="horizontal"
			data-status={status}
			className="self-center bg-muted data-[status=success]:bg-primary data-disabled:opacity-50 transition-all duration-300 ease-in-out data-[orientation=horizontal]:h-0.5 data-[orientation=horizontal]:min-w-4 data-[orientation=horizontal]:flex-1"
		/>
	);
};

function ProviderStepContent({ form }: { form: RepoFormApi }) {
	return (
		<form.Field name="provider">
			{(field) => (
				<>
					<p className="text-muted-foreground text-sm mb-4">
						Select the Git provider for this repository.
					</p>
					<RadioGroup
						value={field.state.value}
						onValueChange={(v) => field.handleChange(v as RepositoryProvider)}
						className="grid grid-cols-3 gap-3"
					>
						{PROVIDERS.map((p) => {
							const inputId = `provider-${p.id}`;
							return (
								<FieldLabel
									key={p.id}
									htmlFor={inputId}
									className={cn(
										"transition-colors",
										p.disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
									)}
								>
									<Field
										className="relative aspect-square items-center justify-center p-4 text-center"
										data-disabled={p.disabled ? "true" : undefined}
									>
										<RadioGroupItem
											value={p.id}
											id={inputId}
											disabled={p.disabled}
											className="hidden"
										/>
										<FieldContent className="items-center justify-center gap-2">
											<img
												src={`/assets/icons/${p.icon}.svg`}
												alt={p.label}
												className="h-10 w-10"
											/>
											<FieldTitle className="justify-center text-base">{p.label}</FieldTitle>
										</FieldContent>
									</Field>
								</FieldLabel>
							);
						})}
					</RadioGroup>
				</>
			)}
		</form.Field>
	);
}

function RepositoryStepContent({ form }: { form: RepoFormApi }) {
	return (
		<FieldGroup>
			<form.Field
				name="url"
				validators={{ onSubmit: repositorySchema.shape.url }}
			>
				{(field) => {
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
								autoFocus
							/>
							{isInvalid && <FieldError errors={field.state.meta.errors} />}
						</Field>
					);
				}}
			</form.Field>
			<form.Field
				name="authToken"
				validators={{ onSubmit: repositorySchema.shape.authToken }}
			>
				{(field) => {
					const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
					return (
						<Field data-invalid={isInvalid}>
							<Label htmlFor={field.name}>Auth Token (Optional)</Label>
							<Input
								id={field.name}
								type="password"
								value={field.state.value}
								onBlur={field.handleBlur}
								onChange={(e) => field.handleChange(e.target.value)}
								placeholder="Paste a personal access token"
							/>
							<p className="text-muted-foreground text-xs">
								Recommended, but not required for public repositories.
							</p>
							{isInvalid && <FieldError errors={field.state.meta.errors} />}
						</Field>
					);
				}}
			</form.Field>
		</FieldGroup>
	);
}

function StepperNavigation({
	stepper,
	onNext,
	handleClose,
	isSubmitting,
	isEditing,
}: {
	stepper: { state: { current: { index: number; data: { id: string } }; isLast: boolean } };
	onNext: (stepId: string, advance: () => void) => void;
	handleClose: () => void;
	isSubmitting: boolean;
	isEditing: boolean;
}) {
	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" onClick={handleClose} disabled={isSubmitting}>
				Cancel
			</Button>
			<div className="flex gap-2">
				{stepper.state.current.index > 0 && (
					<Stepper.Prev
						render={(domProps) => (
							<Button type="button" variant="outline" {...domProps}>
								Previous
							</Button>
						)}
					/>
				)}
				{stepper.state.isLast ? (
					<Button type="submit" disabled={isSubmitting}>
						{isSubmitting ? "Saving..." : isEditing ? "Update Repository" : "Connect Repository"}
					</Button>
				) : (
					<Stepper.Next
						render={(domProps) => (
							<Button
								type="button"
								onClick={(e) => onNext(stepper.state.current.data.id, () => domProps.onClick?.(e))}
							>
								Next
							</Button>
						)}
					/>
				)}
			</div>
		</div>
	);
}

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
			url: repository?.url ?? "",
			provider: repository?.provider ?? "github",
			authToken: "",
		},
		validators: {
			onSubmit: repositorySchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				const authToken = value.authToken?.trim() ? value.authToken.trim() : undefined;
				const authMethod = authToken ? "token" : "none";

				if (isEditing) {
					await updateRepository(repository.id, {
						url: value.url,
						authMethod,
						authToken,
						syncType: repository.syncType,
						pollingIntervalSeconds: repository.pollingIntervalSeconds ?? undefined,
					});
					toast.success("Repository updated");
				} else {
					await createRepository({
						url: value.url,
						provider: value.provider,
						authMethod,
						authToken,
						syncType: "manual",
					});
					toast.success("Repository created");
				}
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : "Failed to save repository");
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const handleClose = () => {
		setOpen(false);
		form.reset();
	};

	async function handleNext(stepId: string, advance: () => void) {
		if (stepId === "repository") {
			const [urlErrors, tokenErrors] = await Promise.all([
				form.validateField("url", "submit"),
				form.validateField("authToken", "submit"),
			]);
			if (urlErrors?.length || tokenErrors?.length) {
				return;
			}
		}
		advance();
	}

	return (
		<Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : handleClose())}>
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
						Connect Repository
					</Button>
				)}
			</DialogTrigger>
			<DialogContent onPointerDownOutside={(e) => e.preventDefault()} className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>{isEditing ? "Edit Repository" : "Connect Repository"}</DialogTitle>
				</DialogHeader>
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root key={String(open)} className="w-full space-y-6" orientation="horizontal">
						{({ stepper }) => (
							<>
								<Stepper.List className="flex list-none gap-2 flex-row items-center justify-between">
									{stepper.state.all.map((stepData, index) => {
										const currentIndex = stepper.state.current.index;
										const status: StepStatus =
											index < currentIndex
												? "success"
												: index === currentIndex
													? "active"
													: "inactive";
										const isLast = index === stepper.state.all.length - 1;
										return (
											<React.Fragment key={stepData.id}>
												<Stepper.Item
													step={stepData.id}
													className="group peer relative flex shrink-0 items-center gap-2"
												>
													<StepperTriggerWrapper />
												</Stepper.Item>
												<StepperSeparatorWithStatus status={status} isLast={isLast} />
											</React.Fragment>
										);
									})}
								</Stepper.List>

								{stepper.flow.switch({
									provider: () => (
										<Stepper.Content
											step="provider"
											render={(props) => (
												<div {...props}>
													<ProviderStepContent form={form} />
												</div>
											)}
										/>
									),
									repository: () => (
										<Stepper.Content
											step="repository"
											render={(props) => (
												<div {...props}>
													<RepositoryStepContent form={form} />
												</div>
											)}
										/>
									),
								})}

								<StepperNavigation
									stepper={stepper}
									onNext={handleNext}
									handleClose={handleClose}
									isSubmitting={isSubmitting}
									isEditing={isEditing}
								/>
							</>
						)}
					</Stepper.Root>
				</form>
			</DialogContent>
		</Dialog>
	);
}
