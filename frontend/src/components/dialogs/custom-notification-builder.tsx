import { ExternalLink } from "lucide-react";
import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

const shouterrrDocsUrl = "https://shoutrrr.nickfedor.com/services/overview/";

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

export function CustomShouterrrUrlField({ field }: { field: CustomBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<div className="flex items-center justify-between gap-3">
				<Label htmlFor={field.name}>{m.shouterrrUrl()}</Label>
				<a
					href={shouterrrDocsUrl}
					target="_blank"
					rel="noreferrer"
					className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
				>
					{m.shouterrrDocs()}
					<ExternalLink className="h-3 w-3" />
				</a>
			</div>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationCustomShouterrrUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type CustomNotificationBuilderValues = {
	customShouterrrUrl: string;
};

export function isValidShouterrrUrl(rawUrl: string): boolean {
	try {
		const parsedUrl = new URL(rawUrl);
		return parsedUrl.protocol.length > 1;
	} catch {
		return false;
	}
}

export function buildCustomNotificationConfig(
	values: CustomNotificationBuilderValues,
): string | null {
	const shouterrrUrl = values.customShouterrrUrl.trim();
	if (!isValidShouterrrUrl(shouterrrUrl)) {
		return null;
	}

	return shouterrrUrl;
}
