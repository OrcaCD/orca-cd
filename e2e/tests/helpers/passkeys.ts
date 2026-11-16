import { expect, type CDPSession, type Page } from "@playwright/test";

export interface TestCredential {
	credentialId: string;
	isResidentCredential: boolean;
	rpId?: string;
	privateKey: string;
	userHandle?: string;
	signCount: number;
}

const authenticators = new WeakMap<Page, { client: CDPSession; authenticatorId: string }>();

export async function installAuthenticator(page: Page) {
	const existing = authenticators.get(page);
	if (existing) {
		return existing;
	}
	const client = await page.context().newCDPSession(page);
	await client.send("WebAuthn.enable");
	const { authenticatorId } = await client.send("WebAuthn.addVirtualAuthenticator", {
		options: {
			protocol: "ctap2",
			transport: "internal",
			hasResidentKey: true,
			hasUserVerification: true,
			isUserVerified: true,
			automaticPresenceSimulation: true,
		},
	});
	const authenticator = { client, authenticatorId: authenticatorId as string };
	authenticators.set(page, authenticator);
	return authenticator;
}

export async function credentials(page: Page): Promise<TestCredential[]> {
	const { client, authenticatorId } = await installAuthenticator(page);
	return (await client.send("WebAuthn.getCredentials", { authenticatorId })).credentials;
}

export async function importCredential(page: Page, credential: TestCredential) {
	const { client, authenticatorId } = await installAuthenticator(page);
	await client.send("WebAuthn.addCredential", { authenticatorId, credential });
}

export async function enroll(page: Page, enrollmentToken: string) {
	await installAuthenticator(page);
	await page.goto("/register");
	await page.getByLabel("Enrollment code", { exact: true }).fill(enrollmentToken);
	await page.getByRole("button", { name: "Create passkey", exact: true }).click();
	await page.waitForURL("/");
	const [credential] = await credentials(page);
	expect(credential).toBeDefined();
	return credential;
}

export async function replaceAuthenticator(page: Page) {
	const { client, authenticatorId } = await installAuthenticator(page);
	await client.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
	authenticators.delete(page);
	return installAuthenticator(page);
}
