// oxlint-disable react/no-children-prop
import { ChevronRightIcon, FileText, FolderIcon, Pencil, Plus } from "lucide-react";
import { Button } from "../ui/button";
import z from "zod";
import { createApplication, updateApplication, type Application } from "@/lib/applications";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useForm, useStore } from "@tanstack/react-form";
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
import { type Repository } from "@/lib/repsitories";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { m } from "@/lib/paraglide/messages";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";
import { cn } from "@/lib/utils";

const applicationSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, m.validationApplicationNameRequired())
		.max(128, m.validationApplicationNameMaxLength()),
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
});

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
							<CollapsibleTrigger asChild>
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
							</CollapsibleTrigger>
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
			repositoryId: application?.repositoryId ?? "",
			agentId: application?.agentId ?? "",
			branch: application?.branch ?? "",
			path: application?.path ?? "",
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
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				setOpen(nextOpen);
				if (!nextOpen) {
					form.reset();
				}
			}}
		>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem onSelect={(e) => e.preventDefault()}>
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
						{m.newApplication()}
					</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						{isEditing ? m.editApplication() : m.addApplication()}
					</DialogTitle>
					<DialogDescription className="py-2">
						{isEditing ? m.editApplicationDescription() : m.addApplicationDescription()}
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
											placeholder={m.applicationNamePlaceholder()}
											autoFocus
										/>
										{isInvalid && <FieldError errors={field.state.meta.errors} />}
									</Field>
								);
							}}
						/>

						<form.Field
							name="repositoryId"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field orientation="responsive" data-invalid={isInvalid}>
										<FieldContent>
											<FieldLabel htmlFor="repository-select">{m.repository()}</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={(value) => {
												field.handleChange(value);
												form.setFieldValue("branch", "");
												form.setFieldValue("path", "");
											}}
										>
											<SelectTrigger
												id="repository-select"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder={m.selectRepository()} />
											</SelectTrigger>
											<SelectContent position="item-aligned">
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
							name="agentId"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
								return (
									<Field orientation="responsive" data-invalid={isInvalid}>
										<FieldContent>
											<FieldLabel htmlFor="agent-select">{m.columnAgent()}</FieldLabel>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</FieldContent>
										<Select
											name={field.name}
											value={field.state.value}
											onValueChange={field.handleChange}
										>
											<SelectTrigger
												id="agent-select"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue placeholder={m.selectAgent()} />
											</SelectTrigger>
											<SelectContent position="item-aligned">
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

						<form.Field
							name="branch"
							children={(field) => {
								const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
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
												field.handleChange(value);
												form.setFieldValue("path", "");
											}}
											disabled={!repositoryId || isBranchesLoading}
										>
											<SelectTrigger
												id="branch-select"
												aria-invalid={isInvalid}
												className="min-w-30"
											>
												<SelectValue
													placeholder={repositoryId ? m.selectBranch() : m.selectRepositoryFirst()}
												/>
											</SelectTrigger>
											<SelectContent position="item-aligned">
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
							children={(field) => {
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
						/>

						<div className="flex gap-2 pt-2">
							<Button type="submit" disabled={isSubmitting}>
								{isSubmitting
									? m.savingDots()
									: isEditing
										? m.updateApplication()
										: m.addApplication()}
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
	);
}
