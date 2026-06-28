import { createFileRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { updateApplication, type Application } from "@/lib/applications";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useForm, useStore } from "@tanstack/react-form";
import { Field, FieldContent, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import LucideIconPicker, { type LucideIconName } from "@/components/lucide-icon-picker";
import { applicationSchema, buildFileTree, TreeNodeList, type RepositoryTreeEntry } from "@/components/dialogs/upsert-application";

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

function GeneralSettingsPage() {
	const { id } = Route.useParams();
	const { data: application } = useFetch<Application>(`/applications/${id}`);

	const [isSubmitting, setIsSubmitting] = useState(false);
	const [, setOpen] = useState(false);

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
			imagePollDeleteOldImages: application?.imagePollDeleteOldImages ?? false,
		},
		validators: {
			onSubmit: applicationSchema,
		},
		onSubmit: async ({ value }) => {
			setIsSubmitting(true);
			try {
				await updateApplication(application!.id, value);
				toast.success(m.applicationUpdated());
				setOpen(false);
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveApplication());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const repositoryId = useStore(form.store, (state) => state.values.repositoryId);
	const branch = useStore(form.store, (state) => state.values.branch);

	const { data: branches, isLoading: isBranchesLoading } = useFetch<string[]>(
		repositoryId ? `/repositories/${repositoryId}/branches` : null,
	);

	const { data: fileTreeEntries, isLoading: isFileTreeLoading } = useFetch<RepositoryTreeEntry[]>(
		branch ? `/repositories/${repositoryId}/tree?branch=${branch}` : null,
	);

	const fileTree = useMemo(() => {
		return buildFileTree(fileTreeEntries ?? []);
	}, [fileTreeEntries]);

	return (
		<div className="flex flex-col gap-6">
			<div>
				<h1 className="text-2xl font-bold">{m.general()}</h1>
				<p className="text-muted-foreground text-sm">{m.generalDescription()}</p>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>{m.editApplication()}</CardTitle>
					<CardDescription>{m.editApplicationDescription()}</CardDescription>
				</CardHeader>
				<Separator />
				<CardContent>
					<form
						className="overflow-y-auto"
						onSubmit={async (e) => {
							e.preventDefault();
							await form.handleSubmit();
						}}
					>
						<FieldGroup className="max-w-xl">
							<form.Field name="name">
								{(field) => {
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
							</form.Field>

							<form.Field name="icon" validators={{ onSubmit: applicationSchema.shape.icon }}>
								{(field) => (
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
							</form.Field>

							<form.Field name="repositoryId">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field orientation="responsive" className="flex-col!" data-invalid={isInvalid}>
											<FieldContent>
												<FieldLabel htmlFor="repository-select">{m.repository()}</FieldLabel>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</FieldContent>
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
													className="min-w-30"
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
										</Field>
									);
								}}
							</form.Field>

							<form.Field name="agentId">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field orientation="responsive" className="flex-col!" data-invalid={isInvalid}>
											<FieldContent>
												<FieldLabel htmlFor="agent-select">{m.columnAgent()}</FieldLabel>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</FieldContent>
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
													className="min-w-30"
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
										</Field>
									);
								}}
							</form.Field>

							<form.Field name="branch">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field orientation="responsive" className="flex-col!" data-invalid={isInvalid}>
											<FieldContent>
												<FieldLabel htmlFor="branch-select">{m.branch()}</FieldLabel>
												{isInvalid && <FieldError errors={field.state.meta.errors} />}
											</FieldContent>
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
													className="min-w-30"
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
										</Field>
									);
								}}
							</form.Field>

							<form.Field name="path">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.composeFile()}</Label>
											<div className="max-h-64 overflow-auto rounded-md border p-1">
												{!repositoryId ? (
													<div className="p-2 text-sm text-muted-foreground">
														{m.selectRepositoryFirst()}
													</div>
												) : !branch ? (
													<div className="p-2 text-sm text-muted-foreground">
														{m.selectBranchFirst()}
													</div>
												) : isFileTreeLoading ? (
													<div className="p-2 text-sm text-muted-foreground">
														{m.loadingRepositoryTreeDots()}
													</div>
												) : fileTree.length === 0 ? (
													<div className="p-2 text-sm text-muted-foreground">
														{m.noFilesFoundInBranch()}
													</div>
												) : (
													<div className="flex flex-col gap-1">
														<TreeNodeList
															nodes={fileTree}
															depth={0}
															selectedPath={field.state.value}
															onSelectPath={(path) => field.handleChange(path)}
														/>
													</div>
												)}
											</div>
											<Input
												id={field.name}
												value={field.state.value}
												readOnly
												className="mt-2"
												placeholder={m.selectedFilePath()}
											/>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							</form.Field>

							<div className="flex gap-2 pt-2">
								<Button type="submit" disabled={isSubmitting}>
									{isSubmitting ? m.savingDots() : m.updateApplication()}
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
				</CardContent>
			</Card>
		</div>
	);
}
