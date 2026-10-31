import { Popover } from "@base-ui/react/popover";
import { useState, type ComponentProps, type ReactElement, type ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { m } from "@/lib/paraglide/messages";
import { cn } from "@/lib/utils";

// Action and navigation controls keep Tooltip so taps still perform their primary action.
export function InfoTooltip({ children }: { children: ReactNode }) {
	const [open, setOpen] = useState(false);
	return (
		<Popover.Root open={open} onOpenChange={setOpen}>
			<Tooltip disabled={open}>{children}</Tooltip>
		</Popover.Root>
	);
}

export function InfoTooltipTrigger({
	render,
	nativeButton = !render,
	...props
}: Omit<ComponentProps<"button">, "ref"> & { render?: ReactElement; nativeButton?: boolean }) {
	return (
		<TooltipTrigger
			render={<Popover.Trigger render={render} nativeButton={nativeButton} />}
			tabIndex={0}
			{...props}
		/>
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
	return (
		<>
			<TooltipContent {...{ className, side, sideOffset, align, alignOffset }}>
				{children}
			</TooltipContent>
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
		</>
	);
}
