import { useRef, useState, type MouseEvent } from "react";

/**
 * State of a dialog that more than one button opens, such as the create dialog of a list page that
 * is offered in the page header and in the empty state. The dialog is rendered once, outside of
 * its buttons, so it and whatever it shows after saving survive when the list refreshes and the
 * empty state disappears. Closing it returns focus to the button that opened it, or to the
 * `fallbackRef` button when that one is gone.
 */
export function useSharedDialog() {
	const [open, setOpen] = useState(false);
	const openerRef = useRef<HTMLElement | null>(null);
	const fallbackRef = useRef<HTMLButtonElement>(null);

	return {
		fallbackRef,
		triggerProps: {
			"aria-haspopup": "dialog" as const,
			"aria-expanded": open,
		},
		openFrom: (event: MouseEvent<HTMLElement>) => {
			openerRef.current = event.currentTarget;
			setOpen(true);
		},
		dialogProps: {
			open,
			onOpenChange: setOpen,
			finalFocus: () =>
				(openerRef.current?.isConnected ? openerRef.current : fallbackRef.current) ?? true,
		},
	};
}

export type SharedDialogProps = ReturnType<typeof useSharedDialog>["dialogProps"];
