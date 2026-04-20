import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
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
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription>{description}</DialogDescription>
				</DialogHeader>

				<div className="space-y-2">
					<Label htmlFor={inputId}>{label}</Label>
					<Input id={inputId} value={value} readOnly />
				</div>

				<DialogFooter>
					<CopyButton text={value} title={copyTitle} />
					<Button type="button" onClick={() => onOpenChange(false)}>
						{doneLabel}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
