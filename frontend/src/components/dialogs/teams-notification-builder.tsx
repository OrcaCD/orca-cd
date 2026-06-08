import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

type TeamsBuilderFieldBinding = {
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

export function TeamsHostField({ field }: { field: TeamsBuilderFieldBinding }) {
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
				placeholder={m.notificationTeamsHostPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function TeamsTitleField({ field }: { field: TeamsBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.title()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationTeamsTitlePlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type TeamsNotificationConfig = {
	host: string;
	title?: string;
};

export type TeamsNotificationBuilderValues = {
	teamsHost: string;
	teamsTitle: string;
};

export function isValidTeamsHost(rawUrl: string): boolean {
	let parsed: URL;
	try {
		parsed = new URL(rawUrl.trim());
	} catch {
		return false;
	}

	if (parsed.protocol !== "https:") {
		return false;
	}

	if (!/^[a-z0-9.-]+\.webhook\.office\.com$/i.test(parsed.hostname)) {
		return false;
	}

	const pattern =
		/^\/webhookb2\/[0-9a-f-]{36}@[0-9a-f-]{36}\/IncomingWebhook\/[0-9a-f]{32}\/[0-9a-f-]{36}\/[A-Za-z0-9_-]+\/?$/i;
	return pattern.test(parsed.pathname);
}

export function buildTeamsNotificationConfig(
	values: TeamsNotificationBuilderValues,
): string | null {
	const host = values.teamsHost.trim();
	if (!isValidTeamsHost(host)) {
		return null;
	}

	const config: TeamsNotificationConfig = { host };
	const title = values.teamsTitle.trim();
	if (title !== "") {
		config.title = title;
	}

	return JSON.stringify(config);
}
