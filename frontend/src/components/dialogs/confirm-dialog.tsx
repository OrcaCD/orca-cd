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
import { useState } from "react";
import type { VariantProps } from "class-variance-authority";

interface ConfirmationDialogProps {
	triggerText?: string | React.ReactNode;
	title?: string;
	description?: string;
	confirmText?: string;
	cancelText?: string;
	onConfirm: () => void;
	triggerProps?: VariantProps<typeof buttonVariants>;
}

export default function ConfirmationDialog({
	triggerText = "Confirm action",
	title = "Are you sure?",
	description = "Do you really want to perform this action? This action cannot be undone.",
	confirmText = "Yes, proceed",
	cancelText = "Cancel",
	onConfirm,
	triggerProps = { variant: "outline" },
}: ConfirmationDialogProps) {
	const [open, setOpen] = useState(false);

	const handleConfirm = () => {
		onConfirm();
		setOpen(false);
	};

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger asChild>
				<Button {...triggerProps}>{triggerText}</Button>
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
