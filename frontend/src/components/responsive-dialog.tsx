import * as React from "react";
import { Drawer as DrawerPrimitive } from "@base-ui/react/drawer";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import {
	Drawer,
	DrawerContent,
	DrawerDescription,
	DrawerTitle,
	DrawerTrigger,
} from "@/components/ui/drawer";
import { useIsMobile } from "@/hooks/use-mobile";

const ResponsiveDialogContext = React.createContext(false);

function useIsDrawer() {
	return React.useContext(ResponsiveDialogContext);
}

function ResponsiveDialog({
	open,
	onOpenChange,
	onOpenChangeComplete,
	modal,
	disablePointerDismissal = false,
	children,
}: {
	open: boolean;
	onOpenChange?: (open: boolean) => void;
	onOpenChangeComplete?: (open: boolean) => void;
	modal?: boolean;
	disablePointerDismissal?: boolean;
	children: React.ReactNode;
}) {
	const isMobile = useIsMobile();

	return (
		<ResponsiveDialogContext.Provider value={isMobile}>
			{isMobile ? (
				<Drawer
					open={open}
					onOpenChange={(next, details) => {
						// A swipe is as easy to trigger by accident as an outside press.
						if (!next && disablePointerDismissal && details.reason === "swipe") {
							details.cancel();
							return;
						}
						onOpenChange?.(next);
					}}
					onOpenChangeComplete={onOpenChangeComplete}
					modal={modal}
					disablePointerDismissal={disablePointerDismissal}
					showSwipeHandle={!disablePointerDismissal}
				>
					{children}
				</Drawer>
			) : (
				<Dialog
					open={open}
					onOpenChange={(next) => onOpenChange?.(next)}
					onOpenChangeComplete={onOpenChangeComplete}
					modal={modal}
					disablePointerDismissal={disablePointerDismissal}
				>
					{children}
				</Dialog>
			)}
		</ResponsiveDialogContext.Provider>
	);
}

function ResponsiveDialogTrigger(
	props: Omit<React.ComponentProps<typeof DialogTrigger>, "handle">,
) {
	return useIsDrawer() ? <DrawerTrigger {...props} /> : <DialogTrigger {...props} />;
}

function ResponsiveDialogContent({
	className,
	showCloseButton,
	onPaste,
	finalFocus,
	children,
}: {
	className?: string;
	showCloseButton?: boolean;
	onPaste?: React.ClipboardEventHandler<HTMLElement>;
	/** Element that receives focus on close, by default the trigger or the element focused before. */
	finalFocus?: React.ComponentProps<typeof DialogContent>["finalFocus"];
	children: React.ReactNode;
}) {
	if (useIsDrawer()) {
		return (
			<DrawerPrimitive.VirtualKeyboardProvider>
				<DrawerContent finalFocus={finalFocus}>
					<div
						className="flex min-h-0 flex-col gap-4 overflow-y-auto p-4 pb-[calc(1rem+env(safe-area-inset-bottom,0px))]"
						onPaste={onPaste}
					>
						{children}
					</div>
				</DrawerContent>
			</DrawerPrimitive.VirtualKeyboardProvider>
		);
	}

	return (
		<DialogContent
			className={className}
			showCloseButton={showCloseButton}
			onPaste={onPaste}
			finalFocus={finalFocus}
		>
			{children}
		</DialogContent>
	);
}

function ResponsiveDialogTitle(props: React.ComponentProps<typeof DialogTitle>) {
	return useIsDrawer() ? <DrawerTitle {...props} /> : <DialogTitle {...props} />;
}

function ResponsiveDialogDescription(props: React.ComponentProps<typeof DialogDescription>) {
	return useIsDrawer() ? <DrawerDescription {...props} /> : <DialogDescription {...props} />;
}

export {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	DialogFooter as ResponsiveDialogFooter,
	DialogHeader as ResponsiveDialogHeader,
	ResponsiveDialogTitle,
	ResponsiveDialogTrigger,
};
