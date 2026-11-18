import {
	browserSupportsWebAuthn,
	startAuthentication,
	startRegistration,
	WebAuthnError,
	type PublicKeyCredentialCreationOptionsJSON,
	type PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import { fetcher } from "./api";
import { m } from "@/lib/paraglide/messages";

export interface Passkey {
	id: string;
	name: string;
	createdAt: string;
	lastUsedAt: string | null;
}

export function passkeysSupported(): boolean {
	return window.isSecureContext && browserSupportsWebAuthn();
}

export function passkeyErrorMessage(error: unknown): string {
	if (error instanceof WebAuthnError) {
		if (error.code === "ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED") {
			return m.passkeyAlreadyRegistered();
		}
		if (error.name === "NotAllowedError" || error.name === "AbortError") {
			return m.passkeyCancelled();
		}
	}
	return error instanceof Error ? error.message : m.passkeyFailed();
}

function requireSupport() {
	if (!passkeysSupported()) {
		throw new Error(m.passkeysUnsupported());
	}
}

export async function loginWithPasskey(): Promise<void> {
	requireSupport();
	const optionsJSON = await fetcher<PublicKeyCredentialRequestOptionsJSON>(
		"/auth/passkeys/login/begin",
		"POST",
	);
	const response = await startAuthentication({ optionsJSON });
	await fetcher("/auth/passkeys/login/finish", "POST", response);
}

export async function registerPasskey(data: {
	name?: string;
	email?: string;
	setupCode?: string;
	enrollmentToken?: string;
}): Promise<void> {
	requireSupport();
	const optionsJSON = await fetcher<PublicKeyCredentialCreationOptionsJSON>(
		"/auth/passkeys/register/begin",
		"POST",
		data,
	);
	const response = await startRegistration({ optionsJSON });
	await fetcher("/auth/passkeys/register/finish", "POST", response);
}

export async function addPasskey(name: string): Promise<void> {
	const verificationToken = await verifyPasskey();
	const optionsJSON = await fetcher<PublicKeyCredentialCreationOptionsJSON>(
		"/auth/passkeys/begin",
		"POST",
		{ name, verificationToken },
	);
	const response = await startRegistration({ optionsJSON });
	await fetcher("/auth/passkeys/finish", "POST", response);
}

export async function deletePasskey(id: string): Promise<void> {
	const verificationToken = await verifyPasskey();
	await fetcher(`/auth/passkeys/${id}`, "DELETE", { verificationToken });
}

async function verifyPasskey(): Promise<string> {
	requireSupport();
	const optionsJSON = await fetcher<PublicKeyCredentialRequestOptionsJSON>(
		"/auth/passkeys/verify/begin",
		"POST",
	);
	const response = await startAuthentication({ optionsJSON });
	const result = await fetcher<{ verificationToken: string }>(
		"/auth/passkeys/verify/finish",
		"POST",
		response,
	);
	return result.verificationToken;
}
