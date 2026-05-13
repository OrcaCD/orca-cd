import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

type DiscordBuilderFieldBinding = {
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

export function DiscordWebhookUrlField({ field }: { field: DiscordBuilderFieldBinding }) {
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
				placeholder={m.notificationWebhookUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function DiscordBotNameField({ field }: { field: DiscordBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.notificationBotName()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationBotNamePlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function DiscordAvatarUrlField({ field }: { field: DiscordBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.notificationAvatarUrl()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				type="url"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationAvatarUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function DiscordThreadIdField({ field }: { field: DiscordBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.notificationThreadId()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				inputMode="numeric"
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationThreadIdPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type DiscordNotificationConfig = {
	token: string;
	webhookId: string;
	threadId?: string;
	username?: string;
	avatarUrl?: string;
};

export type DiscordWebhookUrlParts = {
	token: string;
	webhookId: string;
	threadId?: string;
};

export type DiscordNotificationBuilderValues = {
	discordWebhookUrl: string;
	discordBotName: string;
	discordAvatarUrl: string;
	discordThreadId: string;
};

const emptyDiscordNotificationBuilderValues: DiscordNotificationBuilderValues = {
	discordWebhookUrl: "",
	discordBotName: "",
	discordAvatarUrl: "",
	discordThreadId: "",
};

const discordHostPattern = /(^|\.)discord(?:app)?\.com$/i;

function normalizeString(value: unknown): string {
	return typeof value === "string" ? value.trim() : "";
}

function toDiscordWebhookUrl(webhookId: string, token: string): string {
	return `https://discord.com/api/webhooks/${webhookId}/${token}`;
}

export function parseDiscordWebhookUrl(rawWebhookUrl: string): DiscordWebhookUrlParts | null {
	let parsedUrl: URL;
	try {
		parsedUrl = new URL(rawWebhookUrl.trim());
	} catch {
		return null;
	}

	if (!discordHostPattern.test(parsedUrl.hostname)) {
		return null;
	}

	const pathParts = parsedUrl.pathname.split("/").filter(Boolean);
	if (
		pathParts.length < 4 ||
		pathParts[0]?.toLowerCase() !== "api" ||
		pathParts[1]?.toLowerCase() !== "webhooks"
	) {
		return null;
	}

	const webhookId = pathParts[2]?.trim() ?? "";
	const token = pathParts[3]?.trim() ?? "";
	if (webhookId === "" || token === "") {
		return null;
	}

	const threadId = parsedUrl.searchParams.get("thread_id")?.trim() ?? "";

	return {
		token,
		webhookId,
		threadId: threadId === "" ? undefined : threadId,
	};
}

function parseDiscordShoutrrrUrl(rawUrl: string): DiscordNotificationBuilderValues | null {
	let parsedUrl: URL;
	try {
		parsedUrl = new URL(rawUrl.trim());
	} catch {
		return null;
	}

	if (parsedUrl.protocol !== "discord:") {
		return null;
	}

	const token = parsedUrl.username.trim();
	const webhookId = parsedUrl.host.trim();
	if (token === "" || webhookId === "") {
		return null;
	}

	return {
		discordWebhookUrl: toDiscordWebhookUrl(webhookId, token),
		discordBotName: parsedUrl.searchParams.get("username")?.trim() ?? "",
		discordAvatarUrl:
			parsedUrl.searchParams.get("avatarurl")?.trim() ??
			parsedUrl.searchParams.get("avatarUrl")?.trim() ??
			"",
		discordThreadId: parsedUrl.searchParams.get("thread_id")?.trim() ?? "",
	};
}

function parseDiscordWebhookBuilderValues(
	rawWebhookUrl: string,
): DiscordNotificationBuilderValues | null {
	const parsedWebhook = parseDiscordWebhookUrl(rawWebhookUrl);
	if (!parsedWebhook) {
		return null;
	}

	return {
		...emptyDiscordNotificationBuilderValues,
		discordWebhookUrl: toDiscordWebhookUrl(parsedWebhook.webhookId, parsedWebhook.token),
		discordThreadId: parsedWebhook.threadId ?? "",
	};
}

export function parseDiscordBuilderValues(
	rawConfig: string | undefined,
): DiscordNotificationBuilderValues {
	if (!rawConfig) {
		return { ...emptyDiscordNotificationBuilderValues };
	}

	const trimmedConfig = rawConfig.trim();
	if (trimmedConfig === "") {
		return { ...emptyDiscordNotificationBuilderValues };
	}

	if (trimmedConfig.startsWith("{")) {
		try {
			const parsedConfig = JSON.parse(trimmedConfig) as Record<string, unknown>;
			const botName = normalizeString(parsedConfig.username);
			const avatarUrl = normalizeString(parsedConfig.avatarUrl);
			const threadId = normalizeString(parsedConfig.threadId);

			const token = normalizeString(parsedConfig.token);
			const webhookId = normalizeString(parsedConfig.webhookId);
			if (token !== "" && webhookId !== "") {
				return {
					discordWebhookUrl: toDiscordWebhookUrl(webhookId, token),
					discordBotName: botName,
					discordAvatarUrl: avatarUrl,
					discordThreadId: threadId,
				};
			}

			const configUrls: string[] = [];
			const directUrl = normalizeString(parsedConfig.url);
			if (directUrl !== "") {
				configUrls.push(directUrl);
			}
			if (Array.isArray(parsedConfig.urls)) {
				for (const candidate of parsedConfig.urls) {
					const normalizedCandidate = normalizeString(candidate);
					if (normalizedCandidate !== "") {
						configUrls.push(normalizedCandidate);
					}
				}
			}

			for (const configUrl of configUrls) {
				const parsedBuilderValues =
					parseDiscordShoutrrrUrl(configUrl) ?? parseDiscordWebhookBuilderValues(configUrl);
				if (parsedBuilderValues) {
					return {
						...parsedBuilderValues,
						discordBotName: botName === "" ? parsedBuilderValues.discordBotName : botName,
						discordAvatarUrl: avatarUrl === "" ? parsedBuilderValues.discordAvatarUrl : avatarUrl,
						discordThreadId: threadId === "" ? parsedBuilderValues.discordThreadId : threadId,
					};
				}
			}

			return {
				...emptyDiscordNotificationBuilderValues,
				discordBotName: botName,
				discordAvatarUrl: avatarUrl,
				discordThreadId: threadId,
			};
		} catch {
			return { ...emptyDiscordNotificationBuilderValues };
		}
	}

	return (
		parseDiscordShoutrrrUrl(trimmedConfig) ??
		parseDiscordWebhookBuilderValues(trimmedConfig) ?? {
			...emptyDiscordNotificationBuilderValues,
		}
	);
}

export function buildDiscordNotificationConfig(
	values: DiscordNotificationBuilderValues,
): string | null {
	const parsedWebhook = parseDiscordWebhookUrl(values.discordWebhookUrl);
	if (!parsedWebhook) {
		return null;
	}

	const config: DiscordNotificationConfig = {
		token: parsedWebhook.token,
		webhookId: parsedWebhook.webhookId,
	};

	const botName = normalizeString(values.discordBotName);
	if (botName !== "") {
		config.username = botName;
	}

	const avatarUrl = normalizeString(values.discordAvatarUrl);
	if (avatarUrl !== "") {
		config.avatarUrl = avatarUrl;
	}

	const threadId = normalizeString(values.discordThreadId) || parsedWebhook.threadId || "";
	if (threadId !== "") {
		config.threadId = threadId;
	}

	return JSON.stringify(config);
}
