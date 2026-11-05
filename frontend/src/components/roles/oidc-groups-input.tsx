import { useState } from "react";
import { Plus, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { m } from "@/lib/paraglide/messages";

export function OIDCGroupsInput({
	groups,
	onChange,
}: {
	groups: string[];
	onChange: (groups: string[]) => void;
}) {
	const [value, setValue] = useState("");

	const add = () => {
		const group = value.trim();
		if (group && !groups.includes(group)) {
			onChange([...groups, group]);
		}
		setValue("");
	};

	return (
		<div className="flex flex-col gap-3">
			<form
				className="flex gap-2 sm:max-w-sm"
				onSubmit={(e) => {
					e.preventDefault();
					add();
				}}
			>
				<Input
					value={value}
					maxLength={255}
					aria-label={m.roleOidcGroups()}
					placeholder={m.roleOidcGroupPlaceholder()}
					onChange={(e) => setValue(e.target.value)}
				/>
				<Button type="submit" variant="outline" disabled={!value.trim()}>
					<Plus className="size-4" />
					{m.add()}
				</Button>
			</form>
			{groups.length > 0 && (
				<ul className="flex flex-wrap gap-2" aria-label={m.roleOidcGroups()}>
					{groups.map((group) => (
						<li key={group}>
							<Badge variant="secondary" className="h-7 gap-1 pr-1 pl-2.5 font-mono text-xs">
								{group}
								<button
									type="button"
									className="hover:bg-background/60 rounded-full p-0.5"
									onClick={() => onChange(groups.filter((g) => g !== group))}
									aria-label={m.roleRemoveOidcGroup({ group })}
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
