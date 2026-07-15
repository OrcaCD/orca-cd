// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { InfoIcon } from "lucide-react";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { updateRepository, type Repository, type RepositorySyncType } from "@/lib/repositories";
import { SyncTypeRadioGroup, WebhookSetupDetails } from "@/components/dialogs/repository-shared";
import SuccessAlert from "@/components/alerts/success-alert";
import ErrorAlert from "@/components/alerts/error-alert";
import { m } from "@/lib/paraglide/messages";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useFetch } from "@/lib/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	Combobox,
	ComboboxChips,
	ComboboxChip,
	ComboboxChipsInput,
	ComboboxValue,
	ComboboxContent,
	ComboboxList,
	ComboboxItem,
	ComboboxEmpty,
	useComboboxAnchor,
} from "@/components/ui/combobox";

function arraysEqual(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((value, index) => value === b[index]);
}

type OIDCAllowedActions = "both" | "repoSyncOnly" | "imageSyncOnly";

function toOIDCAllowedActions(allowRepoSync: boolean, allowImageSync: boolean): OIDCAllowedActions {
	if (allowRepoSync && allowImageSync) {
		return "both";
	}
	return allowRepoSync ? "repoSyncOnly" : "imageSyncOnly";
}

function fromOIDCAllowedActions(value: OIDCAllowedActions): {
	allowRepoSync: boolean;
	allowImageSync: boolean;
} {
	return {
		allowRepoSync: value !== "imageSyncOnly",
		allowImageSync: value !== "repoSyncOnly",
	};
}

// Free-text multi-value combobox: since workflow file names can't be enumerated from
// an API, the dropdown offers a single "Add "<query>"" item for whatever is typed.
function WorkflowsComboboxField({
	id,
	value,
	onChange,
	onBlur,
}: {
	id: string;
	value: string[];
	onChange: (value: string[]) => void;
	onBlur: () => void;
}) {
	const anchor = useComboboxAnchor();
	const [query, setQuery] = useState("");
	const trimmed = query.trim();
	const items = trimmed && !value.includes(trimmed) ? [trimmed] : [];

	return (
		<Combobox
			items={items}
			multiple
			autoHighlight
			filter={null}
			inputValue={query}
			onInputValueChange={setQuery}
			value={value}
			onValueChange={(next) => {
				onChange(next);
				setQuery("");
			}}
		>
			<ComboboxChips ref={anchor}>
				<ComboboxValue>
					{(values) => (
						<>
							{values.map((v: string) => (
								<ComboboxChip key={v}>{v}</ComboboxChip>
							))}
							<ComboboxChipsInput
								id={id}
								onBlur={onBlur}
								placeholder={
									value.length === 0 ? m.githubActionsOIDCAllowedWorkflowsPlaceholder() : ""
								}
							/>
						</>
					)}
				</ComboboxValue>
			</ComboboxChips>
			<ComboboxContent anchor={anchor}>
				<ComboboxEmpty>{m.githubActionsOIDCAllowedWorkflowsEmpty()}</ComboboxEmpty>
				<ComboboxList>
					{(item: string) => (
						<ComboboxItem key={item} value={item}>
							{m.githubActionsOIDCAddWorkflow({ value: item })}
						</ComboboxItem>
					)}
				</ComboboxList>
			</ComboboxContent>
		</Combobox>
	);
}

export const Route = createFileRoute("/_authenticated/repositories/$id/settings/sync")({
	component: RepositorySyncPage,
	head: () => ({
		meta: [{ title: `${m.pageRepositories()} - ${m.editRepositorySyncShort()}` }],
	}),
});

const syncSchema = z.object({
	syncType: z.enum(["webhook", "polling", "manual"]),
	pollingIntervalSeconds: z.number().int(),
	githubActionsOIDCEnabled: z.boolean(),
	githubActionsOIDCAllowRepoSync: z.boolean(),
	githubActionsOIDCAllowImageSync: z.boolean(),
	githubActionsOIDCAllowedBranches: z.array(z.string()),
	githubActionsOIDCAllowedWorkflows: z.array(z.string()),
});

function hasSyncFormChanged(values: z.infer<typeof syncSchema>, repository: Repository): boolean {
	if (values.syncType !== repository.syncType) {
		return true;
	}
	if (
		values.syncType === "polling" &&
		values.pollingIntervalSeconds !== (repository.pollingIntervalSeconds ?? 300)
	) {
		return true;
	}
	if (values.githubActionsOIDCEnabled !== repository.githubActionsOIDCEnabled) {
		return true;
	}
	if (values.githubActionsOIDCAllowRepoSync !== repository.githubActionsOIDCAllowRepoSync) {
		return true;
	}
	if (values.githubActionsOIDCAllowImageSync !== repository.githubActionsOIDCAllowImageSync) {
		return true;
	}
	if (
		!arraysEqual(
			values.githubActionsOIDCAllowedBranches,
			repository.githubActionsOIDCAllowedBranches,
		)
	) {
		return true;
	}
	if (
		!arraysEqual(
			values.githubActionsOIDCAllowedWorkflows,
			repository.githubActionsOIDCAllowedWorkflows,
		)
	) {
		return true;
	}
	return false;
}

function RepositorySyncPage() {
	const { id } = Route.useParams();
	const { data: repository } = useFetch<Repository>(`/repositories/${id}`);
	const { data: branches } = useFetch<string[]>(
		repository?.provider === "github" ? `/repositories/${id}/branches` : null,
	);
	const branchesAnchor = useComboboxAnchor();

	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const [successData, setSuccessData] = useState<{
		webhookUrl?: string;
		webhookSecret?: string;
	} | null>(null);

	const form = useForm({
		defaultValues: {
			syncType: (repository?.syncType ?? "webhook") as RepositorySyncType,
			pollingIntervalSeconds: repository?.pollingIntervalSeconds ?? 300,
			githubActionsOIDCEnabled: repository?.githubActionsOIDCEnabled ?? false,
			githubActionsOIDCAllowRepoSync: repository?.githubActionsOIDCAllowRepoSync ?? true,
			githubActionsOIDCAllowImageSync: repository?.githubActionsOIDCAllowImageSync ?? true,
			githubActionsOIDCAllowedBranches: repository?.githubActionsOIDCAllowedBranches ?? [],
			githubActionsOIDCAllowedWorkflows: repository?.githubActionsOIDCAllowedWorkflows ?? [],
		},
		validators: {
			onSubmit: syncSchema.superRefine((val, ctx) => {
				if (val.syncType === "polling") {
					if (val.pollingIntervalSeconds < 60) {
						ctx.addIssue({
							code: "custom",
							message: m.validationPollingIntervalMin(),
							path: ["pollingIntervalSeconds"],
						});
					} else if (val.pollingIntervalSeconds > 86400) {
						ctx.addIssue({
							code: "custom",
							message: m.validationPollingIntervalMax(),
							path: ["pollingIntervalSeconds"],
						});
					}
				}
				if (
					val.githubActionsOIDCEnabled &&
					!val.githubActionsOIDCAllowRepoSync &&
					!val.githubActionsOIDCAllowImageSync
				) {
					ctx.addIssue({
						code: "custom",
						message: m.githubActionsOIDCAtLeastOneActionRequired(),
						path: ["githubActionsOIDCAllowRepoSync"],
					});
				}
			}),
		},
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setError(undefined);
			try {
				const pollingIntervalSeconds =
					value.syncType === "polling" ? value.pollingIntervalSeconds : undefined;
				const repo = await updateRepository(id, {
					syncType: value.syncType,
					pollingIntervalSeconds,
					githubActionsOIDCEnabled: value.githubActionsOIDCEnabled,
					githubActionsOIDCAllowRepoSync: value.githubActionsOIDCAllowRepoSync,
					githubActionsOIDCAllowImageSync: value.githubActionsOIDCAllowImageSync,
					githubActionsOIDCAllowedBranches: value.githubActionsOIDCAllowedBranches,
					githubActionsOIDCAllowedWorkflows: value.githubActionsOIDCAllowedWorkflows,
				});
				setSuccessData({ webhookUrl: repo.webhookUrl, webhookSecret: repo.webhookSecret });
			} catch (err) {
				setError(err instanceof Error ? err.message : m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<div className="space-y-6">
			<Card>
				<CardHeader>
					<CardTitle>{m.editRepositorySync()}</CardTitle>
					<CardDescription>{m.editRepositorySyncDescription()}</CardDescription>
				</CardHeader>
				<Separator />
				<CardContent>
					{successData ? (
						<div className="space-y-2 max-w-xl">
							<SuccessAlert
								title={m.repositorySyncUpdatedTitle()}
								description={m.repositorySyncUpdatedDescription()}
							/>
							{successData.webhookSecret ? (
								<WebhookSetupDetails
									webhookUrl={successData.webhookUrl}
									webhookSecret={successData.webhookSecret}
								/>
							) : (
								<p className="text-sm text-muted-foreground mt-2">
									{m.repositoryNoFurtherAction()}
								</p>
							)}
							<div className="pt-2">
								<Button type="button" variant="outline" onClick={() => setSuccessData(null)}>
									{m.close()}
								</Button>
							</div>
						</div>
					) : (
						<form
							onSubmit={async (e) => {
								e.preventDefault();
								await form.handleSubmit();
							}}
						>
							<FieldGroup className="max-w-xl">
								<form.Field name="syncType">
									{(field) => (
										<SyncTypeRadioGroup
											value={field.state.value as RepositorySyncType}
											onChange={field.handleChange}
											onBlur={field.handleBlur}
											className="w-full"
										/>
									)}
								</form.Field>

								<form.Subscribe selector={(state) => state.values.syncType}>
									{(syncType) =>
										syncType === "polling" ? (
											<form.Field name="pollingIntervalSeconds">
												{(field) => {
													const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
													return (
														<Field data-invalid={isInvalid}>
															<Label htmlFor={field.name}>{m.pollingIntervalSeconds()}</Label>
															<Input
																id={field.name}
																type="number"
																min={60}
																max={86400}
																value={field.state.value ?? ""}
																onBlur={field.handleBlur}
																onChange={(e) => field.handleChange(Number(e.target.value))}
															/>
															<p className="text-muted-foreground text-xs">
																{m.pollingIntervalHint()}
															</p>
															{isInvalid && <FieldError errors={field.state.meta.errors} />}
														</Field>
													);
												}}
											</form.Field>
										) : null
									}
								</form.Subscribe>

								{repository?.provider === "github" && (
									<>
										<Separator />
										<form.Field name="githubActionsOIDCEnabled">
											{(field) => (
												<div className="flex items-center justify-between gap-4">
													<div className="flex items-center gap-1.5">
														<Label htmlFor={field.name} className="cursor-pointer">
															{m.githubActionsOIDCEnabled()}
														</Label>
														<Tooltip>
															<TooltipTrigger
																render={
																	<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
																}
															/>
															<TooltipContent>{m.githubActionsOIDCEnabledTooltip()}</TooltipContent>
														</Tooltip>
													</div>
													<Switch
														id={field.name}
														checked={field.state.value}
														onCheckedChange={field.handleChange}
													/>
												</div>
											)}
										</form.Field>

										<form.Subscribe selector={(state) => state.values.githubActionsOIDCEnabled}>
											{(oidcEnabled) =>
												oidcEnabled ? (
													<>
														<form.Subscribe
															selector={(state) => [
																state.values.githubActionsOIDCAllowRepoSync,
																state.values.githubActionsOIDCAllowImageSync,
															]}
														>
															{([allowRepoSync, allowImageSync]) => (
																<Field>
																	<div className="flex items-center gap-1.5">
																		<Label htmlFor="githubActionsOIDCAllowedActions">
																			{m.githubActionsOIDCAllowedActions()}
																		</Label>
																		<Tooltip>
																			<TooltipTrigger
																				render={
																					<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
																				}
																			/>
																			<TooltipContent>
																				{m.githubActionsOIDCAllowedActionsTooltip()}
																			</TooltipContent>
																		</Tooltip>
																	</div>
																	<Select
																		items={[
																			{ value: "both", label: m.githubActionsOIDCActionsBoth() },
																			{
																				value: "repoSyncOnly",
																				label: m.githubActionsOIDCActionsRepoSyncOnly(),
																			},
																			{
																				value: "imageSyncOnly",
																				label: m.githubActionsOIDCActionsImageSyncOnly(),
																			},
																		]}
																		value={toOIDCAllowedActions(allowRepoSync, allowImageSync)}
																		onValueChange={(value: OIDCAllowedActions | null) => {
																			if (!value) {
																				return;
																			}
																			const next = fromOIDCAllowedActions(value);
																			form.setFieldValue(
																				"githubActionsOIDCAllowRepoSync",
																				next.allowRepoSync,
																			);
																			form.setFieldValue(
																				"githubActionsOIDCAllowImageSync",
																				next.allowImageSync,
																			);
																		}}
																	>
																		<SelectTrigger
																			id="githubActionsOIDCAllowedActions"
																			className="w-full"
																		>
																			<SelectValue />
																		</SelectTrigger>
																		<SelectContent>
																			<SelectItem value="both">
																				{m.githubActionsOIDCActionsBoth()}
																			</SelectItem>
																			<SelectItem value="repoSyncOnly">
																				{m.githubActionsOIDCActionsRepoSyncOnly()}
																			</SelectItem>
																			<SelectItem value="imageSyncOnly">
																				{m.githubActionsOIDCActionsImageSyncOnly()}
																			</SelectItem>
																		</SelectContent>
																	</Select>
																</Field>
															)}
														</form.Subscribe>

														<form.Field name="githubActionsOIDCAllowedBranches">
															{(field) => (
																<Field>
																	<div className="flex items-center gap-1.5">
																		<Label htmlFor={field.name}>
																			{m.githubActionsOIDCAllowedBranches()}
																		</Label>
																		<Tooltip>
																			<TooltipTrigger
																				render={
																					<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
																				}
																			/>
																			<TooltipContent>
																				{m.githubActionsOIDCAllowedBranchesTooltip()}
																			</TooltipContent>
																		</Tooltip>
																	</div>
																	<Combobox
																		items={branches ?? []}
																		multiple
																		autoHighlight
																		value={field.state.value}
																		onValueChange={field.handleChange}
																	>
																		<ComboboxChips ref={branchesAnchor}>
																			<ComboboxValue>
																				{(values) => (
																					<>
																						{values.map((value: string) => (
																							<ComboboxChip key={value}>{value}</ComboboxChip>
																						))}
																						<ComboboxChipsInput
																							placeholder={
																								field.state.value.length === 0
																									? m.githubActionsOIDCAllowedBranchesPlaceholder()
																									: ""
																							}
																						/>
																					</>
																				)}
																			</ComboboxValue>
																		</ComboboxChips>
																		<ComboboxContent anchor={branchesAnchor}>
																			<ComboboxEmpty>{m.noBranchesAvailable()}</ComboboxEmpty>
																			<ComboboxList>
																				{(branch) => (
																					<ComboboxItem key={branch} value={branch}>
																						{branch}
																					</ComboboxItem>
																				)}
																			</ComboboxList>
																		</ComboboxContent>
																	</Combobox>
																</Field>
															)}
														</form.Field>

														<form.Field name="githubActionsOIDCAllowedWorkflows">
															{(field) => (
																<Field>
																	<div className="flex items-center gap-1.5">
																		<Label htmlFor={field.name}>
																			{m.githubActionsOIDCAllowedWorkflows()}
																		</Label>
																		<Tooltip>
																			<TooltipTrigger
																				render={
																					<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
																				}
																			/>
																			<TooltipContent>
																				{m.githubActionsOIDCAllowedWorkflowsTooltip()}
																			</TooltipContent>
																		</Tooltip>
																	</div>
																	<WorkflowsComboboxField
																		id={field.name}
																		value={field.state.value}
																		onChange={field.handleChange}
																		onBlur={field.handleBlur}
																	/>
																</Field>
															)}
														</form.Field>
													</>
												) : null
											}
										</form.Subscribe>
									</>
								)}

								<form.Subscribe selector={(state) => state.values}>
									{(values) => {
										const unchanged = !!repository && !hasSyncFormChanged(values, repository);
										return (
											<>
												{error && <ErrorAlert description={error} />}
												<div className="flex gap-2 pt-2">
													<Button type="submit" disabled={isLoading || unchanged || !repository}>
														{m.updateRepository()}
													</Button>
													<Button
														type="button"
														variant="outline"
														onClick={() => {
															setError(undefined);
															form.reset();
														}}
														disabled={isLoading}
													>
														{m.cancel()}
													</Button>
												</div>
											</>
										);
									}}
								</form.Subscribe>
							</FieldGroup>
						</form>
					)}
				</CardContent>
			</Card>
		</div>
	);
}
