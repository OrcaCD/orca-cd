import { AlertCircle } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useState } from "react";
import type { VariantProps } from "class-variance-authority";
import { m } from "@/lib/paraglide/messages";

interface ConfirmationDialogProps {
	triggerText?: string | React.ReactNode;
	title?: string;
	description?: string;
	confirmText?: string;
	cancelText?: string;
	onConfirm: () => void;
	triggerProps?: VariantProps<typeof buttonVariants>;
	asDropdownItem?: boolean;
}

export default function ConfirmationDialog({
	triggerText = m.confirmAction(),
	title = m.areYouSure(),
	description = m.confirmDialogDefaultDescription(),
	confirmText = m.confirmProceed(),
	cancelText = m.cancel(),
	onConfirm,
	triggerProps = { variant: "outline" },
	asDropdownItem = false,
}: ConfirmationDialogProps) {
	const [open, setOpen] = useState(false);

	const handleConfirm = () => {
		onConfirm();
		setOpen(false);
	};

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger asChild>
				{asDropdownItem ? (
					<DropdownMenuItem variant="destructive" onSelect={(e) => e.preventDefault()}>
						{triggerText}
					</DropdownMenuItem>
				) : (
					<Button {...triggerProps}>{triggerText}</Button>
				)}
			</DialogTrigger>
			<DialogContent className="sm:max-w-106.25">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<AlertCircle className="h-5 w-5 text-red-500" />
						{title}
					</DialogTitle>
					<DialogDescription className="py-2">{description}</DialogDescription>
				</DialogHeader>
				<DialogFooter className="flex-col space-y-2 sm:flex-row sm:justify-end sm:space-x-2 sm:space-y-0">
					<Button
						type="button"
						variant="secondary"
						onClick={() => setOpen(false)}
						className="w-full sm:w-auto"
					>
						{cancelText}
					</Button>
					<Button
						type="button"
						variant="destructive"
						onClick={handleConfirm}
						className="w-full sm:w-auto"
					>
						{confirmText}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
