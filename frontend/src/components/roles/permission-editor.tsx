import { useState } from "react";
import { EllipsisVertical, Plus, TriangleAlert, X } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useFetch } from "@/lib/api";
import type { Agent } from "@/lib/agents";
import type { ApplicationListItem } from "@/lib/applications";
import type { Notification } from "@/lib/notifications";
import type { Repository } from "@/lib/repositories";
import { permissionActions, permissionResources, type PermissionResource } from "@/lib/roles";
import {
	ALL,
	actionDescription,
	actionLabels,
	allResourcesLabels,
	canReadAny,
	isReadImplied,
	removeResource,
	resourceIcons,
	resourceLabels,
	scopedActions,
	setActions,
	type Grants,
} from "@/lib/role-draft";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

export interface ResourceOption {
	id: string;
	name: string;
	description?: string;
}

export type ResourceOptions = Record<PermissionResource, ResourceOption[] | undefined>;

export function useResourceOptions(): ResourceOptions {
	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");
	const { data: repositories } = useFetch<Repository[]>("/repositories");
	const { data: agents } = useFetch<Agent[]>("/agents");
	const { data: notifications } = useFetch<Notification[]>("/notifications");

	return {
		application: applications?.map((app) => ({
			id: app.id,
			name: app.name,
			description: `${app.repositoryName} / ${app.agentName}`,
		})),
		repository: repositories?.map((repo) => ({
			id: repo.id,
			name: repo.name,
			description: repo.url,
		})),
		agent: agents?.map((agent) => ({ id: agent.id, name: agent.name })),
		notification: notifications?.map((notification) => ({
			id: notification.id,
			name: notification.name,
			description: notification.type,
		})),
	};
}

export function PermissionEditor({
	grants,
	onChange,
	options,
}: {
	grants: Grants;
	onChange: (grants: Grants) => void;
	options: ResourceOptions;
}) {
	return (
		<div className="flex flex-col gap-4">
			{permissionResources.map((resource) => (
				<ResourcePermissions
					key={resource}
					resource={resource}
					grants={grants}
					onChange={onChange}
					options={options[resource]}
				/>
			))}
		</div>
	);
}

function ResourcePermissions({
	resource,
	grants,
	onChange,
	options,
}: {
	resource: PermissionResource;
	grants: Grants;
	onChange: (grants: Grants) => void;
	options: ResourceOption[] | undefined;
}) {
	const Icon = resourceIcons[resource];
	const actions = permissionActions[resource];
	const rows = grants[resource];
	const allActions = rows[ALL] ?? [];
	const scopedIds = Object.keys(rows).filter((key) => key !== ALL);
	const available = options?.filter((option) => !(option.id in rows)) ?? [];
	const optionById = new Map(options?.map((option) => [option.id, option]));

	const summary =
		allActions.length === actions.length
			? m.permissionSummaryFull()
			: allActions.length > 0 || scopedIds.length > 0
				? m.permissionSummaryPartial({ count: scopedIds.length })
				: m.permissionSummaryNone();

	return (
		<section
			aria-label={resourceLabels[resource]()}
			className="overflow-hidden rounded-lg border bg-card"
		>
			<header className="flex items-center gap-3 border-b bg-muted/40 px-4 py-3">
				<div className="bg-background flex size-8 items-center justify-center rounded-md border">
					<Icon className="size-4" />
				</div>
				<div className="min-w-0 flex-1">
					<h3 className="text-sm font-medium">{resourceLabels[resource]()}</h3>
					<p className="text-muted-foreground text-xs">{summary}</p>
				</div>
				<DropdownMenu>
					<DropdownMenuTrigger
						render={
							<Button variant="ghost" size="icon" className="size-8">
								<EllipsisVertical className="size-4" />
								<span className="sr-only">
									{m.permissionPresets({ resource: resourceLabels[resource]() })}
								</span>
							</Button>
						}
					/>
					<DropdownMenuContent align="end">
						<DropdownMenuGroup>
							<DropdownMenuItem
								onClick={() => onChange(setActions(grants, resource, ALL, [...actions]))}
							>
								{m.permissionPresetFull()}
							</DropdownMenuItem>
							<DropdownMenuItem
								onClick={() => onChange(setActions(grants, resource, ALL, ["read"]))}
							>
								{m.permissionPresetReadOnly()}
							</DropdownMenuItem>
							<DropdownMenuItem
								onClick={() => onChange({ ...grants, [resource]: {} })}
								variant="destructive"
							>
								{m.permissionPresetNone()}
							</DropdownMenuItem>
						</DropdownMenuGroup>
					</DropdownMenuContent>
				</DropdownMenu>
			</header>

			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="text-muted-foreground text-xs">
							<th className="px-4 py-2 text-left font-medium">
								<span className="sr-only">{m.permissionScope()}</span>
							</th>
							{actions.map((action) => (
								<th key={action} className="px-2 py-2 text-center font-medium whitespace-nowrap">
									<ActionHeader resource={resource} action={action} />
								</th>
							))}
							<th className="w-10" />
						</tr>
					</thead>
					<tbody>
						<PermissionRow
							resource={resource}
							label={allResourcesLabels[resource]()}
							description={m.permissionAllDescription()}
							actions={actions}
							granted={allActions}
							onToggle={(next) => onChange(setActions(grants, resource, ALL, next))}
						/>
						{scopedIds.map((id) => {
							const option = optionById.get(id);
							return (
								<PermissionRow
									key={id}
									resource={resource}
									label={option?.name ?? id}
									description={option?.description}
									actions={actions}
									granted={rows[id]}
									inherited={allActions}
									onToggle={(next) => onChange(setActions(grants, resource, id, next))}
									onRemove={() => onChange(removeResource(grants, resource, id))}
								/>
							);
						})}
					</tbody>
				</table>
			</div>

			<footer className="border-t px-4 py-2">
				<AddResource
					resource={resource}
					options={available}
					onAdd={(id) => onChange(setActions(grants, resource, id, ["read"]))}
				/>
			</footer>

			{resource === "application" && (
				<ApplicationDependencies grants={grants} onChange={onChange} />
			)}
		</section>
	);
}

function ActionHeader({
	resource,
	action,
}: {
	resource: PermissionResource;
	action: (typeof permissionActions)[PermissionResource][number];
}) {
	const description = actionDescription(resource, action);
	if (!description) {
		return <>{actionLabels[action]()}</>;
	}
	return (
		<Tooltip>
			<TooltipTrigger
				render={<span className="cursor-help underline decoration-dotted underline-offset-4" />}
			>
				{actionLabels[action]()}
			</TooltipTrigger>
			<TooltipContent>{description}</TooltipContent>
		</Tooltip>
	);
}

function PermissionRow({
	resource,
	label,
	description,
	actions,
	granted,
	inherited = [],
	onToggle,
	onRemove,
}: {
	resource: PermissionResource;
	label: string;
	description?: string;
	actions: readonly (typeof permissionActions)[PermissionResource][number][];
	granted: (typeof permissionActions)[PermissionResource][number][];
	inherited?: (typeof permissionActions)[PermissionResource][number][];
	onToggle: (next: (typeof permissionActions)[PermissionResource][number][]) => void;
	onRemove?: () => void;
}) {
	const isScoped = onRemove !== undefined;
	const allowed = isScoped ? scopedActions(resource) : actions;
	const readImplied = isReadImplied(granted) || isReadImplied(inherited);

	return (
		<tr className="border-t first:border-t-0 hover:bg-muted/30">
			<td className="px-4 py-2.5">
				<div className="flex min-w-40 flex-col">
					<span className={cn("font-medium", !isScoped && "text-foreground")}>{label}</span>
					{description && (
						<span className="text-muted-foreground max-w-64 truncate text-xs">{description}</span>
					)}
				</div>
			</td>
			{actions.map((action) => {
				const cellLabel = `${actionLabels[action]()}: ${label}`;
				if (!allowed.includes(action)) {
					return (
						<td key={action} className="text-muted-foreground px-2 text-center" aria-hidden>
							–
						</td>
					);
				}

				const fromAll =
					inherited.includes(action) || (action === "read" && isReadImplied(inherited));
				const implied = action === "read" && readImplied && !granted.includes("read");
				const checked = granted.includes(action) || fromAll || implied;
				const locked = fromAll || (action === "read" && readImplied);
				const hint = fromAll
					? m.permissionInheritedFromAll()
					: action === "read" && readImplied
						? m.permissionReadImplied()
						: null;

				const checkbox = (
					<Checkbox
						aria-label={cellLabel}
						checked={checked}
						disabled={locked}
						onCheckedChange={(next) =>
							onToggle(next ? [...granted, action] : granted.filter((a) => a !== action))
						}
					/>
				);

				return (
					<td key={action} className="px-2 text-center">
						<div className="flex justify-center">
							{hint ? (
								<Tooltip>
									<TooltipTrigger render={<span className="inline-flex" />}>
										{checkbox}
									</TooltipTrigger>
									<TooltipContent>{hint}</TooltipContent>
								</Tooltip>
							) : (
								checkbox
							)}
						</div>
					</td>
				);
			})}
			<td className="pr-2 text-right">
				{onRemove && (
					<Button
						variant="ghost"
						size="icon"
						className="size-7"
						onClick={onRemove}
						aria-label={m.permissionRemoveResource({ name: label })}
					>
						<X className="size-4" />
					</Button>
				)}
			</td>
		</tr>
	);
}

function AddResource({
	resource,
	options,
	onAdd,
}: {
	resource: PermissionResource;
	options: ResourceOption[];
	onAdd: (id: string) => void;
}) {
	const [open, setOpen] = useState(false);

	if (!open) {
		return (
			<Button
				variant="ghost"
				size="sm"
				className="text-muted-foreground -ml-2"
				onClick={() => setOpen(true)}
			>
				<Plus className="size-4" />
				{m.permissionAddResource({ resource: resourceLabels[resource]() })}
			</Button>
		);
	}

	return (
		<Combobox
			items={options}
			autoHighlight
			defaultOpen
			value={null}
			itemToStringLabel={(item: ResourceOption) => item.name}
			onValueChange={(item: ResourceOption | null) => {
				if (item) {
					onAdd(item.id);
				}
				setOpen(false);
			}}
			onOpenChange={(next) => {
				if (!next) {
					setOpen(false);
				}
			}}
		>
			<ComboboxInput
				autoFocus
				placeholder={m.permissionSearchResource({ resource: resourceLabels[resource]() })}
				className="max-w-sm"
			/>
			<ComboboxContent>
				<ComboboxEmpty>{m.permissionNoResourcesLeft()}</ComboboxEmpty>
				<ComboboxList>
					{(item: ResourceOption) => (
						<ComboboxItem key={item.id} value={item}>
							<Item className="p-0">
								<ItemContent>
									<ItemTitle className="whitespace-nowrap">{item.name}</ItemTitle>
									{item.description && <ItemDescription>{item.description}</ItemDescription>}
								</ItemContent>
							</Item>
						</ComboboxItem>
					)}
				</ComboboxList>
			</ComboboxContent>
		</Combobox>
	);
}

// Creating and editing applications needs view access to their repository and
// agent, so point out roles that can never use their permission.
function ApplicationDependencies({
	grants,
	onChange,
}: {
	grants: Grants;
	onChange: (grants: Grants) => void;
}) {
	const changesApplications = Object.values(grants.application).some(
		(actions) => actions.includes("create") || actions.includes("update"),
	);
	const missing = (["repository", "agent"] as const).filter(
		(resource) => !canReadAny(grants, resource),
	);
	if (!changesApplications || missing.length === 0) {
		return null;
	}

	return (
		<div className="border-t p-3">
			<Alert>
				<TriangleAlert />
				<AlertTitle>{m.permissionDependencyTitle()}</AlertTitle>
				<AlertDescription className="flex flex-col items-start gap-2">
					<span>
						{m.permissionDependencyDescription({
							resources: missing.map((resource) => resourceLabels[resource]()).join(" & "),
						})}
					</span>
					<Button
						size="sm"
						variant="outline"
						onClick={() =>
							onChange(
								missing.reduce(
									(next, resource) =>
										setActions(next, resource, ALL, [...(next[resource][ALL] ?? []), "read"]),
									grants,
								),
							)
						}
					>
						{m.permissionDependencyFix()}
					</Button>
				</AlertDescription>
			</Alert>
		</div>
	);
}

export function PermissionCountBadge({ count }: { count: number }) {
	return <Badge variant={count > 0 ? "secondary" : "outline"}>{count}</Badge>;
}
