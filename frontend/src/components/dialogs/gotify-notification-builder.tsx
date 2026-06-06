import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { m } from "@/lib/paraglide/messages";

type GotifyBuilderFieldBinding = {
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

export function GotifyServerUrlField({ field }: { field: GotifyBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.serverUrl()}</Label>
			<Input
				id={field.name}
				type="url"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationGotifyServerUrlPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function GotifyAppTokenField({ field }: { field: GotifyBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.appToken()}</Label>
			<Input
				id={field.name}
				type="password"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationGotifyAppTokenPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function GotifyPriorityField({ field }: { field: GotifyBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.priority()}</Label>
			<Input
				id={field.name}
				type="number"
				min={-2}
				max={10}
				step={1}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder="5"
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function GotifyCustomPathField({ field }: { field: GotifyBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.customPath()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationGotifyCustomPathPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export type GotifyNotificationConfig = {
	serverUrl: string;
	appToken: string;
	priority: number;
	customPath?: string;
};

export type GotifyNotificationBuilderValues = {
	gotifyServerUrl: string;
	gotifyAppToken: string;
	gotifyPriority: string;
	gotifyCustomPath: string;
};

const gotifyTokenPattern = /^A[-._A-Za-z0-9]{14}$/;

export function isValidGotifyAppToken(appToken: string): boolean {
	return gotifyTokenPattern.test(appToken.trim());
}

export function parseGotifyPriority(priority: string): number | null {
	const trimmedPriority = priority.trim();
	if (!/^-?\d+$/.test(trimmedPriority)) {
		return null;
	}

	const parsedPriority = Number.parseInt(trimmedPriority, 10);
	if (parsedPriority < -2 || parsedPriority > 10) {
		return null;
	}

	return parsedPriority;
}

export function buildGotifyNotificationConfig(
	values: GotifyNotificationBuilderValues,
): string | null {
	const serverUrl = values.gotifyServerUrl.trim();
	const appToken = values.gotifyAppToken.trim();
	const priority = parseGotifyPriority(values.gotifyPriority);
	if (serverUrl === "" || appToken === "" || priority === null) {
		return null;
	}

	const config: GotifyNotificationConfig = { serverUrl, appToken, priority };
	const customPath = values.gotifyCustomPath.trim();
	if (customPath !== "") {
		config.customPath = customPath;
	}

	return JSON.stringify(config);
}
