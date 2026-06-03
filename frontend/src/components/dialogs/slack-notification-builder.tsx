import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

type SlackBuilderFieldBinding = {
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

export function SlackWebhookUrlField({ field }: { field: SlackBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.webhookUrl()}</Label>
			<Input
				id={field.name}
				type="url"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationSlackWebhookUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type SlackNotificationConfig = {
	webhookUrl: string;
};

export type SlackNotificationBuilderValues = {
	slackWebhookUrl: string;
};

const emptySlackNotificationBuilderValues: SlackNotificationBuilderValues = {
	slackWebhookUrl: "",
};

export function isValidSlackWebhookUrl(rawUrl: string): boolean {
	let parsed: URL;
	try {
		parsed = new URL(rawUrl.trim());
	} catch {
		return false;
	}

	if (parsed.protocol !== "https:") {
		return false;
	}

	if (parsed.hostname.toLowerCase() !== "hooks.slack.com") {
		return false;
	}

	if (!parsed.pathname.startsWith("/services/")) {
		return false;
	}

	const parts = parsed.pathname
		.slice("/services/".length)
		.replace(/\/$/, "")
		.split("/")
		.filter(Boolean);
	return parts.length === 3;
}

function normalizeString(value: unknown): string {
	return typeof value === "string" ? value.trim() : "";
}

export function parseSlackBuilderValues(
	rawConfig: string | undefined,
): SlackNotificationBuilderValues {
	if (!rawConfig) {
		return { ...emptySlackNotificationBuilderValues };
	}

	const trimmedConfig = rawConfig.trim();
	if (trimmedConfig === "") {
		return { ...emptySlackNotificationBuilderValues };
	}

	if (trimmedConfig.startsWith("{")) {
		try {
			const parsedConfig = JSON.parse(trimmedConfig) as Record<string, unknown>;
			return {
				slackWebhookUrl: normalizeString(parsedConfig.webhookUrl),
			};
		} catch {
			return { ...emptySlackNotificationBuilderValues };
		}
	}

	return { ...emptySlackNotificationBuilderValues };
}

export function buildSlackNotificationConfig(
	values: SlackNotificationBuilderValues,
): string | null {
	const webhookUrl = values.slackWebhookUrl.trim();
	if (!isValidSlackWebhookUrl(webhookUrl)) {
		return null;
	}

	return JSON.stringify({ webhookUrl } satisfies SlackNotificationConfig);
}
