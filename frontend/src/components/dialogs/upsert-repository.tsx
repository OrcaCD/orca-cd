// oxlint-disable react/no-children-prop
import { Loader2Icon, PencilIcon, PlusIcon } from "lucide-react";
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
	testRepositoryConnection,
	type RepositorySyncType,
	getGitProviderIconPath,
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
	FieldDescription,
} from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { defineStepper } from "@stepperize/react";
import { useStepItemContext, type StepStatus } from "@stepperize/react/primitives";
import { cn } from "@/lib/utils";
import ErrorAlert from "../error-alert";
import SuccessAlert from "../success-alert";

const PROVIDERS = [
	{ id: "github", label: "GitHub", disabled: false },
	{ id: "gitlab", label: "GitLab", disabled: true },
	{ id: "bitbucket", label: "Bitbucket", disabled: true },
	{ id: "azure_devops", label: "Azure DevOps", disabled: true },
	{ id: "gitea", label: "Gitea", disabled: true },
	{ id: "generic", label: "Generic", disabled: true },
] as const;

const syncTypes = [
	{
		id: "webhook",
		label: "Webhook (Recommended)",
		description: "Real-time updates with minimal resource usage.",
	},
	{
		id: "polling",
		label: "Polling",
		description: "Periodic checks for updates. May have a delay.",
	},
	{ id: "manual", label: "Manual", description: "Updates must be triggered manually." },
] as const satisfies { id: RepositorySyncType; label: string; description: string }[];

const { Stepper } = defineStepper(
	{ id: "provider" },
	{ id: "repository" },
	{ id: "syncType" },
	{ id: "summary" },
);

const repositorySchema = z.object({
	url: z.url({ error: "Repository URL must be a valid URL", protocol: /^https?$/ }),
	provider: z.enum(["github", "gitlab", "generic"]),
	authToken: z.string().trim().max(1024, "Auth token must be at most 1024 characters"),
	syncType: z.enum(["webhook", "polling", "manual"]),
});

// Only used for ReturnType inference — never called at runtime
function useRepoForm() {
	return useForm({
		defaultValues: {
			url: "",
			provider: "github" as RepositoryProvider,
			authToken: "",
			syncType: "webhook" as RepositorySyncType,
		},
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
					className="rounded-full cursor-default"
					variant={isInactive ? "secondary" : "default"}
					size="icon"
					{...domProps}
					onClick={(e) => {
						e.preventDefault();
					}}
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
											<img src={getGitProviderIconPath(p.id)} alt={p.label} className="h-10 w-10" />
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

function RepositoryStepContent({ form, error }: { form: RepoFormApi; error: string | undefined }) {
	return (
		<>
			<p className="text-muted-foreground text-sm mb-4">
				Enter the repository URL and an auth token.
			</p>
			<FieldGroup>
				<form.Field name="url" validators={{ onSubmit: repositorySchema.shape.url }}>
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
				<form.Field name="authToken" validators={{ onSubmit: repositorySchema.shape.authToken }}>
					{(field) => {
						const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
						return (
							<Field data-invalid={isInvalid}>
								<Label htmlFor={field.name}>Auth Token</Label>
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
				{error && <ErrorAlert title="Can't connect to repository" description={error} />}
			</FieldGroup>
		</>
	);
}

function SyncTypeStepContent({ form }: { form: RepoFormApi }) {
	return (
		<>
			<p className="text-muted-foreground text-sm mb-4">
				Decide how OrcaCD should check for updates in this repository.
			</p>
			<FieldGroup>
				<form.Field name="syncType" validators={{ onSubmit: repositorySchema.shape.syncType }}>
					{(field) => (
						<RadioGroup
							value={field.state.value}
							onBlur={field.handleBlur}
							onValueChange={(v) => field.handleChange(v as RepositorySyncType)}
							className="w-fit"
						>
							{syncTypes.map((type) => (
								<FieldLabel
									htmlFor={`syncType-${type.id}`}
									key={type.id}
									className="cursor-pointer transition-colors"
								>
									<Field orientation="horizontal">
										<FieldContent className="ps-1">
											<FieldTitle>{type.label}</FieldTitle>
											<FieldDescription>{type.description}</FieldDescription>
										</FieldContent>
										<RadioGroupItem value={type.id} id={`syncType-${type.id}`} />
									</Field>
								</FieldLabel>
							))}
						</RadioGroup>
					)}
				</form.Field>
			</FieldGroup>
		</>
	);
}

function SyncTypeSummaryContent({ isEditing }: { isEditing: boolean }) {
	return (
		<>
			<SuccessAlert
				title={isEditing ? "Repository updated" : "Repository connected"}
				description={
					isEditing
						? "The repository has been successfully updated."
						: "The repository has been successfully connected."
				}
			/>
			<div className="text-destructive">TODO: Show Webhook setup token here</div>
		</>
	);
}

function StepperNavigation({
	stepper,
	onNext,
	handleClose,
	isEditing,
}: {
	stepper: { state: { current: { index: number; data: { id: string } }; isLast: boolean } };
	onNext: (stepId: string, advance: () => void) => void;
	handleClose: () => void;
	isEditing: boolean;
}) {
	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" onClick={handleClose}>
				{stepper.state.isLast ? "Close" : "Cancel"}
			</Button>
			<div className="flex gap-2">
				{stepper.state.current.index > 0 && !stepper.state.isLast && (
					<Stepper.Prev
						render={(domProps) => (
							<Button type="button" variant="outline" {...domProps}>
								Previous
							</Button>
						)}
					/>
				)}
				{stepper.state.current.data.id === "syncType" ? (
					<Button type="submit">{isEditing ? "Update Repository" : "Connect Repository"}</Button>
				) : !stepper.state.isLast ? (
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
				) : null}
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
	const [isLoading, setIsLoading] = useState(false);
	const [open, setOpen] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const stepperRef = React.useRef<{ navigation: { next: () => void } } | null>(null);

	const form = useForm({
		defaultValues: {
			url: repository?.url ?? "",
			provider: repository?.provider ?? "github",
			authToken: "",
			syncType: repository?.syncType ?? "webhook",
		},
		validators: {
			onSubmit: repositorySchema,
		},
		onSubmit: async ({ value }) => {
			setIsLoading(true);
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
				} else {
					await createRepository({
						url: value.url,
						provider: value.provider,
						authMethod,
						authToken,
						syncType: value.syncType,
					});
				}

				stepperRef.current?.navigation.next();
			} catch (err: any) {
				toast.error(err?.message || "An unexpected error occurred");
			} finally {
				setIsLoading(false);
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

			const url = form.getFieldValue("url");
			const authToken = form.getFieldValue("authToken")?.trim() || undefined;
			const provider = form.getFieldValue("provider");

			// To-Do support other auth methods
			const authMethod = authToken?.length ? "token" : "none";

			setIsLoading(true);

			try {
				await testRepositoryConnection({ provider, url, authToken: authToken, authMethod });
			} catch (err: any) {
				setError(
					err?.message || "Failed to connect to repository. Please check the URL and auth token.",
				);
				return;
			} finally {
				setIsLoading(false);
			}
		}

		setError(undefined);
		advance();
	}

	return (
		<Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : handleClose())}>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
						<PencilIcon className="h-4 w-4" />
						Edit
					</DropdownMenuItem>
				) : isEditing ? (
					<Button variant="ghost" size="icon">
						<PencilIcon className="h-4 w-4" />
					</Button>
				) : (
					<Button>
						<PlusIcon className="h-4 w-4" />
						Add Repository
					</Button>
				)}
			</DialogTrigger>
			<DialogContent
				onPointerDownOutside={(e) => e.preventDefault()}
				className="sm:max-w-md overflow-hidden"
			>
				{isLoading && (
					<div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-background/80 backdrop-blur-sm">
						<Loader2Icon className="h-8 w-8 animate-spin text-primary" />
						<p className="text-sm text-muted-foreground">Loading…</p>
					</div>
				)}
				<DialogHeader>
					<DialogTitle>{isEditing ? "Edit Repository" : "Add Repository"}</DialogTitle>
				</DialogHeader>
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root key={String(open)} className="w-full space-y-6" orientation="horizontal">
						{({ stepper }) => {
							stepperRef.current = stepper;
							return (
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
														<RepositoryStepContent form={form} error={error} />
													</div>
												)}
											/>
										),
										syncType: () => (
											<Stepper.Content
												step="syncType"
												render={(props) => (
													<div {...props}>
														<SyncTypeStepContent form={form} />
													</div>
												)}
											/>
										),
										summary: () => (
											<Stepper.Content
												step="summary"
												render={(props) => (
													<div {...props}>
														<SyncTypeSummaryContent isEditing={isEditing} />
													</div>
												)}
											/>
										),
									})}

									<StepperNavigation
										stepper={stepper}
										onNext={handleNext}
										handleClose={handleClose}
										isEditing={isEditing}
									/>
								</>
							);
						}}
					</Stepper.Root>
				</form>
			</DialogContent>
		</Dialog>
	);
}
