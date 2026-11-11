import { X } from "lucide-react";
import { useState } from "react";
import z from "zod";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { m } from "@/lib/paraglide/messages";

// A registry host with an optional port, without a path.
const registrySchema = z
	.string()
	.trim()
	.toLowerCase()
	.regex(/^[a-z0-9.-]+(:\d+)?$/, m.validationRegistryHost());

// RegistryListInput edits a list of registry hosts like registry.example.com:5000.
export function RegistryListInput({
	id,
	value,
	onChange,
}: {
	id: string;
	value: string[];
	onChange: (registries: string[]) => void;
}) {
	const [newRegistry, setNewRegistry] = useState("");
	const [error, setError] = useState<string | null>(null);

	function addRegistry() {
		const parsed = registrySchema.safeParse(newRegistry);
		if (!parsed.success) {
			setError(parsed.error.issues[0]?.message ?? m.validationRegistryHost());
			return;
		}
		if (!value.includes(parsed.data)) {
			onChange([...value, parsed.data]);
		}
		setNewRegistry("");
		setError(null);
	}

	return (
		<div className="space-y-2">
			{value.length > 0 && (
				<div className="flex flex-wrap gap-2">
					{value.map((registry) => (
						<Badge key={registry} variant="secondary" className="gap-1 font-mono">
							{registry}
							<button
								type="button"
								className="rounded-sm hover:text-foreground"
								aria-label={m.releaseAgeRemoveRegistry({ registry })}
								onClick={() => onChange(value.filter((r) => r !== registry))}
							>
								<X className="h-3 w-3" />
							</button>
						</Badge>
					))}
				</div>
			)}
			<div className="flex gap-2">
				<Input
					id={id}
					placeholder="registry.example.com:5000"
					value={newRegistry}
					aria-invalid={error !== null}
					onChange={(e) => {
						setNewRegistry(e.target.value);
						setError(null);
					}}
					onKeyDown={(e) => {
						if (e.key === "Enter") {
							e.preventDefault();
							addRegistry();
						}
					}}
				/>
				<Button type="button" variant="outline" onClick={addRegistry}>
					{m.releaseAgeAddRegistry()}
				</Button>
			</div>
			{error && <FieldError errors={[{ message: error }]} />}
		</div>
	);
}
