import { useId, useMemo, useState } from "react";
import { ArrowUp, Check, ChevronDown, FileText, Folder, Search, X } from "lucide-react";
import { m } from "@/lib/paraglide/messages";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

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
					<RepositoryFileBrowser
						key={composePath}
						composePath={composePath}
						entries={entries}
						value={value}
						onChange={onChange}
					/>
				)}
			</div>
		</Field>
	);
}

function RepositoryFileBrowser({
	composePath,
	entries,
	value,
	onChange,
}: Omit<AdditionalFilesFieldProps, "readOnly">) {
	const [folder, setFolder] = useState("");
	const [query, setQuery] = useState("");
	const files = useMemo(
		() =>
			entries
				.flatMap(({ type, path }) => {
					const destination = type === "file" ? additionalFileDestination(composePath, path) : null;
					return destination === null ? [] : [{ path, destination }];
				})
				.sort((a, b) => a.destination.localeCompare(b.destination)),
		[composePath, entries],
	);
	const search = query.trim().toLowerCase();
	const prefix = folder ? folder + "/" : "";
	const folders = new Set<string>();
	const visibleFiles = files.filter(({ destination }) => {
		if (search) {
			return destination.toLowerCase().includes(search);
		}
		if (!destination.startsWith(prefix)) {
			return false;
		}
		const relative = destination.slice(prefix.length);
		const slash = relative.indexOf("/");
		if (slash === -1) {
			return true;
		}
		folders.add(relative.slice(0, slash));
		return false;
	});

	return (
		<Collapsible>
			<CollapsibleTrigger
				render={<Button type="button" variant="outline" className="w-full justify-between" />}
			>
				{m.browseAdditionalFiles()}
				<ChevronDown aria-hidden="true" />
			</CollapsibleTrigger>
			<CollapsibleContent className="mt-2 overflow-hidden rounded-md border">
				<div className="relative p-2">
					<Search
						aria-hidden="true"
						className="pointer-events-none absolute top-1/2 left-4 size-4 -translate-y-1/2 text-muted-foreground"
					/>
					<Input
						type="search"
						aria-label={m.searchAdditionalFiles()}
						placeholder={m.searchAdditionalFiles()}
						value={query}
						onChange={(event) => setQuery(event.currentTarget.value)}
						className="pl-8"
					/>
				</div>
				{!search && (
					<div className="flex min-w-0 items-center gap-2 border-b px-2 pb-2">
						{folder && (
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								aria-label={m.additionalFilesParentFolder()}
								onClick={() => setFolder(folder.slice(0, Math.max(0, folder.lastIndexOf("/"))))}
							>
								<ArrowUp aria-hidden="true" />
							</Button>
						)}
						<span
							className="truncate px-2 font-mono text-xs text-muted-foreground"
							title={`./${folder}`}
						>
							./{folder}
						</span>
					</div>
				)}
				<div key={search ? "search" : folder} className="max-h-48 overflow-y-auto p-1">
					{[...folders].map((name) => (
						<Button
							key={name}
							type="button"
							variant="ghost"
							size="sm"
							className="w-full justify-start"
							onClick={() => setFolder(prefix + name)}
						>
							<Folder aria-hidden="true" />
							<span className="truncate">{name}</span>
						</Button>
					))}
					{visibleFiles.map(({ path, destination }) => {
						const selected = value.includes(path);
						return (
							<Button
								key={path}
								type="button"
								variant="ghost"
								size="sm"
								className="w-full justify-start aria-pressed:bg-accent"
								title={destination}
								aria-pressed={selected}
								disabled={!selected && value.length >= maxAdditionalFiles}
								onClick={() =>
									onChange(
										selected ? value.filter((entry) => entry !== path) : [...value, path].sort(),
									)
								}
							>
								<FileText aria-hidden="true" />
								<span className="min-w-0 flex-1 truncate text-left">
									{search ? destination : destination.slice(prefix.length)}
								</span>
								{selected && <Check aria-hidden="true" />}
							</Button>
						);
					})}
					{folders.size === 0 && visibleFiles.length === 0 && (
						<p className="px-2 py-3 text-sm text-muted-foreground">{m.noResults()}</p>
					)}
				</div>
			</CollapsibleContent>
		</Collapsible>
	);
}
