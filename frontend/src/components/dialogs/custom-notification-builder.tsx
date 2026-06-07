import { ExternalLink } from "lucide-react";
import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

const shoutrrrDocsUrl = "https://shoutrrr.nickfedor.com/services/overview/";

type CustomBuilderFieldBinding = {
	name: string;
	state: {
		value: string;
		meta: {
			isTouched: boolean;
			isValid: boolean;
			errors: unknown[];
		};
	};
	handleBlur: () => void;
	handleChange: (value: string) => void;
};

type FieldErrorList = Array<{ message?: string } | undefined>;

export function CustomShoutrrrUrlField({ field }: { field: CustomBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<div className="flex items-center justify-between gap-3">
				<Label htmlFor={field.name}>{m.shoutrrrUrl()}</Label>
				<a
					href={shoutrrrDocsUrl}
					target="_blank"
					rel="noopener noreferrer"
					className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
				>
					{m.shoutrrrDocs()}
					<ExternalLink className="h-3 w-3" />
				</a>
			</div>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationCustomShoutrrrUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type CustomNotificationBuilderValues = {
	customShoutrrrUrl: string;
};

export function isValidShoutrrrUrl(rawUrl: string): boolean {
	const trimmed = rawUrl.trim();
	if (!trimmed.includes("://")) {
		return false;
	}
	let parsedUrl: URL;
	try {
		parsedUrl = new URL(trimmed);
	} catch {
		return false;
	}

	// Shoutrrr uses service schemes (e.g. discord://, slack://, gotify://), not http(s).
	return parsedUrl.protocol !== "http:" && parsedUrl.protocol !== "https:";
}

export function buildCustomNotificationConfig(
	values: CustomNotificationBuilderValues,
): string | null {
	const shoutrrrUrl = values.customShoutrrrUrl.trim();
	if (!isValidShoutrrrUrl(shoutrrrUrl)) {
		return null;
	}

	return shoutrrrUrl;
}
