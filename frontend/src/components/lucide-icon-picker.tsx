import {
	Box,
	Boxes,
	Cloud,
	Container,
	Database,
	GitBranch,
	GitPullRequest,
	HardDrive,
	Network,
	Package,
	Rocket,
	Server,
	Shield,
	Terminal,
	Workflow,
	type LucideIcon,
} from "lucide-react";
import { type ComponentProps, useMemo, useState } from "react";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import { cn } from "@/lib/utils";

const allowedIconOptions = [
	{ name: "server", label: "Server", icon: Server },
	{ name: "container", label: "Container", icon: Container },
	{ name: "box", label: "Box", icon: Box },
	{ name: "boxes", label: "Boxes", icon: Boxes },
	{ name: "hard-drive", label: "Hard Drive", icon: HardDrive },
	{ name: "database", label: "Database", icon: Database },
	{ name: "cloud", label: "Cloud", icon: Cloud },
	{ name: "network", label: "Network", icon: Network },
	{ name: "workflow", label: "Workflow", icon: Workflow },
	{ name: "git-branch", label: "Git Branch", icon: GitBranch },
	{ name: "git-pull-request", label: "Git Pull Request", icon: GitPullRequest },
	{ name: "package", label: "Package", icon: Package },
	{ name: "rocket", label: "Rocket", icon: Rocket },
	{ name: "terminal", label: "Terminal", icon: Terminal },
	{ name: "shield", label: "Shield", icon: Shield },
] as const satisfies readonly {
	name: string;
	label: string;
	icon: LucideIcon;
}[];

type LucideIconName = (typeof allowedIconOptions)[number]["name"];

type LucideIconPickerProps = {
	value: LucideIconName;
	onValueChange: (value: LucideIconName) => void;
	placeholder: string;
	emptyMessage: string;
	className?: string;
};

const fallbackIconName = "server" satisfies LucideIconName;
const iconOptions = allowedIconOptions.map((option) => ({
	...option,
	searchText: `${option.name} ${option.label}`.toLocaleLowerCase(),
}));
const iconOptionByName = new Map<LucideIconName, (typeof allowedIconOptions)[number]>(
	allowedIconOptions.map((option) => [option.name, option]),
);

function isLucideIconName(name: string | null | undefined): name is LucideIconName {
	return typeof name === "string" && iconOptionByName.has(name as LucideIconName);
}

function normalizeIconName(name: string | null | undefined): LucideIconName {
	return isLucideIconName(name) ? name : fallbackIconName;
}

function getIconLabel(name: string) {
	return iconOptionByName.get(normalizeIconName(name))?.label ?? name;
}

function getVisibleIcons(query: string, value: LucideIconName) {
	const normalizedQuery = query.trim().toLocaleLowerCase();
	const matchingIcons = normalizedQuery
		? iconOptions
				.filter((option) => option.searchText.includes(normalizedQuery))
				.map((option) => option.name)
		: iconOptions.map((option) => option.name);

	return Array.from(new Set([normalizeIconName(value), ...matchingIcons]));
}

function StaticLucideIcon({
	name,
	...props
}: Omit<ComponentProps<LucideIcon>, "name"> & {
	name: string | null | undefined;
}) {
	const Icon = iconOptionByName.get(normalizeIconName(name))?.icon ?? Server;

	return <Icon {...props} />;
}

export default function LucideIconPicker({
	value,
	onValueChange,
	placeholder,
	emptyMessage,
	className,
}: LucideIconPickerProps) {
	const [query, setQuery] = useState("");
	const visibleIcons = useMemo(() => getVisibleIcons(query, value), [query, value]);

	return (
		<Combobox
			items={visibleIcons}
			filter={null}
			itemToStringLabel={getIconLabel}
			value={value}
			onInputValueChange={setQuery}
			onValueChange={(nextValue) => {
				if (isLucideIconName(nextValue)) {
					onValueChange(nextValue);
					setQuery("");
				}
			}}
		>
			<ComboboxInput
				className={cn("w-full", className)}
				placeholder={placeholder}
				aria-label={placeholder}
			>
				<div className="pointer-events-none order-first flex h-full items-center pl-2 text-muted-foreground">
					<StaticLucideIcon name={value} className="size-4" />
				</div>
			</ComboboxInput>
			<ComboboxContent className="min-w-72 pointer-events-auto">
				<ComboboxEmpty>{emptyMessage}</ComboboxEmpty>
				<ComboboxList className="grid max-h-72 grid-cols-2 gap-1 sm:grid-cols-3">
					{(name) => (
						<ComboboxItem key={name} value={name} className="h-9 min-w-0 pr-2">
							<StaticLucideIcon name={name} className="size-4" />
							<span className="truncate">{getIconLabel(name)}</span>
						</ComboboxItem>
					)}
				</ComboboxList>
			</ComboboxContent>
		</Combobox>
	);
}

export { StaticLucideIcon };
export type { LucideIconName };
