import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { roleName, type UserRole } from "@/lib/roles";
import { m } from "@/lib/paraglide/messages";
import { ShieldCheck, Users } from "lucide-react";

export function RoleBadge({ role }: { role: UserRole }) {
	const fromOIDC = role.sources.includes("oidc");
	const badge = (
		<Badge variant={role.builtIn === "admin" ? "default" : "secondary"}>
			{role.builtIn === "admin" && <ShieldCheck />}
			{roleName(role)}
			{fromOIDC && <Users className="opacity-70" />}
		</Badge>
	);

	if (!fromOIDC) {
		return badge;
	}

	return (
		<Tooltip>
			<TooltipTrigger render={badge} />
			<TooltipContent>{m.roleFromOidcGroup()}</TooltipContent>
		</Tooltip>
	);
}

export function RoleBadges({ roles }: { roles: UserRole[] }) {
	if (roles.length === 0) {
		return <span className="text-muted-foreground text-sm">{m.noRoles()}</span>;
	}

	return (
		<div className="flex max-w-sm flex-wrap gap-1">
			{roles.map((role) => (
				<RoleBadge key={role.id} role={role} />
			))}
		</div>
	);
}
