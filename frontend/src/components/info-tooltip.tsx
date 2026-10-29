import { Popover } from "@base-ui/react/popover";
import {
	createContext,
	useContext,
	useSyncExternalStore,
	type ComponentProps,
	type ReactElement,
	type ReactNode,
} from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

// Use input capabilities rather than viewport width, including large touch-only tablets.
const touchQuery = "(hover: none), (pointer: coarse)";
const TouchContext = createContext(false);

function subscribe(onChange: () => void) {
	const query = window.matchMedia(touchQuery);
	query.addEventListener("change", onChange);
	return () => query.removeEventListener("change", onChange);
}

function getSnapshot() {
	return window.matchMedia(touchQuery).matches;
}

function getServerSnapshot() {
	return false;
}

export function InfoTooltip({ children }: { children: ReactNode }) {
	const touch = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
	return (
		<TouchContext value={touch}>
			{touch ? <Popover.Root>{children}</Popover.Root> : <Tooltip>{children}</Tooltip>}
		</TouchContext>
	);
}

export function InfoTooltipTrigger({
	render,
	nativeButton = !render,
	...props
}: Omit<ComponentProps<"button">, "ref"> & { render?: ReactElement; nativeButton?: boolean }) {
	const touch = useContext(TouchContext);
	return touch ? (
		<Popover.Trigger render={render} nativeButton={nativeButton} {...props} />
	) : (
		<TooltipTrigger render={render} tabIndex={0} {...props} />
	);
}

export function InfoTooltipContent({
	children,
	className,
	side = "top",
	sideOffset = 4,
	align = "center",
	alignOffset = 0,
}: {
	children: ReactNode;
	className?: string;
} & Pick<Popover.Positioner.Props, "side" | "sideOffset" | "align" | "alignOffset">) {
	const touch = useContext(TouchContext);
	if (!touch) {
		return (
			<TooltipContent {...{ className, side, sideOffset, align, alignOffset }}>
				{children}
			</TooltipContent>
		);
	}
	return (
		<Popover.Portal>
			<Popover.Positioner
				side={side}
				sideOffset={sideOffset}
				align={align}
				alignOffset={alignOffset}
				className="isolate z-50"
			>
				<Popover.Popup
					aria-label={m.details()}
					className={cn(
						"flex w-fit max-w-[min(20rem,calc(100vw-2rem))] flex-col gap-1.5 rounded-md bg-foreground px-3 py-2 text-xs text-background shadow-md outline-none",
						className,
					)}
				>
					{children}
				</Popover.Popup>
			</Popover.Positioner>
		</Popover.Portal>
	);
}
