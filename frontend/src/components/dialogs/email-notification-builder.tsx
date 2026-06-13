import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { m } from "@/lib/paraglide/messages";

type EmailBuilderFieldBinding = {
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

type EmailBuilderBooleanFieldBinding = {
	state: {
		value: boolean;
	};
	handleChange: (value: boolean) => void;
};

type FieldErrorList = Array<{ message?: string } | undefined>;

export function EmailSMTPHostField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.smtpHost()}</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailSMTPHostPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailSMTPPortField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.smtpPort()}</Label>
			<Input
				id={field.name}
				type="number"
				min={1}
				max={65535}
				step={1}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder="587"
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailUsernameField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.smtpUsername()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailUsernamePlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailPasswordField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.password()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				type="password"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailPasswordPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailFromAddressField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.fromAddress()}</Label>
			<Input
				id={field.name}
				type="email"
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailFromAddressPlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailFromNameField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>
				{m.fromName()} {m.optional()}
			</Label>
			<Input
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailFromNamePlaceholder()}
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailToAddressesField({ field }: { field: EmailBuilderFieldBinding }) {
	const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;

	return (
		<Field data-invalid={isInvalid}>
			<Label htmlFor={field.name}>{m.toAddresses()}</Label>
			<Textarea
				id={field.name}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				placeholder={m.notificationEmailToAddressesPlaceholder()}
				className="min-h-20"
			/>
			{isInvalid && <FieldError errors={field.state.meta.errors as FieldErrorList} />}
		</Field>
	);
}

export function EmailUseTLSField({ field }: { field: EmailBuilderBooleanFieldBinding }) {
	return (
		<div className="flex items-center justify-between rounded-md border p-3 gap-4">
			<div>
				<p className="text-sm font-medium">{m.useTls()}</p>
				<p className="text-xs text-muted-foreground">{m.notificationEmailUseTLSDescription()}</p>
			</div>
			<Switch checked={field.state.value} onCheckedChange={field.handleChange} />
		</div>
	);
}

export type EmailNotificationConfig = {
	smtpHost: string;
	smtpPort: number;
	username?: string;
	password?: string;
	fromAddress: string;
	fromName?: string;
	toAddresses: string[];
	useTls: boolean;
};

export type EmailNotificationBuilderValues = {
	emailSMTPHost: string;
	emailSMTPPort: string;
	emailUsername: string;
	emailPassword: string;
	emailFromAddress: string;
	emailFromName: string;
	emailToAddresses: string;
	emailUseTLS: boolean;
};

const emailPattern = /^[^\s@]+@(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$/;

export function isValidEmailAddress(value: string): boolean {
	const emailAddress = value.trim();
	const [localPart, domain] = emailAddress.split("@");

	return (
		emailAddress.length <= 254 &&
		localPart !== undefined &&
		localPart.length > 0 &&
		localPart.length <= 64 &&
		domain !== undefined &&
		domain.length <= 253 &&
		emailPattern.test(emailAddress)
	);
}

export function isValidSMTPHost(value: string): boolean {
	const smtpHost = value.trim();
	if (smtpHost === "") {
		return false;
	}

	try {
		const parsedUrl = new URL(`smtp://${smtpHost}`);
		return (
			parsedUrl.hostname !== "" &&
			parsedUrl.username === "" &&
			parsedUrl.password === "" &&
			parsedUrl.port === "" &&
			parsedUrl.pathname === "" &&
			parsedUrl.search === "" &&
			parsedUrl.hash === ""
		);
	} catch {
		return false;
	}
}

export function parseSMTPPort(value: string): number | null {
	const trimmedValue = value.trim();
	if (!/^\d+$/.test(trimmedValue)) {
		return null;
	}

	const parsedPort = Number.parseInt(trimmedValue, 10);
	if (parsedPort < 1 || parsedPort > 65535) {
		return null;
	}

	return parsedPort;
}

export function parseEmailToAddresses(value: string): string[] | null {
	const addresses = value
		.split(/\r?\n|,/)
		.map((address) => address.trim())
		.filter((address) => address !== "");

	if (addresses.length === 0 || addresses.some((address) => !isValidEmailAddress(address))) {
		return null;
	}

	return addresses;
}

export function buildEmailNotificationConfig(
	values: EmailNotificationBuilderValues,
): string | null {
	const smtpHost = values.emailSMTPHost.trim();
	const smtpPort = parseSMTPPort(values.emailSMTPPort);
	const fromAddress = values.emailFromAddress.trim();
	const toAddresses = parseEmailToAddresses(values.emailToAddresses);
	if (
		smtpHost === "" ||
		!isValidSMTPHost(smtpHost) ||
		smtpPort === null ||
		!isValidEmailAddress(fromAddress) ||
		toAddresses === null
	) {
		return null;
	}

	const config: EmailNotificationConfig = {
		smtpHost,
		smtpPort,
		fromAddress,
		toAddresses,
		useTls: values.emailUseTLS,
	};

	const username = values.emailUsername.trim();
	if (username !== "") {
		config.username = username;
	}

	const password = values.emailPassword;
	if (password !== "") {
		config.password = password;
	}

	const fromName = values.emailFromName.trim();
	if (fromName !== "") {
		config.fromName = fromName;
	}

	return JSON.stringify(config);
}
