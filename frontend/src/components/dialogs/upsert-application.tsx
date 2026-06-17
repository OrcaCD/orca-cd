// oxlint-disable react/no-children-prop
import { ChevronRightIcon, FileText, FolderIcon, Pencil, Plus } from "lucide-react";
import { Button } from "../ui/button";
import z from "zod";
import { createApplication, updateApplication, type Application } from "@/lib/applications";
import { Fragment, useMemo, useState } from "react";
import { toast } from "sonner";
import { useForm, useStore } from "@tanstack/react-form";
import { defineStepper } from "@stepperize/react";
import { useStepItemContext, type StepStatus } from "@stepperize/react/primitives";
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
import { type Repository } from "@/lib/repositories";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { m } from "@/lib/paraglide/messages";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";
import { cn } from "@/lib/utils";
import { Switch } from "../ui/switch";
import { Checkbox } from "../ui/checkbox";
import { Separator } from "../ui/separator";
import LucideIconPicker, { type LucideIconName } from "../lucide-icon-picker";

const applicationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationApplicationNameRequired())
		.max(128, m.validationApplicationNameMaxLength()),
	icon: z.string(),
	repositoryId: z.string().min(1, m.validationRepositoryRequired()),
	agentId: z.string().min(1, m.validationAgentRequired()),
	branch: z
		.string()
		.trim()
		.min(1, m.validationBranchRequired())
		.max(256, m.validationBranchMaxLength()),
	path: z
		.string()
		.trim()
		.min(1, m.validationPathRequired())
		.max(512, m.validationPathMaxLength())
		.refine((val) => val.endsWith(".yml") || val.endsWith(".yaml"), {
			message: m.validationPathMustBeYAML(),
		}),
	imagePollEnabled: z.boolean(),
	imagePollIntervalSeconds: z.number().int().min(60, m.validationImagePollIntervalMin()),
	imagePollDeleteOldImages: z.boolean(),
});

const defaultApplicationIcon: LucideIconName = "box";
const { Stepper } = defineStepper({ id: "details" }, { id: "source" }, { id: "imagePolling" });

type ApplicationFormValues = z.infer<typeof applicationSchema>;
type ApplicationStepId = "details" | "source" | "imagePolling";

const applicationStepFields = {
	details: ["name", "icon", "agentId"],
	source: ["repositoryId", "branch", "path"],
	imagePolling: ["imagePollEnabled", "imagePollIntervalSeconds", "imagePollDeleteOldImages"],
} satisfies Record<ApplicationStepId, (keyof ApplicationFormValues)[]>;

type FileTreeNode = {
	name: string;
	path: string;
	type: "file" | "dir";
	children: FileTreeNode[];
};

interface RepositoryTreeEntry {
	path: string;
	type: "file" | "dir";
}

function sortTreeNodes(nodes: FileTreeNode[]): void {
	nodes.sort((a, b) => {
		if (a.type !== b.type) {
			return a.type === "dir" ? -1 : 1;
		}
		return a.name.localeCompare(b.name);
	});

	for (const node of nodes) {
		sortTreeNodes(node.children);
	}
}

function buildFileTree(entries: RepositoryTreeEntry[]): FileTreeNode[] {
	const root: FileTreeNode = {
		name: "",
		path: "",
		type: "dir",
		children: [],
	};

	const nodeMap = new Map<string, FileTreeNode>([["", root]]);

	for (const entry of entries) {
		const cleanPath = entry.path.trim().replace(/^\/+/, "");
		if (!cleanPath) {
			continue;
		}

		const segments = cleanPath.split("/");
		for (let index = 0; index < segments.length; index += 1) {
			const segment = segments[index];
			const currentPath = segments.slice(0, index + 1).join("/");
			const parentPath = segments.slice(0, index).join("/");
			const isLeaf = index === segments.length - 1;

			const parent = nodeMap.get(parentPath);
			if (!parent) {
				continue;
			}

			let node = nodeMap.get(currentPath);
			if (!node) {
				node = {
					name: segment,
					path: currentPath,
					type: isLeaf ? entry.type : "dir",
					children: [],
				};
				nodeMap.set(currentPath, node);
				parent.children.push(node);
			} else if (!isLeaf || entry.type === "dir") {
				node.type = "dir";
			}
		}
	}

	sortTreeNodes(root.children);
	return root.children;
}

function TreeNodeList({
	nodes,
	depth,
	selectedPath,
	onSelectPath,
}: {
	nodes: FileTreeNode[];
	depth: number;
	selectedPath: string;
	onSelectPath: (path: string) => void;
}) {
	return (
		<>
			{nodes.map((node) => {
				if (node.type === "dir") {
					return (
						<Collapsible key={node.path}>
							<CollapsibleTrigger
								render={
									<Button
										type="button"
										variant="ghost"
										size="sm"
										className="group w-full justify-start transition-none hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50"
									>
										<ChevronRightIcon className="transition-transform group-data-[state=open]:rotate-90" />
										<FolderIcon />
										{node.name}
									</Button>
								}
							></CollapsibleTrigger>
							<CollapsibleContent className="mt-1 ml-5 style-lyra:ml-4">
								<div className="flex flex-col gap-1">
									<TreeNodeList
										nodes={node.children}
										depth={depth + 1}
										selectedPath={selectedPath}
										onSelectPath={onSelectPath}
									/>
								</div>
							</CollapsibleContent>
						</Collapsible>
					);
				}

				const isSelected = node.path === selectedPath;
				return (
					<Button
						key={node.path}
						type="button"
						variant="link"
						size="sm"
						onClick={() => onSelectPath(node.path)}
						className={cn("text-foreground mr-auto", isSelected ? "font-medium" : "")}
						disabled={!node.name.endsWith(".yml") && !node.name.endsWith(".yaml")}
					>
						<FileText className="h-4 w-4" />
						<span className="truncate">{node.name}</span>
					</Button>
				);
			})}
		</>
	);
}

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

function StepperNavigation({
	stepper,
	onNext,
	handleClose,
	isSubmitting,
	submitLabel,
}: {
	stepper: { state: { current: { index: number; data: { id: string } }; isLast: boolean } };
	onNext: (stepId: string, advance: () => void) => void;
	handleClose: () => void;
	isSubmitting: boolean;
	submitLabel: string;
}) {
	const isAtFirstVisibleStep = stepper.state.current.index === 0;

	return (
		<div className="flex items-center justify-between gap-4 pt-2">
			<Button type="button" variant="outline" onClick={handleClose} disabled={isSubmitting}>
				{m.cancel()}
			</Button>
			<div className="flex gap-2">
				{!isAtFirstVisibleStep && (
					<Stepper.Prev
						render={(domProps) => (
							<Button type="button" variant="outline" disabled={isSubmitting} {...domProps}>
								{m.previous()}
							</Button>
						)}
					/>
				)}
				{stepper.state.isLast ? (
					<Button type="submit" disabled={isSubmitting}>
						{submitLabel}
					</Button>
				) : (
					<Stepper.Next
						render={(domProps) => (
							<Button
								type="button"
								disabled={isSubmitting}
								onClick={(e) => onNext(stepper.state.current.data.id, () => domProps.onClick?.(e))}
							>
								{m.next()}
							</Button>
						)}
					/>
				)}
			</div>
		</div>
	);
}

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
			icon: application?.icon ?? defaultApplicationIcon,
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
				if (isEditing && application) {
					await updateApplication(application.id, value);
					toast.success(m.applicationUpdated());
				} else {
					await createApplication(value);
					toast.success(m.applicationCreated());
				}
				setOpen(false);
				form.reset();
			} catch (err) {
				toast.error(err instanceof Error ? err.message : m.failedSaveApplication());
			} finally {
				setIsSubmitting(false);
			}
		},
	});

	const repositoryId = useStore(form.store, (state) => state.values.repositoryId);
	const branch = useStore(form.store, (state) => state.values.branch);
	const imagePollEnabled = useStore(form.store, (state) => state.values.imagePollEnabled);

	const { data: branches, isLoading: isBranchesLoading } = useFetch<string[]>(
		repositoryId ? `/repositories/${repositoryId}/branches` : null,
	);

	const { data: fileTreeEntries, isLoading: isFileTreeLoading } = useFetch<RepositoryTreeEntry[]>(
		branch ? `/repositories/${repositoryId}/tree?branch=${branch}` : null,
	);

	const fileTree = useMemo(() => {
		return buildFileTree(fileTreeEntries ?? []);
	}, [fileTreeEntries]);

	const handleClose = () => {
		setOpen(false);
		form.reset();
	};

	async function handleNext(stepId: string, advance: () => void) {
		const fields = applicationStepFields[stepId as ApplicationStepId];
		if (!fields) {
			advance();
			return;
		}

		for (const fieldName of fields) {
			const errors = await Promise.resolve(form.validateField(fieldName, "submit"));
			if (errors?.length) {
				return;
			}
		}

		advance();
	}

	return (
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				if (nextOpen) {
					setOpen(true);
					return;
				}
				handleClose();
			}}
		>
			<DialogTrigger
				render={
					asDropdownItem ? (
						<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
							<Pencil />
							{m.edit()}
						</DropdownMenuItem>
					) : isEditing ? (
						<Button variant="outline">
							<Pencil />
							{m.editApplication()}
						</Button>
					) : (
						<Button>
							<Plus />
							{m.newApplication()}
						</Button>
					)
				}
			></DialogTrigger>
			<DialogContent className="flex max-h-[90vh] flex-col sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? m.editApplication() : m.addApplication()}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing ? m.editApplicationDescription() : m.addApplicationDescription()}
					</DialogDescription>
				</DialogHeader>

				<form
					className="overflow-y-auto"
					onSubmit={async (e) => {
						e.preventDefault();
						await form.handleSubmit();
					}}
				>
					<Stepper.Root key={String(open)} className="w-full space-y-6" orientation="horizontal">
						{({ stepper }) => {
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
												<Fragment key={stepData.id}>
													<Stepper.Item
														step={stepData.id}
														className="group peer relative flex shrink-0 items-center gap-2"
													>
														<StepperTriggerWrapper displayNumber={displayIndex + 1} />
													</Stepper.Item>
													<StepperSeparatorWithStatus status={status} isLast={isLast} />
												</Fragment>
											);
										})}
									</Stepper.List>

									{stepper.flow.switch({
										details: () => (
											<Stepper.Content
												step="details"
												render={(props) => (
													<FieldGroup {...props}>
														<form.Field
															name="name"
															validators={{ onSubmit: applicationSchema.shape.name }}
															children={(field) => {
																const isInvalid =
																	field.state.meta.isTouched && !field.state.meta.isValid;
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
															name="icon"
															validators={{ onSubmit: applicationSchema.shape.icon }}
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

														<form.Field
															name="agentId"
															validators={{ onSubmit: applicationSchema.shape.agentId }}
															children={(field) => {
																const isInvalid =
																	field.state.meta.isTouched && !field.state.meta.isValid;
																return (
																	<Field orientation="responsive" data-invalid={isInvalid}>
																		<FieldContent>
																			<FieldLabel htmlFor="agent-select">
																				{m.columnAgent()}
																			</FieldLabel>
																			{isInvalid && <FieldError errors={field.state.meta.errors} />}
																		</FieldContent>
																		<Select
																			name={field.name}
																			value={field.state.value}
																			onValueChange={(value) => field.handleChange(value ?? "")}
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
																			<SelectContent alignItemWithTrigger={false}>
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
													</FieldGroup>
												)}
											/>
										),
										source: () => (
											<Stepper.Content
												step="source"
												render={(props) => (
													<FieldGroup {...props}>
														<form.Field
															name="repositoryId"
															validators={{ onSubmit: applicationSchema.shape.repositoryId }}
															children={(field) => {
																const isInvalid =
																	field.state.meta.isTouched && !field.state.meta.isValid;
																return (
																	<Field orientation="responsive" data-invalid={isInvalid}>
																		<FieldContent>
																			<FieldLabel htmlFor="repository-select">
																				{m.repository()}
																			</FieldLabel>
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
																			<SelectContent alignItemWithTrigger={false}>
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
															name="branch"
															validators={{ onSubmit: applicationSchema.shape.branch }}
															children={(field) => {
																const isInvalid =
																	field.state.meta.isTouched && !field.state.meta.isValid;
																return (
																	<Field orientation="responsive" data-invalid={isInvalid}>
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
																						repositoryId
																							? m.selectBranch()
																							: m.selectRepositoryFirst()
																					}
																				/>
																			</SelectTrigger>
																			<SelectContent alignItemWithTrigger={false}>
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
														/>

														<form.Field
															name="path"
															validators={{ onSubmit: applicationSchema.shape.path }}
															children={(field) => {
																const isInvalid =
																	field.state.meta.isTouched && !field.state.meta.isValid;
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
														/>
													</FieldGroup>
												)}
											/>
										),
										imagePolling: () => (
											<Stepper.Content
												step="imagePolling"
												render={(props) => (
													<FieldGroup {...props}>
														<p className="text-sm font-medium">{m.imagePollSectionTitle()}</p>

														<form.Field
															name="imagePollEnabled"
															validators={{ onSubmit: applicationSchema.shape.imagePollEnabled }}
															children={(field) => (
																<Field>
																	<div className="flex items-start gap-3">
																		<Switch
																			id={field.name}
																			checked={field.state.value}
																			onCheckedChange={(checked) => field.handleChange(checked)}
																		/>
																		<div className="space-y-1">
																			<Label htmlFor={field.name}>{m.imagePollEnabled()}</Label>
																			<p className="text-muted-foreground text-xs">
																				{m.imagePollEnabledDescription()}
																			</p>
																		</div>
																	</div>
																</Field>
															)}
														/>

														{imagePollEnabled && (
															<>
																<form.Field
																	name="imagePollIntervalSeconds"
																	validators={{
																		onSubmit: applicationSchema.shape.imagePollIntervalSeconds,
																	}}
																	children={(field) => {
																		const isInvalid =
																			field.state.meta.isTouched && !field.state.meta.isValid;
																		return (
																			<Field data-invalid={isInvalid}>
																				<Label htmlFor={field.name}>
																					{m.imagePollIntervalSeconds()}
																				</Label>
																				<Input
																					id={field.name}
																					type="number"
																					min={60}
																					value={field.state.value}
																					onBlur={field.handleBlur}
																					onChange={(e) =>
																						field.handleChange(Number(e.target.value))
																					}
																				/>
																				<p className="text-muted-foreground text-xs">
																					{m.imagePollIntervalHint()}
																				</p>
																				{isInvalid && (
																					<FieldError errors={field.state.meta.errors} />
																				)}
																			</Field>
																		);
																	}}
																/>

																<form.Field
																	name="imagePollDeleteOldImages"
																	validators={{
																		onSubmit: applicationSchema.shape.imagePollDeleteOldImages,
																	}}
																	children={(field) => (
																		<Field>
																			<div className="flex items-start gap-2">
																				<Checkbox
																					id={field.name}
																					checked={field.state.value}
																					onCheckedChange={(checked) =>
																						field.handleChange(checked === true)
																					}
																				/>
																				<div className="space-y-1">
																					<Label htmlFor={field.name}>
																						{m.imagePollDeleteOldImages()}
																					</Label>
																					<p className="text-muted-foreground text-xs">
																						{m.imagePollDeleteOldImagesDescription()}
																					</p>
																				</div>
																			</div>
																		</Field>
																	)}
																/>
															</>
														)}
													</FieldGroup>
												)}
											/>
										),
									})}

									<Separator />

									<StepperNavigation
										stepper={stepper}
										onNext={handleNext}
										handleClose={handleClose}
										isSubmitting={isSubmitting}
										submitLabel={
											isSubmitting
												? m.savingDots()
												: isEditing
													? m.updateApplication()
													: m.addApplication()
										}
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
