import { useCallback, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { Lock, Plus, ShieldCheck, Users } from "lucide-react";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogFooter,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
} from "@/components/responsive-dialog";
import { RoleEditor } from "@/components/roles/role-editor";
import { useResourceOptions } from "@/components/roles/permission-editor";
import { useFetch } from "@/lib/api";
import { roleDescription, roleName, type Role } from "@/lib/roles";
import type { UserDetail } from "@/lib/users";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

const NEW_ROLE = "new";

const rolesSearchSchema = z.object({
	role: z.string().optional(),
});

export const Route = createFileRoute("/_authenticated/admin/roles")({
	validateSearch: (search) => rolesSearchSchema.safeParse(search).data ?? {},
	component: RolesPage,
	head: () => ({
		meta: [
			{
				title: `${m.admin()} - ${m.adminRoles()}`,
			},
		],
	}),
});

function RolesPage() {
	const { role: selectedId } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { data: roles, isLoading } = useFetch<Role[]>("/admin/roles");
	const { data: users } = useFetch<UserDetail[]>("/admin/users");
	const options = useResourceOptions();
	const [dirty, setDirty] = useState(false);
	// A role picked while the current one has unsaved changes, waiting for confirmation
	const [pending, setPending] = useState<string | null>(null);
	const onDirtyChange = useCallback((value: boolean) => setDirty(value), []);

	const isNew = selectedId === NEW_ROLE;
	const selected = isNew
		? null
		: (roles?.find((role) => role.id === selectedId) ?? roles?.[0] ?? null);

	const open = (id: string) => {
		setDirty(false);
		setPending(null);
		void navigate({ search: { role: id }, replace: true });
	};

	const select = (id: string) => {
		if (id === (isNew ? NEW_ROLE : selected?.id)) {
			return;
		}
		if (dirty) {
			setPending(id);
			return;
		}
		open(id);
	};

	return (
		<div className="grid gap-6 lg:grid-cols-[18rem_minmax(0,1fr)] lg:items-start">
			<nav
				aria-label={m.adminRoles()}
				className="bg-card flex flex-col gap-1 rounded-xl border p-2 lg:sticky lg:top-4"
			>
				<p className="text-muted-foreground px-2 pt-1 pb-2 text-xs font-medium">{m.roles()}</p>
				{isLoading &&
					Array.from({ length: 3 }, (_, i) => (
						<Skeleton key={i} className="h-14 w-full rounded-lg" />
					))}
				{roles?.map((role) => (
					<RoleListItem
						key={role.id}
						role={role}
						active={!isNew && selected?.id === role.id}
						onSelect={() => select(role.id)}
					/>
				))}
				<Button
					variant={isNew ? "secondary" : "outline"}
					className="mt-2"
					onClick={() => select(NEW_ROLE)}
				>
					<Plus className="size-4" />
					{m.newRole()}
				</Button>
			</nav>

			<ResponsiveDialog open={pending !== null} onOpenChange={(next) => !next && setPending(null)}>
				<ResponsiveDialogContent className="sm:max-w-106.25">
					<ResponsiveDialogHeader>
						<ResponsiveDialogTitle>{m.unsavedChanges()}</ResponsiveDialogTitle>
						<ResponsiveDialogDescription className="py-2">
							{m.roleDiscardChanges()}
						</ResponsiveDialogDescription>
					</ResponsiveDialogHeader>
					<ResponsiveDialogFooter className="flex-col gap-2 sm:flex-row sm:justify-end">
						<Button variant="secondary" onClick={() => setPending(null)}>
							{m.cancel()}
						</Button>
						<Button variant="destructive" onClick={() => pending && open(pending)}>
							{m.discard()}
						</Button>
					</ResponsiveDialogFooter>
				</ResponsiveDialogContent>
			</ResponsiveDialog>

			{roles && (isNew || selected) && (
				<RoleEditor
					key={isNew ? NEW_ROLE : selected?.id}
					role={isNew ? null : selected}
					users={users}
					options={options}
					onDirtyChange={onDirtyChange}
					onSaved={(role) => open(role.id)}
					onDeleted={() => {
						setDirty(false);
						void navigate({ search: {}, replace: true });
					}}
				/>
			)}
		</div>
	);
}

function RoleListItem({
	role,
	active,
	onSelect,
}: {
	role: Role;
	active: boolean;
	onSelect: () => void;
}) {
	const description = roleDescription(role);

	return (
		<button
			type="button"
			onClick={onSelect}
			aria-current={active ? "page" : undefined}
			className={cn(
				"flex w-full items-start gap-3 rounded-lg px-2 py-2 text-left transition-colors",
				"hover:bg-muted focus-visible:ring-ring/50 outline-none focus-visible:ring-[3px]",
				active && "bg-muted",
			)}
		>
			<div className="bg-background mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md border">
				{role.builtIn === "admin" ? (
					<ShieldCheck className="size-4" />
				) : role.builtIn ? (
					<Lock className="size-4" />
				) : (
					<span className="text-xs font-semibold">{role.name.slice(0, 2).toUpperCase()}</span>
				)}
			</div>
			<div className="min-w-0 flex-1">
				<p className="truncate text-sm font-medium">{roleName(role)}</p>
				{description && <p className="text-muted-foreground truncate text-xs">{description}</p>}
			</div>
			<span
				className="text-muted-foreground mt-0.5 flex items-center gap-1 text-xs tabular-nums"
				title={m.roleMemberCount({ count: role.members.length })}
			>
				<Users className="size-3.5" />
				{role.members.length}
			</span>
		</button>
	);
}
