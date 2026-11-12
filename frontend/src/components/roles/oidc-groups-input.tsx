import { useState } from "react";
import { Plus, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { useFetch } from "@/lib/api";
import type { OIDCProviderDetail } from "@/lib/oidc";
import type { RoleOIDCGroup } from "@/lib/roles";
import { m } from "@/lib/paraglide/messages";

export function OIDCGroupsInput({
	groups,
	onChange,
}: {
	groups: RoleOIDCGroup[];
	onChange: (groups: RoleOIDCGroup[]) => void;
}) {
	const { data: providers } = useFetch<OIDCProviderDetail[]>("/admin/oidc-providers");
	// Groups are only read from providers with a groups claim
	const syncing = providers?.filter((provider) => provider.groupsClaim !== "") ?? [];
	const [providerId, setProviderId] = useState<string | null>(null);
	const [value, setValue] = useState("");
	const selectedProvider = providerId ?? syncing[0]?.id ?? null;
	const providerName = (id: string) =>
		providers?.find((provider) => provider.id === id)?.name ?? m.unknown();

	const add = () => {
		const group = value.trim();
		if (
			group &&
			selectedProvider &&
			!groups.some((g) => g.providerId === selectedProvider && g.group === group)
		) {
			onChange([...groups, { providerId: selectedProvider, group }]);
		}
		setValue("");
	};

	return (
		<div className="flex flex-col gap-3">
			{providers && syncing.length === 0 ? (
				<p className="text-muted-foreground rounded-lg border border-dashed p-4 text-sm">
					{m.roleOidcNoProviders()}
				</p>
			) : (
				<form
					className="flex flex-col gap-2 sm:flex-row"
					onSubmit={(e) => {
						e.preventDefault();
						add();
					}}
				>
					<Select
						value={selectedProvider}
						onValueChange={(next) => setProviderId(next)}
						items={syncing.map((provider) => ({ label: provider.name, value: provider.id }))}
					>
						<SelectTrigger aria-label={m.roleOidcGroupProvider()} className="sm:w-48">
							<SelectValue placeholder={m.roleOidcGroupProvider()} />
						</SelectTrigger>
						<SelectContent>
							{syncing.map((provider) => (
								<SelectItem key={provider.id} value={provider.id}>
									{provider.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					<Input
						value={value}
						maxLength={255}
						aria-label={m.roleOidcGroups()}
						placeholder={m.roleOidcGroupPlaceholder()}
						className="sm:max-w-xs"
						onChange={(e) => setValue(e.target.value)}
						// A typed group is not lost when saving without clicking add
						onBlur={add}
					/>
					<Button type="submit" variant="outline" disabled={!value.trim() || !selectedProvider}>
						<Plus className="size-4" />
						{m.add()}
					</Button>
				</form>
			)}
			{groups.length > 0 && (
				<ul className="flex flex-wrap gap-2" aria-label={m.roleOidcGroups()}>
					{groups.map((group) => (
						<li key={`${group.providerId}:${group.group}`}>
							<Badge variant="secondary" className="h-7 gap-1 pr-1 pl-2.5 text-xs">
								<span className="text-muted-foreground">{providerName(group.providerId)}:</span>
								<span className="font-mono">{group.group}</span>
								<button
									type="button"
									className="hover:bg-background/60 rounded-full p-0.5"
									onClick={() => onChange(groups.filter((g) => g !== group))}
									aria-label={m.roleRemoveOidcGroup({ group: group.group })}
								>
									<X className="size-3" />
								</button>
							</Badge>
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
