import { DynamicIcon, iconNames, type IconName } from "lucide-react/dynamic";
import { useMemo, useState } from "react";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
import { cn } from "@/lib/utils";

type LucideIconPickerProps = {
	value: IconName;
	onValueChange: (value: IconName) => void;
	placeholder: string;
	emptyMessage: string;
	className?: string;
};

const visibleIconLimit = 84;
const preferredIcons = [
	"server",
	"container",
	"box",
	"boxes",
	"hard-drive",
	"database",
	"cloud",
	"network",
	"router",
	"workflow",
	"git-branch",
	"git-pull-request",
	"package",
	"rocket",
	"terminal",
	"monitor",
	"cpu",
	"settings",
	"shield",
	"key-round",
	"lock",
	"activity",
	"gauge",
	"webhook",
] satisfies IconName[];

function formatIconName(name: IconName) {
	return name
		.split("-")
		.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
		.join(" ");
}

const iconOptions = iconNames.map((name) => {
	const label = formatIconName(name);

	return {
		name,
		label,
		searchText: `${name} ${label}`.toLocaleLowerCase(),
	};
});

const preferredIconSet = new Set<IconName>(preferredIcons);
const iconLabelByName = new Map(iconOptions.map((option) => [option.name, option.label]));

function getIconLabel(name: IconName) {
	return iconLabelByName.get(name) ?? name;
}

function getVisibleIcons(query: string, value: IconName) {
	const normalizedQuery = query.trim().toLocaleLowerCase();
	const matchingIcons = normalizedQuery
		? iconOptions
				.filter((option) => option.searchText.includes(normalizedQuery))
				.map((option) => option.name)
		: [
				...preferredIcons,
				...iconOptions
					.filter((option) => !preferredIconSet.has(option.name))
					.map((option) => option.name),
			];

	return Array.from(new Set([value, ...matchingIcons])).slice(0, visibleIconLimit);
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
				if (nextValue) {
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
					<DynamicIcon name={value} className="size-4" />
				</div>
			</ComboboxInput>
			<ComboboxContent className="min-w-72 pointer-events-auto">
				<ComboboxEmpty>{emptyMessage}</ComboboxEmpty>
				<ComboboxList className="grid max-h-72 grid-cols-2 gap-1 sm:grid-cols-3">
					{(name) => (
						<ComboboxItem key={name} value={name} className="h-9 min-w-0 pr-2">
							<DynamicIcon name={name} className="size-4" />
							<span className="truncate">{getIconLabel(name)}</span>
						</ComboboxItem>
					)}
				</ComboboxList>
			</ComboboxContent>
		</Combobox>
	);
}

export type { IconName as LucideIconName };
