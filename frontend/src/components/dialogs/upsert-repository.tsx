// oxlint-disable react/no-children-prop
import { EyeIcon, EyeOffIcon, Loader2Icon, PencilIcon, PlusIcon } from "lucide-react";
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
	getGitProviderIconClass,
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
import ErrorAlert from "../alerts/error-alert";
import SuccessAlert from "../alerts/success-alert";
import CopyButton from "../copy-btn";

const PROVIDERS = [
	{ id: "github", label: "GitHub", disabled: false, placeholderUrl: "https://github.com/org/repo" },
	{
		id: "gitlab",
		label: "GitLab",
		disabled: false,
		placeholderUrl: "https://gitlab.com/group/project",
	},
	{
		id: "gitea",
		label: "Gitea",
		disabled: false,
		placeholderUrl: "https://gitea.example.com/org/repo",
	},
	{
		id: "bitbucket",
		label: "Bitbucket",
		disabled: true,
		placeholderUrl: "https://bitbucket.org/workspace/repository",
	},
	{
		id: "azure_devops",
		label: "Azure DevOps",
		disabled: true,
		placeholderUrl: "https://dev.azure.com/org/project/_git/repository",
	},
	{
		id: "generic",
		label: "Generic",
		disabled: true,
		placeholderUrl: "https://git.example.com/org/repo",
	},
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
	url: z.url({ error: "Repository URL must be a valid URL", protocol: /^https?$/ }).trim(),
	provider: z.enum(["github", "gitlab", "gitea", "generic"]),
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

const StepperTriggerWrapper = ({ displayNumber }: { displayNumber?: number }) => {
	const item = useStepItemContext();
	const isInactive = item.status === "inactive";
	const number = displayNumber ?? item.index + 1;

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
					<Stepper.Indicator>{number}</Stepper.Indicator>
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
												src={getGitProviderIconPath(p.id)}
												alt={p.label}
												className={`h-10 w-10 ${getGitProviderIconClass(p.id)}`}
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

function RepositoryStepContent({ form, error }: { form: RepoFormApi; error: string | undefined }) {
	return (
		<form.Subscribe selector={(state) => state.values.provider}>
			{(provider) => (
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
											placeholder={
												PROVIDERS.find((p) => p.id === provider)?.placeholderUrl ||
												"https://github.com/org/repo"
											}
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
			)}
		</form.Subscribe>
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

function SyncTypeSummaryContent({
	isEditing,
	webhookUrl,
	webhookSecret,
}: {
	isEditing: boolean;
	webhookUrl: string | undefined;
	webhookSecret: string | undefined;
}) {
	const [visible, setVisible] = useState(false);

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

			{!webhookSecret ? (
				<p className="text-sm text-muted-foreground mt-4">
					No further action is needed. OrcaCD will start syncing with the repository shortly.
				</p>
			) : (
				<div className="space-y-3 mt-4">
					<p className="text-sm text-muted-foreground">
						Set up a webhook in your repository to enable real-time updates:
					</p>

					<div className="space-y-1">
						<p className="text-xs font-medium">Webhook URL</p>
						<div className="flex items-center gap-1 rounded-md border bg-muted/50 px-3 py-1">
							<code className="flex-1 truncate font-mono text-sm">{webhookUrl}</code>
							<CopyButton text={webhookUrl ?? ""} title="Copy webhook URL" />
						</div>
					</div>

					<div className="space-y-1">
						<p className="text-xs font-medium">Webhook Secret</p>
						<div className="flex items-center gap-1 rounded-md border bg-muted/50 px-3 py-1">
							<code className="flex-1 truncate font-mono text-sm">
								{visible ? webhookSecret : "•".repeat(32)}
							</code>
							<Button
								type="button"
								variant="ghost"
								size="icon"
								className="h-7 w-7 shrink-0 text-muted-foreground hover:text-foreground"
								onClick={() => setVisible((v) => !v)}
								title={visible ? "Hide secret" : "Reveal secret"}
							>
								{visible ? <EyeOffIcon className="h-4 w-4" /> : <EyeIcon className="h-4 w-4" />}
							</Button>
							<CopyButton text={webhookSecret} title="Copy webhook secret" />
						</div>
						<p className="text-xs text-muted-foreground">
							Save this secret now — it won't be shown again.
						</p>
					</div>
					<div className="text-destructive">TODO: Link to docs</div>
				</div>
			)}
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
	const isAtFirstVisibleStep = isEditing
		? stepper.state.current.data.id === "repository"
		: stepper.state.current.index === 0;

	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" onClick={handleClose}>
				{stepper.state.isLast ? "Close" : "Cancel"}
			</Button>
			<div className="flex gap-2">
				{!isAtFirstVisibleStep && !stepper.state.isLast && (
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
	existingRepository,
	asDropdownItem = false,
}: {
	existingRepository?: Repository | undefined;
	asDropdownItem?: boolean;
}) {
	const isEditing = !!existingRepository;
	const [isLoading, setIsLoading] = useState(false);
	const [open, setOpen] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const stepperRef = React.useRef<{ navigation: { next: () => void } } | null>(null);
	const [webhookSecret, setWebhookSecret] = useState<string | undefined>();
	const [webhookUrl, setWebhookUrl] = useState<string | undefined>();

	const form = useForm({
		defaultValues: {
			url: existingRepository?.url ?? "",
			provider: existingRepository?.provider ?? "github",
			authToken: "",
			syncType: existingRepository?.syncType ?? "webhook",
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
					const repo = await updateRepository(existingRepository.id, {
						url: value.url,
						authMethod,
						authToken,
						syncType: value.syncType,
						// To-Do add sync interval setting to edit form
						// I intentionally skipped it in inital setup form now to reduce complexity, but it should be editable when updating
						pollingIntervalSeconds: existingRepository.pollingIntervalSeconds ?? undefined,
					});
					setWebhookSecret(repo.webhookSecret);
					setWebhookUrl(repo.webhookUrl);
				} else {
					const repo = await createRepository({
						url: value.url,
						provider: value.provider,
						authMethod,
						authToken,
						syncType: value.syncType,
					});
					setWebhookSecret(repo.webhookSecret);
					setWebhookUrl(repo.webhookUrl);
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
				aria-describedby={undefined}
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
					className="max-w-[calc(var(--container-md)-2rem)]"
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root
						key={String(open)}
						className="w-full space-y-6"
						orientation="horizontal"
						initialStep={isEditing ? "repository" : undefined}
					>
						{({ stepper }) => {
							stepperRef.current = stepper;
							const allSteps = stepper.state.all;
							const stepsToShow = isEditing
								? allSteps.filter((s) => s.id !== "provider")
								: allSteps;
							const currentRealIndex = stepper.state.current.index;
							return (
								<>
									<Stepper.List className="flex list-none gap-2 flex-row items-center justify-between">
										{stepsToShow.map((stepData, displayIndex) => {
											const realIndex = allSteps.findIndex((s) => s.id === stepData.id);
											const status: StepStatus =
												realIndex < currentRealIndex
													? "success"
													: realIndex === currentRealIndex
														? "active"
														: "inactive";
											const isLast = displayIndex === stepsToShow.length - 1;
											return (
												<React.Fragment key={stepData.id}>
													<Stepper.Item
														step={stepData.id}
														className="group peer relative flex shrink-0 items-center gap-2"
													>
														<StepperTriggerWrapper displayNumber={displayIndex + 1} />
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
														<SyncTypeSummaryContent
															isEditing={isEditing}
															webhookSecret={webhookSecret}
															webhookUrl={webhookUrl}
														/>
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
