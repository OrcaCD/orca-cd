// oxlint-disable react/no-children-prop
import { PencilIcon, PlusIcon } from "lucide-react";
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
	type RepositoryProvider,
	testRepositoryConnection,
	type RepositorySyncType,
	getGitProviderIconPath,
	getGitProviderIconClass,
} from "@/lib/repositories";
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
import ErrorAlert from "../alerts/error-alert";
import SuccessAlert from "../alerts/success-alert";
import { m } from "@/lib/paraglide/messages";
import {
	RepositoryDialogLoadingOverlay,
	SyncTypeRadioGroup,
	WebhookSetupDetails,
} from "./repository-shared";

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

const { Stepper } = defineStepper(
	{ id: "provider" },
	{ id: "repository" },
	{ id: "syncType" },
	{ id: "summary" },
);

const repositorySchema = z.object({
	url: z.url({ error: m.validationRepositoryUrlInvalid(), protocol: /^https?$/ }).trim(),
	provider: z.enum(["github", "gitlab", "gitea", "generic"]),
	authToken: z.string().trim().max(1024, m.validationAuthTokenMaxLength()),
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
					<p className="text-muted-foreground text-sm mb-4">{m.selectProviderDescription()}</p>
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
						{m.repositoryConnectionDescription()}
					</p>
					<FieldGroup>
						<form.Field name="url" validators={{ onSubmit: repositorySchema.shape.url }}>
							{(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field data-invalid={isInvalid}>
										<Label htmlFor={field.name}>{m.repository()}</Label>
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
										<Label htmlFor={field.name}>{m.authToken()}</Label>
										<Input
											id={field.name}
											type="password"
											value={field.state.value}
											onBlur={field.handleBlur}
											onChange={(e) => field.handleChange(e.target.value)}
											placeholder={m.authTokenPlaceholder()}
										/>
										<p className="text-muted-foreground text-xs">{m.authTokenRecommendedHint()}</p>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						</form.Field>
						{error && <ErrorAlert title={m.cantConnectToRepository()} description={error} />}
					</FieldGroup>
				</>
			)}
		</form.Subscribe>
	);
}

function SyncTypeStepContent({ form }: { form: RepoFormApi }) {
	return (
		<>
			<p className="text-muted-foreground text-sm mb-4">{m.syncTypeDescription()}</p>
			<FieldGroup>
				<form.Field name="syncType" validators={{ onSubmit: repositorySchema.shape.syncType }}>
					{(field) => (
						<SyncTypeRadioGroup
							value={field.state.value}
							onChange={field.handleChange}
							onBlur={field.handleBlur}
						/>
					)}
				</form.Field>
			</FieldGroup>
		</>
	);
}

function SyncTypeSummaryContent({
	webhookUrl,
	webhookSecret,
}: {
	webhookUrl: string | undefined;
	webhookSecret: string | undefined;
}) {
	return (
		<>
			<SuccessAlert
				title={m.repositoryConnectedTitle()}
				description={m.repositoryConnectedDescription()}
			/>

			{!webhookSecret ? (
				<p className="text-sm text-muted-foreground mt-4">{m.repositoryNoFurtherAction()}</p>
			) : (
				<WebhookSetupDetails webhookUrl={webhookUrl} webhookSecret={webhookSecret} />
			)}
		</>
	);
}

function StepperNavigation({
	stepper,
	onNext,
	handleClose,
}: {
	stepper: { state: { current: { index: number; data: { id: string } }; isLast: boolean } };
	onNext: (stepId: string, advance: () => void) => void;
	handleClose: () => void;
}) {
	const isAtFirstVisibleStep = stepper.state.current.index === 0;

	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" onClick={handleClose}>
				{stepper.state.isLast ? m.close() : m.cancel()}
			</Button>
			<div className="flex gap-2">
				{!isAtFirstVisibleStep && !stepper.state.isLast && (
					<Stepper.Prev
						render={(domProps) => (
							<Button type="button" variant="outline" {...domProps}>
								{m.previous()}
							</Button>
						)}
					/>
				)}
				{stepper.state.current.data.id === "syncType" ? (
					<Button type="submit">{m.connectRepository()}</Button>
				) : !stepper.state.isLast ? (
					<Stepper.Next
						render={(domProps) => (
							<Button
								type="button"
								onClick={(e) => onNext(stepper.state.current.data.id, () => domProps.onClick?.(e))}
							>
								{m.next()}
							</Button>
						)}
					/>
				) : null}
			</div>
		</div>
	);
}

export default function CreateRepositoryDialog({
	asDropdownItem = false,
}: {
	asDropdownItem?: boolean;
}) {
	const [isLoading, setIsLoading] = useState(false);
	const [open, setOpen] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const stepperRef = React.useRef<{ navigation: { next: () => void } } | null>(null);
	const [webhookSecret, setWebhookSecret] = useState<string | undefined>();
	const [webhookUrl, setWebhookUrl] = useState<string | undefined>();

	const form = useForm({
		defaultValues: {
			url: "",
			provider: "github" as RepositoryProvider,
			authToken: "",
			syncType: "webhook" as RepositorySyncType,
		},
		validators: {
			onSubmit: repositorySchema,
		},
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const authToken = value.authToken?.trim() ? value.authToken.trim() : undefined;
				const authMethod = authToken ? "token" : "none";

				const repo = await createRepository({
					url: value.url,
					provider: value.provider,
					authMethod,
					authToken,
					syncType: value.syncType,
				});
				setWebhookSecret(repo.webhookSecret);
				setWebhookUrl(repo.webhookUrl);

				stepperRef.current?.navigation.next();
			} catch (err: any) {
				toast.error(err?.message || m.unexpectedError());
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

			// oxlint-disable-next-line no-warning-comments
			// TODO support other auth methods
			const authMethod = authToken?.length ? "token" : "none";

			setIsLoading(true);

			try {
				await testRepositoryConnection({ provider, url, authToken: authToken, authMethod });
			} catch (err: any) {
				setError(err?.message || m.failedConnectRepository());
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
						{m.edit()}
					</DropdownMenuItem>
				) : (
					<Button>
						<PlusIcon className="h-4 w-4" />
						{m.addRepository()}
					</Button>
				)}
			</DialogTrigger>
			<DialogContent
				onPointerDownOutside={(e) => e.preventDefault()}
				className="sm:max-w-md overflow-hidden"
				aria-describedby={undefined}
			>
				<RepositoryDialogLoadingOverlay isLoading={isLoading} />
				<DialogHeader>
					<DialogTitle>{m.addRepository()}</DialogTitle>
				</DialogHeader>
				<form
					className="max-w-[calc(var(--container-md)-2rem)]"
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root key={String(open)} className="w-full space-y-6" orientation="horizontal">
						{({ stepper }) => {
							stepperRef.current = stepper;
							const allSteps = stepper.state.all;
							const currentIndex = stepper.state.current.index;

							return (
								<>
									<Stepper.List className="flex list-none gap-2 flex-row items-center justify-between">
										{allSteps.map((stepData, displayIndex) => {
											const status: StepStatus =
												displayIndex < currentIndex
													? "success"
													: displayIndex === currentIndex
														? "active"
														: "inactive";
											const isLast = displayIndex === allSteps.length - 1;
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
