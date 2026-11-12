import { useRef, type RefObject } from "react";
import { Search, XIcon } from "lucide-react";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupButton,
	InputGroupInput,
} from "@/components/ui/input-group";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

interface SearchFieldProps {
	value: string;
	onChange: (value: string) => void;
	label: string;
	placeholder: string;
	className?: string;
	/** Lets the page focus the field, e.g. after resetting the search from an empty state. */
	inputRef?: RefObject<HTMLInputElement | null>;
}

export function SearchField({
	value,
	onChange,
	label,
	placeholder,
	className,
	inputRef: externalInputRef,
}: SearchFieldProps) {
	const ownInputRef = useRef<HTMLInputElement>(null);
	const inputRef = externalInputRef ?? ownInputRef;

	const clear = () => {
		onChange("");
		inputRef.current?.focus();
	};

	return (
		<InputGroup role="search" className={cn("bg-card", className)}>
			<InputGroupAddon>
				<Search />
			</InputGroupAddon>
			<InputGroupInput
				ref={inputRef}
				type="search"
				value={value}
				aria-label={label}
				onChange={(event) => onChange(event.target.value)}
				onKeyDown={(event) => {
					if (event.key === "Escape" && value) {
						event.preventDefault();
						onChange("");
					}
				}}
				placeholder={placeholder}
				// The native cancel button only exists in some browsers and ignores the theme, the
				// clear button below replaces it everywhere.
				className="[&::-webkit-search-cancel-button]:appearance-none"
			/>
			{value ? (
				<InputGroupAddon align="inline-end">
					<InputGroupButton size="icon-xs" aria-label={m.searchClear()} onClick={clear}>
						<XIcon />
					</InputGroupButton>
				</InputGroupAddon>
			) : null}
		</InputGroup>
	);
}
