import { useEffect, useState } from "react";
import { Lock, ShieldCheck, Trash2 } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/toast";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import { PermissionEditor, type ResourceOptions } from "@/components/roles/permission-editor";
import { RoleMembers } from "@/components/roles/role-members";
import {
	createRole,
	deleteRole,
	roleDescription,
	roleName,
	updateRole,
	type Role,
} from "@/lib/roles";
import { countGrants, draftKey, toDraft, toRequest, type RoleDraft } from "@/lib/role-draft";
import type { UserDetail } from "@/lib/users";
import { m } from "@/lib/paraglide/messages";

export function RoleEditor({
	role,
	users,
	options,
	onSaved,
	onDeleted,
	onDirtyChange,
}: {
	// null creates a new role
	role: Role | null;
	users: UserDetail[] | undefined;
	options: ResourceOptions;
	onSaved: (role: Role) => void;
	onDeleted: () => void;
	onDirtyChange: (dirty: boolean) => void;
}) {
	const [draft, setDraft] = useState<RoleDraft>(() => toDraft(role));
	const [saving, setSaving] = useState(false);
	const [nameError, setNameError] = useState<string | null>(null);

	const isAdmin = role?.builtIn === "admin";
	const isBuiltIn = role !== null && role.builtIn !== null;
	const dirty = role === null || draftKey(draft) !== draftKey(toDraft(role));
	const oidcMembers = role?.members.filter((member) => member.sources.includes("oidc")) ?? [];
	const memberCount = new Set([...draft.userIds, ...oidcMembers.map((member) => member.id)]).size;

	useEffect(() => {
		onDirtyChange(role !== null && dirty);
	}, [role, dirty, onDirtyChange]);

	async function save() {
		const name = isBuiltIn ? role.name : draft.name.trim();
		if (!name) {
			setNameError(m.roleNameRequired());
			return;
		}
		setNameError(null);
		setSaving(true);
		try {
			const request = { ...toRequest(draft), name };
			const saved = role ? await updateRole(role.id, request) : await createRole(request);
			setDraft(toDraft(saved));
			toast.add({ type: "success", title: role ? m.roleSaved() : m.roleCreated() });
			onSaved(saved);
		} catch (err) {
			toast.add({ type: "error", title: err instanceof Error ? err.message : m.failedSaveRole() });
		} finally {
			setSaving(false);
		}
	}

	async function remove() {
		if (!role) {
			return;
		}
		try {
			await deleteRole(role.id);
			toast.add({ type: "success", title: m.roleDeleted() });
			onDeleted();
		} catch (err) {
			toast.add({
				type: "error",
				title: err instanceof Error ? err.message : m.failedDeleteRole(),
			});
		}
	}

	return (
		<div className="bg-card flex min-w-0 flex-col rounded-xl border">
			<div className="flex flex-col gap-4 border-b p-4 sm:flex-row sm:items-start sm:p-6">
				<div className="min-w-0 flex-1">
					{isBuiltIn ? (
						<div className="flex flex-col gap-1">
							<div className="flex items-center gap-2">
								{isAdmin && <ShieldCheck className="size-5" />}
								<h2 className="text-lg font-semibold">{roleName(role)}</h2>
								<Badge variant="outline">
									<Lock />
									{m.roleBuiltIn()}
								</Badge>
							</div>
							<p className="text-muted-foreground text-sm">{roleDescription(role)}</p>
						</div>
					) : (
						<FieldGroup className="gap-3">
							<Field data-invalid={nameError !== null}>
								<Label htmlFor="role-name">{m.name()}</Label>
								<Input
									id="role-name"
									value={draft.name}
									maxLength={64}
									placeholder={m.roleNamePlaceholder()}
									autoFocus={role === null}
									onChange={(e) => setDraft({ ...draft, name: e.target.value })}
								/>
								{nameError && <FieldError>{nameError}</FieldError>}
							</Field>
							<Field>
								<Label htmlFor="role-description">{m.description()}</Label>
								<Textarea
									id="role-description"
									value={draft.description}
									maxLength={256}
									rows={2}
									placeholder={m.roleDescriptionPlaceholder()}
									onChange={(e) => setDraft({ ...draft, description: e.target.value })}
								/>
							</Field>
						</FieldGroup>
					)}
				</div>
				{role && !isBuiltIn && (
					<ConfirmationDialog
						onConfirm={() => void remove()}
						title={m.deleteRoleTitle()}
						description={m.deleteRoleDescription({ name: role.name, count: memberCount })}
						triggerText={
							<>
								<Trash2 className="size-4" />
								{m.deleteRole()}
							</>
						}
						triggerProps={{ variant: "destructive" }}
					/>
				)}
			</div>

			<Tabs defaultValue="permissions" className="p-4 sm:p-6">
				<TabsList>
					<TabsTrigger value="permissions">
						{m.rolePermissions()}
						{!isAdmin && <Badge variant="secondary">{countGrants(draft.grants)}</Badge>}
					</TabsTrigger>
					<TabsTrigger value="members">
						{m.roleMembers()}
						<Badge variant="secondary">{memberCount}</Badge>
					</TabsTrigger>
				</TabsList>
				<TabsContent value="permissions" className="pt-3">
					{isAdmin ? (
						<Alert>
							<ShieldCheck />
							<AlertTitle>{m.roleAdminFullAccess()}</AlertTitle>
							<AlertDescription>{m.roleAdminFullAccessDescription()}</AlertDescription>
						</Alert>
					) : (
						<PermissionEditor
							grants={draft.grants}
							options={options}
							onChange={(grants) => setDraft({ ...draft, grants })}
						/>
					)}
				</TabsContent>
				<TabsContent value="members" className="pt-3">
					<RoleMembers
						userIds={draft.userIds}
						oidcMembers={oidcMembers}
						users={users}
						onChange={(userIds) => setDraft({ ...draft, userIds })}
					/>
				</TabsContent>
			</Tabs>

			<div className="bg-card/95 sticky bottom-0 mt-auto flex items-center justify-end gap-2 rounded-b-xl border-t px-4 py-3 backdrop-blur sm:px-6">
				{role && dirty && (
					<span className="text-muted-foreground mr-auto flex items-center gap-2 text-sm">
						<span className="size-2 rounded-full bg-amber-500" />
						{m.unsavedChanges()}
					</span>
				)}
				{role && (
					<Button
						variant="outline"
						disabled={!dirty || saving}
						onClick={() => setDraft(toDraft(role))}
					>
						{m.discard()}
					</Button>
				)}
				<Button disabled={!dirty || saving} onClick={() => void save()}>
					{saving ? m.savingDots() : role ? m.saveChanges() : m.createRole()}
				</Button>
			</div>
		</div>
	);
}
