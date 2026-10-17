import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
	deleteApplication,
	deployApplication,
	updateApplication,
	type Application,
} from "@/lib/applications";
import { useMemo, useState } from "react";
import { toast } from "@/components/ui/toast";
import { useForm, useStore } from "@tanstack/react-form";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldError,
	FieldGroup,
	FieldLabel,
	FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useFetch } from "@/lib/api";
import type { Agent } from "@/lib/agents";
import { type Repository } from "@/lib/repositories";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { m } from "@/lib/paraglide/messages";
import { Separator } from "@/components/ui/separator";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import LucideIconPicker, { type LucideIconName } from "@/components/lucide-icon-picker";
import {
	applicationSchema,
	buildFileTree,
	TreeNodeList,
	type RepositoryTreeEntry,
} from "@/components/dialogs/upsert-application";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { AppWindow, FileCode, GitBranch, Rocket, Trash2, type LucideIcon } from "lucide-react";

export const Route = createFileRoute("/_authenticated/applications/$id/settings/general")({
	component: GeneralSettingsPage,
	head: () => ({
		meta: [
			{
				title: `${m.navApplications()} - ${m.settings()}`,
			},
		],
	}),
});

// Moving or renaming an application removes it from its agent, so it has to be deployed again
function affectsDeployment(previous: Application, updated: Application): boolean {
	return (
		previous.composeFile !== updated.composeFile ||
		previous.agentId !== updated.agentId ||
		previous.name !== updated.name
	);
}

async function deploy(id: string) {
	try {
		await deployApplication(id);
		toast.add({ type: "success", title: m.deploymentStarted() });
	} catch (err) {
		toast.add({
			type: "error",
			title: err instanceof Error ? err.message : m.failedDeployApplication(),
		});
	}
}

function GeneralSettingsPage() {
	const { id } = Route.useParams();
	const { data: application, mutate: mutateApplication } = useFetch<Application>(
		`/applications/${id}`,
	);

	const navigate = useNavigate();

	const [isSubmitting, setIsSubmitting] = useState(false);

	const { data: agents, isLoading: isAgentsLoading } = useFetch<Agent[]>("/agents");
	const { data: repos, isLoading: isReposLoading } = useFetch<Repository[]>("/repositories");

	const form = useForm({
		defaultValues: {
			name: application?.name ?? "",
			icon: application?.icon ?? "",
			repositoryId: application?.repositoryId ?? "",
			agentId: application?.agentId ?? "",
			branch: application?.branch ?? "",
			path: application?.path ?? "",
			imagePollEnabled: application?.imagePollEnabled ?? false,
			imagePollIntervalSeconds: application?.imagePollIntervalSeconds || 120,
			deleteOldImages: application?.deleteOldImages ?? false,
		},
		validators: {
			onSubmit: applicationSchema,
		},
		onSubmit: async ({ value }) => {
			const previous = application!;
			setIsSubmitting(true);
			try {
				const updated = await updateApplication(previous.id, value);
				// Update the cache first, otherwise the form falls back to the stale defaults after the reset
				await mutateApplication(updated, { revalidate: false });
				form.reset(value);
				if (affectsDeployment(previous, updated)) {
					const toastId = toast.add({
						type: "success",
						title: m.applicationUpdated(),
						description: m.applicationUpdatedDeployPrompt(),
						timeout: 0,
						actionProps: {
							children: m.deploy(),
							onClick: async () => {
								toast.close(toastId);
								await deploy(updated.id);
							},
						},
					});
				} else {
					toast.add({ type: "success", title: m.applicationUpdated() });
				}
			} catch (err) {
				toast.add({
					type: "error",
					title: err instanceof Error ? err.message : m.failedSaveApplication(),
				});
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const repositoryId = useStore(form.store, (state) => state.values.repositoryId);
	const branch = useStore(form.store, (state) => state.values.branch);
	const isPristine = useStore(form.store, (state) => state.isDefaultValue);

	const { data: branches, isLoading: isBranchesLoading } = useFetch<string[]>(
		repositoryId ? `/repositories/${repositoryId}/branches` : null,
	);

	const { data: fileTreeEntries, isLoading: isFileTreeLoading } = useFetch<RepositoryTreeEntry[]>(
		branch ? `/repositories/${repositoryId}/tree?branch=${branch}` : null,
	);

	const fileTree = useMemo(() => {
		return buildFileTree(fileTreeEntries ?? []);
	}, [fileTreeEntries]);

	async function deleteApp() {
		try {
			await deleteApplication(id);
			toast.add({
				type: "success",
				title: m.toastApplicationDeleted({ name: application?.name ?? "" }),
			});
			await navigate({ to: "/applications" });
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.toastDeleteApplicationFailed(),
			});
		}
	}

	return (
		<div className="flex flex-col gap-6">
			<Card>
				<CardHeader>
					<CardTitle>{m.general()}</CardTitle>
					<CardDescription>{m.editApplicationDescription()}</CardDescription>
				</CardHeader>
				<Separator />
				<form
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<CardContent className="divide-y">
						<FormSection
							icon={AppWindow}
							title={m.applicationSettingsApplication()}
							description={m.applicationSettingsApplicationDescription()}
						>
							<div className="grid gap-5 sm:grid-cols-2">
								<form.Field name="name">
									{(field) => {
										const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
										return (
											<Field data-invalid={isInvalid}>
												<FieldLabel htmlFor={field.name}>{m.name()}</FieldLabel>
												<Input
													id={field.name}
													value={field.state.value}
													onBlur={field.handleBlur}
													onChange={(e) => field.handleChange(e.target.value)}
													placeholder={m.applicationNamePlaceholder()}
													aria-invalid={isInvalid}
												/>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</Field>
										);
									}}
								</form.Field>

								<form.Field name="icon" validators={{ onSubmit: applicationSchema.shape.icon }}>
									{(field) => (
										<Field>
											<FieldLabel>{m.icon()}</FieldLabel>
											<LucideIconPicker
												value={field.state.value as LucideIconName}
												onValueChange={field.handleChange}
												placeholder={m.selectIcon()}
												emptyMessage={m.noIconsFound()}
											/>
										</Field>
									)}
								</form.Field>
							</div>

							<form.Field name="agentId">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<FieldLabel htmlFor="agent-select">{m.columnAgent()}</FieldLabel>
											<Select
												name={field.name}
												value={field.state.value}
												onValueChange={(value) => {
													field.handleChange(value ?? "");
												}}
												items={
													agents?.map((agent) => ({
														label: agent.name,
														value: agent.id,
													})) ?? []
												}
											>
												<SelectTrigger
													id="agent-select"
													aria-invalid={isInvalid}
													className="w-full"
												>
													<SelectValue placeholder={m.selectAgent()} />
												</SelectTrigger>
												<SelectContent>
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
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							</form.Field>
						</FormSection>

						<FormSection
							icon={GitBranch}
							title={m.applicationSettingsSource()}
							description={m.applicationSettingsSourceDescription()}
						>
							<div className="grid gap-5 sm:grid-cols-2">
								<form.Field name="repositoryId">
									{(field) => {
										const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
										return (
											<Field data-invalid={isInvalid}>
												<FieldLabel htmlFor="repository-select">{m.repository()}</FieldLabel>
												<Select
													name={field.name}
													value={field.state.value}
													onValueChange={(value) => {
														field.handleChange(value ?? "");
														form.setFieldValue("branch", "");
														form.setFieldValue("path", "");
													}}
													items={
														repos?.map((repo) => ({
															label: repo.name,
															value: repo.id,
														})) ?? []
													}
												>
													<SelectTrigger
														id="repository-select"
														aria-invalid={isInvalid}
														className="w-full"
													>
														<SelectValue placeholder={m.selectRepository()} />
													</SelectTrigger>
													<SelectContent>
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
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</Field>
										);
									}}
								</form.Field>

								<form.Field name="branch">
									{(field) => {
										const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
										return (
											<Field data-invalid={isInvalid}>
												<FieldLabel htmlFor="branch-select">{m.branch()}</FieldLabel>
												<Select
													name={field.name}
													value={field.state.value}
													onValueChange={(value) => {
														field.handleChange(value ?? "");
														form.setFieldValue("path", "");
													}}
													disabled={!repositoryId || isBranchesLoading}
													items={
														branches?.map((branch) => ({
															label: branch,
															value: branch,
														})) ?? []
													}
												>
													<SelectTrigger
														id="branch-select"
														aria-invalid={isInvalid}
														className="w-full"
													>
														<SelectValue
															placeholder={
																repositoryId ? m.selectBranch() : m.selectRepositoryFirst()
															}
														/>
													</SelectTrigger>
													<SelectContent>
														{isBranchesLoading ? (
															<div className="p-2">{m.loadingBranchesDots()}</div>
														) : (
															branches?.map((branch) => (
																<SelectItem key={branch} value={branch}>
																	{branch}
																</SelectItem>
															))
														)}
													</SelectContent>
												</Select>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</Field>
										);
									}}
								</form.Field>
							</div>

							<form.Field name="path">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<FieldLabel id="compose-file-label">{m.composeFile()}</FieldLabel>
											<div
												role="group"
												aria-labelledby="compose-file-label"
												aria-invalid={isInvalid}
												className="overflow-hidden rounded-lg border border-input aria-invalid:border-destructive"
											>
												<div className="flex min-w-0 items-center gap-2 border-b bg-muted/50 px-3 py-2 font-mono text-xs">
													<FileCode className="size-3.5 shrink-0 text-muted-foreground" />
													{field.state.value ? (
														<span className="truncate" title={field.state.value}>
															{field.state.value}
														</span>
													) : (
														<span className="truncate text-muted-foreground">
															{m.noComposeFileSelected()}
														</span>
													)}
												</div>
												<div className="max-h-72 overflow-auto p-1">
													{!repositoryId ? (
														<TreePlaceholder>{m.selectRepositoryFirst()}</TreePlaceholder>
													) : !branch ? (
														<TreePlaceholder>{m.selectBranchFirst()}</TreePlaceholder>
													) : isFileTreeLoading ? (
														<TreePlaceholder>{m.loadingRepositoryTreeDots()}</TreePlaceholder>
													) : fileTree.length === 0 ? (
														<TreePlaceholder>{m.noFilesFoundInBranch()}</TreePlaceholder>
													) : (
														<div className="flex flex-col gap-0.5">
															<TreeNodeList
																nodes={fileTree}
																depth={0}
																selectedPath={field.state.value}
																onSelectPath={(path) => field.handleChange(path)}
															/>
														</div>
													)}
												</div>
											</div>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							</form.Field>
						</FormSection>

						<FormSection
							icon={Rocket}
							title={m.applicationSettingsDeployment()}
							description={m.applicationSettingsDeploymentDescription()}
						>
							<form.Field name="deleteOldImages">
								{(field) => (
									<FieldLabel htmlFor={field.name}>
										<Field orientation="horizontal">
											<FieldContent>
												<FieldTitle>{m.deleteOldImages()}</FieldTitle>
												<FieldDescription>{m.deleteOldImagesDescription()}</FieldDescription>
											</FieldContent>
											<Switch
												id={field.name}
												checked={field.state.value}
												onCheckedChange={(checked) => field.handleChange(checked)}
											/>
										</Field>
									</FieldLabel>
								)}
							</form.Field>
						</FormSection>
					</CardContent>
					<CardFooter className="mt-(--card-spacing) justify-end gap-2">
						<Button
							type="button"
							variant="outline"
							onClick={() => form.reset()}
							disabled={isSubmitting || isPristine}
						>
							{m.cancel()}
						</Button>
						<Button type="submit" disabled={isSubmitting || isPristine}>
							{isSubmitting ? m.savingDots() : m.saveChanges()}
						</Button>
					</CardFooter>
				</form>
			</Card>

			<Card className="ring-destructive/30">
				<CardHeader>
					<CardTitle className="text-destructive">{m.dangerZone()}</CardTitle>
					<CardDescription>{m.deleteApplicationHint()}</CardDescription>
					<CardAction className="self-center">
						<ConfirmationDialog
							onConfirm={async () => await deleteApp()}
							triggerProps={{ variant: "destructive" }}
							triggerText={
								<>
									<Trash2 />
									{m.delete()}
								</>
							}
							description={m.deleteApplicationDescription()}
						/>
					</CardAction>
				</CardHeader>
			</Card>
		</div>
	);
}

function FormSection({
	icon: Icon,
	title,
	description,
	children,
}: {
	icon: LucideIcon;
	title: string;
	description: string;
	children: React.ReactNode;
}) {
	return (
		<section className="grid gap-x-10 gap-y-4 py-6 first:pt-2 last:pb-2 lg:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
			<div className="flex flex-col gap-1">
				<h3 className="flex items-center gap-2 font-medium">
					<Icon className="size-4 text-muted-foreground" />
					{title}
				</h3>
				<p className="text-muted-foreground">{description}</p>
			</div>
			<FieldGroup className="max-w-2xl">{children}</FieldGroup>
		</section>
	);
}

function TreePlaceholder({ children }: { children: React.ReactNode }) {
	return <div className="p-2 text-sm text-muted-foreground">{children}</div>;
}
