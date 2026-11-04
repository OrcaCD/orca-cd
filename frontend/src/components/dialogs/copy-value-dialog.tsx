import { Button } from "@/components/ui/button";
import {
	ResponsiveDialog,
	ResponsiveDialogContent,
	ResponsiveDialogDescription,
	ResponsiveDialogFooter,
	ResponsiveDialogHeader,
	ResponsiveDialogTitle,
} from "@/components/responsive-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import CopyButton from "../copy-btn";
import { m } from "@/lib/paraglide/messages";

interface CopyValueDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	title: string;
	description: string;
	label: string;
	value: string;
	inputId?: string;
	copyTitle?: string;
	doneLabel?: string;
}

export default function CopyValueDialog({
	open,
	onOpenChange,
	title,
	description,
	label,
	value,
	inputId = "copy-value",
	copyTitle = m.copyValue(),
	doneLabel = m.done(),
}: CopyValueDialogProps) {
	return (
		<ResponsiveDialog open={open} onOpenChange={onOpenChange}>
			<ResponsiveDialogContent className="sm:max-w-md">
				<ResponsiveDialogHeader>
					<ResponsiveDialogTitle>{title}</ResponsiveDialogTitle>
					<ResponsiveDialogDescription>{description}</ResponsiveDialogDescription>
				</ResponsiveDialogHeader>

				<div className="space-y-2">
					<Label htmlFor={inputId}>{label}</Label>
					<Input id={inputId} value={value} readOnly />
				</div>

				<ResponsiveDialogFooter>
					<CopyButton text={value} title={copyTitle} />
					<Button type="button" onClick={() => onOpenChange(false)}>
						{doneLabel}
					</Button>
				</ResponsiveDialogFooter>
			</ResponsiveDialogContent>
		</ResponsiveDialog>
	);
}
