import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldError,
	FieldLabel,
	FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
	envKeyErrorMessage,
	envValueErrorMessage,
	validateEnvKey,
	validateEnvValue,
} from "@/lib/application-env";
import { m } from "@/lib/paraglide/messages";

export function AddEnvEntryDialog({
	path,
	existingKeys,
	disabled,
	onAdd,
}: {
	path: string;
	existingKeys: ReadonlySet<string>;
	disabled: boolean;
	onAdd: (key: string, value: string, isSecret: boolean) => void;
}) {
	const [open, setOpen] = useState(false);
	const [key, setKey] = useState("");
	const [value, setValue] = useState("");
	const [isSecret, setIsSecret] = useState(false);
	const [submitted, setSubmitted] = useState(false);

	const keyError = validateEnvKey(key);
	const valueError = validateEnvValue(value);
	const keyMessage = keyError
		? envKeyErrorMessage(keyError)
		: existingKeys.has(key)
			? m.envErrorKeyExists()
			: null;
	const valueMessage = valueError
		? envValueErrorMessage(valueError)
		: submitted && value === ""
			? m.envErrorValueEmpty()
			: null;

	function handleOpenChange(nextOpen: boolean) {
		setOpen(nextOpen);
		if (!nextOpen) {
			setKey("");
			setValue("");
			setIsSecret(false);
			setSubmitted(false);
		}
	}

	function handleSubmit(e: React.SyntheticEvent) {
		e.preventDefault();
		setSubmitted(true);
		if (keyMessage || valueMessage || value === "") {
			return;
		}
		onAdd(key, value, isSecret);
		handleOpenChange(false);
	}

	return (
		<>
			<Button variant="outline" size="sm" disabled={disabled} onClick={() => setOpen(true)}>
				<Plus />
				{m.envAddVariable()}
			</Button>
			<Dialog open={open} onOpenChange={handleOpenChange}>
				<DialogContent className="sm:max-w-lg">
					<form onSubmit={handleSubmit} className="flex flex-col gap-4">
						<DialogHeader>
							<DialogTitle>{m.envAddTitle()}</DialogTitle>
							<DialogDescription>{m.envAddDescription({ path })}</DialogDescription>
						</DialogHeader>

						<Field data-invalid={submitted && !!keyMessage}>
							<FieldLabel htmlFor="env-add-key">{m.envKeyLabel()}</FieldLabel>
							<Input
								id="env-add-key"
								className="font-mono"
								value={key}
								onChange={(e) => setKey(e.target.value)}
								placeholder={m.envKeyPlaceholder()}
								autoComplete="off"
								autoCapitalize="off"
								spellCheck={false}
								aria-invalid={submitted && !!keyMessage}
							/>
							{submitted && keyMessage && <FieldError>{keyMessage}</FieldError>}
						</Field>

						<Field data-invalid={!!valueMessage}>
							<FieldLabel htmlFor="env-add-value">{m.envValueLabel()}</FieldLabel>
							<Textarea
								id="env-add-value"
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

						<FieldLabel htmlFor="env-add-secret">
							<Field orientation="horizontal">
								<FieldContent>
									<FieldTitle>{m.envSecretSwitchLabel()}</FieldTitle>
									<FieldDescription>{m.envSecretSwitchDescription()}</FieldDescription>
								</FieldContent>
								<Switch id="env-add-secret" checked={isSecret} onCheckedChange={setIsSecret} />
							</Field>
						</FieldLabel>

						<p className="text-xs text-muted-foreground">{m.envAddStorageNote()}</p>

						<DialogFooter>
							<Button type="button" variant="secondary" onClick={() => handleOpenChange(false)}>
								{m.cancel()}
							</Button>
							<Button type="submit">{m.envAddConfirm()}</Button>
						</DialogFooter>
					</form>
				</DialogContent>
			</Dialog>
		</>
	);
}
