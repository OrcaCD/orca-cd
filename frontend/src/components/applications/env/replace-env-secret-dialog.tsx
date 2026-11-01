import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { envValueErrorMessage, validateEnvValue } from "@/lib/application-env";
import { m } from "@/lib/paraglide/messages";

export function ReplaceEnvSecretDialog({
	envKey,
	disabled,
	onReplace,
}: {
	envKey: string;
	disabled: boolean;
	onReplace: (value: string) => void;
}) {
	const [open, setOpen] = useState(false);
	const [value, setValue] = useState("");
	const [submitted, setSubmitted] = useState(false);

	const valueError = validateEnvValue(value);
	const valueMessage = valueError
		? envValueErrorMessage(valueError)
		: submitted && value === ""
			? m.envErrorValueEmpty()
			: null;

	function handleOpenChange(nextOpen: boolean) {
		setOpen(nextOpen);
		if (!nextOpen) {
			setValue("");
			setSubmitted(false);
		}
	}

	function handleSubmit(e: React.SyntheticEvent) {
		e.preventDefault();
		setSubmitted(true);
		if (valueError || value === "") {
			return;
		}
		onReplace(value);
		handleOpenChange(false);
	}

	return (
		<>
			<Button variant="ghost" size="sm" disabled={disabled} onClick={() => setOpen(true)}>
				<RefreshCw />
				{m.envReplace()}
			</Button>
			<Dialog open={open} onOpenChange={handleOpenChange}>
				<DialogContent className="sm:max-w-lg">
					<form onSubmit={handleSubmit} className="flex flex-col gap-4">
						<DialogHeader>
							<DialogTitle>{m.envReplaceTitle()}</DialogTitle>
							<DialogDescription>{m.envReplaceDescription({ key: envKey })}</DialogDescription>
						</DialogHeader>

						<Field data-invalid={!!valueMessage}>
							<FieldLabel htmlFor="env-replace-value">{m.envValueLabel()}</FieldLabel>
							<Textarea
								id="env-replace-value"
								className="min-h-9 resize-none font-mono"
								rows={1}
								value={value}
								onChange={(e) => setValue(e.target.value)}
								autoComplete="off"
								spellCheck={false}
								aria-invalid={!!valueMessage}
							/>
							{valueMessage && <FieldError>{valueMessage}</FieldError>}
						</Field>

						<DialogFooter>
							<Button type="button" variant="secondary" onClick={() => handleOpenChange(false)}>
								{m.cancel()}
							</Button>
							<Button type="submit">{m.envReplaceConfirm()}</Button>
						</DialogFooter>
					</form>
				</DialogContent>
			</Dialog>
		</>
	);
}
