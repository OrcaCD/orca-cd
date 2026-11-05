import { Users, X } from "lucide-react";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { RoleMember } from "@/lib/roles";
import type { UserDetail } from "@/lib/users";
import { m } from "@/lib/paraglide/messages";

function initials(name: string): string {
	return name
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((part) => part[0]?.toUpperCase())
		.join("");
}

export function RoleMembers({
	userIds,
	oidcMembers,
	users,
	onChange,
}: {
	userIds: string[];
	// Members from OIDC groups are synced on login and can't be removed here
	oidcMembers: RoleMember[];
	users: UserDetail[] | undefined;
	onChange: (userIds: string[]) => void;
}) {
	const userById = new Map(users?.map((user) => [user.id, user]));
	const oidcIds = new Set(oidcMembers.map((member) => member.id));
	const members = [
		...userIds.map((id) => {
			const user = userById.get(id);
			return { id, name: user?.name ?? id, email: user?.email ?? "", fromOIDC: oidcIds.has(id) };
		}),
		...oidcMembers
			.filter((member) => !userIds.includes(member.id))
			.map((member) => ({ ...member, fromOIDC: true, manual: false })),
	].sort((a, b) => a.name.localeCompare(b.name));
	const candidates = users?.filter((user) => !userIds.includes(user.id)) ?? [];

	return (
		<div className="flex flex-col gap-3">
			<Combobox
				items={candidates}
				autoHighlight
				value={null}
				itemToStringLabel={(user: UserDetail) => user.name}
				onValueChange={(user: UserDetail | null) => {
					if (user) {
						onChange([...userIds, user.id]);
					}
				}}
			>
				<ComboboxInput placeholder={m.roleAddMember()} className="sm:max-w-sm" />
				<ComboboxContent>
					<ComboboxEmpty>{m.roleNoUsersLeft()}</ComboboxEmpty>
					<ComboboxList>
						{(user: UserDetail) => (
							<ComboboxItem key={user.id} value={user}>
								<Item className="p-0">
									<ItemContent>
										<ItemTitle className="whitespace-nowrap">{user.name}</ItemTitle>
										<ItemDescription>{user.email}</ItemDescription>
									</ItemContent>
								</Item>
							</ComboboxItem>
						)}
					</ComboboxList>
				</ComboboxContent>
			</Combobox>

			{members.length === 0 ? (
				<div className="text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm">
					{m.roleNoMembers()}
				</div>
			) : (
				<ul className="divide-y rounded-lg border" aria-label={m.roleMembers()}>
					{members.map((member) => {
						const manual = userIds.includes(member.id);
						return (
							<li key={member.id} className="flex items-center gap-3 px-3 py-2">
								<Avatar className="size-8">
									<AvatarFallback>{initials(member.name)}</AvatarFallback>
								</Avatar>
								<div className="min-w-0 flex-1">
									<p className="truncate text-sm font-medium">{member.name}</p>
									<p className="text-muted-foreground truncate text-xs">{member.email}</p>
								</div>
								{member.fromOIDC && (
									<Tooltip>
										<TooltipTrigger
											render={
												<Badge variant="outline">
													<Users />
													{m.oidcGroup()}
												</Badge>
											}
										/>
										<TooltipContent>{m.roleMemberFromOidc()}</TooltipContent>
									</Tooltip>
								)}
								{manual ? (
									<Button
										variant="ghost"
										size="icon"
										className="size-7"
										onClick={() => onChange(userIds.filter((id) => id !== member.id))}
										aria-label={m.roleRemoveMember({ name: member.name })}
									>
										<X className="size-4" />
									</Button>
								) : (
									<span className="size-7" />
								)}
							</li>
						);
					})}
				</ul>
			)}
		</div>
	);
}
