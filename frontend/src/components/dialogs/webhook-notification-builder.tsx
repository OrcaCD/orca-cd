import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { m } from "@/lib/paraglide/messages";

type WebhookBuilderFieldBinding = {
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

export const webhookMethods = ["POST", "PUT", "PATCH", "DELETE", "GET"] as const;
export type WebhookMethod = (typeof webhookMethods)[number];

export function WebhookUrlField({ field }: { field: WebhookBuilderFieldBinding }) {
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
				placeholder={m.notificationGenericWebhookUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function WebhookMethodField({ field }: { field: WebhookBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.httpMethod()}</Label>
			<Select value={field.state.value} onValueChange={field.handleChange}>
				<SelectTrigger id={field.name} className="w-full">
					<SelectValue placeholder={m.selectMethod()} />
				</SelectTrigger>
				<SelectContent>
					{webhookMethods.map((method) => (
						<SelectItem key={method} value={method}>
							{method}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function WebhookHeadersField({ field }: { field: WebhookBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.headers()} {m.optional()}
			</Label>
			<Textarea
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationWebhookHeadersPlaceholder()}
				className="min-h-24 font-mono text-sm"
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type WebhookNotificationConfig = {
	webhookUrl: string;
	method: WebhookMethod;
	headers?: Record<string, string>;
};

export type WebhookNotificationBuilderValues = {
	webhookUrl: string;
	webhookMethod: WebhookMethod;
	webhookHeaders: string;
};

const headerNamePattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

export function parseWebhookHeaders(rawHeaders: string): Record<string, string> | null {
	const headers: Record<string, string> = {};

	for (const line of rawHeaders.split(/\r?\n/)) {
		const trimmedLine = line.trim();
		if (trimmedLine === "") {
			continue;
		}

		const separatorIndex = trimmedLine.indexOf(":");
		if (separatorIndex <= 0) {
			return null;
		}

		const name = trimmedLine.slice(0, separatorIndex).trim();
		if (!headerNamePattern.test(name)) {
			return null;
		}

		headers[name] = trimmedLine.slice(separatorIndex + 1).trim();
	}

	return headers;
}

export function isWebhookMethod(value: string): value is WebhookMethod {
	return webhookMethods.includes(value as WebhookMethod);
}

export function buildWebhookNotificationConfig(
	values: WebhookNotificationBuilderValues,
): string | null {
	const webhookUrl = values.webhookUrl.trim();
	const method = values.webhookMethod;
	const headers = parseWebhookHeaders(values.webhookHeaders);
	if (!headers || !isWebhookMethod(method)) {
		return null;
	}

	const config: WebhookNotificationConfig = { webhookUrl, method };
	if (Object.keys(headers).length > 0) {
		config.headers = headers;
	}

	return JSON.stringify(config);
}
