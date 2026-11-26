import { useId } from "react";
import { FileText, X } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";

export const maxAdditionalFiles = 64;

const reserved = new Set(["compose.yaml", "compose.override.yaml", ".env"]);

export function additionalFileDestination(composePath: string, repositoryPath: string) {
	if (!composePath || repositoryPath === composePath || repositoryPath.trim() !== repositoryPath) {
		return null;
	}
	const prefix = composePath.slice(0, composePath.lastIndexOf("/") + 1);
	if (!repositoryPath.startsWith(prefix)) {
		return null;
	}
	const destination = repositoryPath.slice(prefix.length);
	const segments = destination.split("/");
	if (
		reserved.has(segments[0].toLowerCase()) ||
		segments.some((segment) => !segment || segment === "." || segment === "..") ||
		destination.includes("\\") ||
		destination.includes(":")
	) {
		return null;
	}
	return destination;
}

interface AdditionalFilesFieldProps {
	composePath: string;
	entries: { path: string; type: "file" | "dir" }[];
	value: string[];
	onChange: (paths: string[]) => void;
	readOnly?: boolean;
}

export function AdditionalFilesField({
	composePath,
	entries,
	value,
	onChange,
	readOnly = false,
}: AdditionalFilesFieldProps) {
	const labelId = useId();
	const descriptionId = useId();
	if (!composePath && value.length === 0) {
		return null;
	}
	const available = entries
		.filter((entry) => entry.type === "file" && !value.includes(entry.path))
		.flatMap(({ path }) => {
			const destination = additionalFileDestination(composePath, path);
			return destination === null ? [] : [{ path, destination }];
		})
		.sort((a, b) => a.destination.localeCompare(b.destination));

	return (
		<Field>
			<FieldLabel id={labelId}>{m.additionalFiles()}</FieldLabel>
			<FieldDescription id={descriptionId}>{m.additionalFilesDescription()}</FieldDescription>
			<div role="group" aria-labelledby={labelId} aria-describedby={descriptionId}>
				{value.length > 0 && (
					<ul className="mb-2 divide-y overflow-hidden rounded-md border">
						{value.map((path) => {
							const destination = additionalFileDestination(composePath, path);
							return (
								<li key={path} className="flex min-w-0 items-center gap-2 px-3 py-1.5">
									<FileText aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
									<code className="min-w-0 flex-1 truncate text-xs" title={path}>
										{destination === null ? path : `./${destination}`}
									</code>
									{!readOnly && (
										<Button
											type="button"
											variant="ghost"
											size="icon-sm"
											aria-label={m.removeAdditionalFile({ path })}
											onClick={() => onChange(value.filter((selected) => selected !== path))}
										>
											<X />
										</Button>
									)}
								</li>
							);
						})}
					</ul>
				)}
				{!readOnly && (
					<Select<string>
						value={null}
						onValueChange={(path) => {
							if (path !== null) {
								onChange([...value, path].sort());
							}
						}}
					>
						<SelectTrigger
							aria-label={m.addAdditionalFile()}
							disabled={available.length === 0 || value.length >= maxAdditionalFiles}
							className="w-full"
						>
							<SelectValue placeholder={m.addAdditionalFile()} />
						</SelectTrigger>
						<SelectContent>
							{available.map(({ path, destination }) => (
								<SelectItem key={path} value={path}>
									{destination}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				)}
			</div>
		</Field>
	);
}
