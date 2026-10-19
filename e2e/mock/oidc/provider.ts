import type { IncomingMessage, ServerResponse } from "node:http";
import Provider from "oidc-provider";
import { OIDC_MOCK_API_PORT } from "../../tests/const.ts";
import { findTamper, getUser, listUsers, MockAdapter, recordAuthRequest } from "./store.ts";

// The hub reaches the mock through the Docker host gateway and the browser resolves the same
// host name to loopback (see launchOptions in playwright.config.ts), so the issuer works for both
export const OIDC_MOCK_ISSUER = `http://host.docker.internal:${OIDC_MOCK_API_PORT}`;

const provider = new Provider(OIDC_MOCK_ISSUER, {
	adapter: MockAdapter,
	cookies: { keys: ["e2e-oidc-mock-cookie-key"] },
	// The hub reads the profile from the id_token, which is not the default for the code flow
	conformIdTokenClaims: false,
	scopes: ["openid", "email", "profile", "groups"],
	claims: {
		openid: ["sub"],
		email: ["email", "email_verified"],
		profile: ["name", "picture"],
		groups: ["groups"],
	},
	pkce: { required: () => true },
	features: { devInteractions: { enabled: false } },
	interactions: {
		url: (_ctx, interaction) => `/interaction/${interaction.uid}`,
	},
	findAccount: (_ctx, sub) => {
		const user = getUser(sub);
		if (!user) {
			return undefined;
		}
		return { accountId: sub, claims: () => ({ sub, ...user }) };
	},
});

function escapeHtml(value: string) {
	return value.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`);
}

async function readForm(req: IncomingMessage) {
	const chunks: Buffer[] = [];
	for await (const chunk of req) {
		chunks.push(chunk as Buffer);
	}
	return new URLSearchParams(Buffer.concat(chunks).toString("utf8"));
}

function loginPage(res: ServerResponse, uid: string) {
	const buttons = listUsers()
		.map(
			(sub) =>
				`<button type="submit" name="sub" value="${escapeHtml(sub)}">Sign in as ${escapeHtml(sub)}</button>`,
		)
		.join("\n");
	res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }).end(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Mock Identity Provider</title></head>
<body>
<h1>Mock Identity Provider</h1>
<form method="post" action="/interaction/${escapeHtml(uid)}">
${buttons}
<button type="submit" name="deny" value="1">Deny access</button>
</form>
</body>
</html>`);
}

async function handleInteraction(req: IncomingMessage, res: ServerResponse, uid: string) {
	const details = await provider.interactionDetails(req, res);
	if (req.method !== "POST") {
		return loginPage(res, uid);
	}

	const form = await readForm(req);
	const sub = form.get("sub");
	if (form.has("deny") || !sub) {
		return provider.interactionFinished(
			req,
			res,
			{ error: "access_denied", error_description: "The user denied the request" },
			{ mergeWithLastSubmission: false },
		);
	}

	const grant = new provider.Grant({
		accountId: sub,
		clientId: String(details.params.client_id),
	});
	grant.addOIDCScope(String(details.params.scope));
	const grantId = await grant.save();
	return provider.interactionFinished(
		req,
		res,
		{ login: { accountId: sub }, consent: { grantId } },
		{ mergeWithLastSubmission: false },
	);
}

const handleProvider = provider.callback();

function tamperLocation(location: string) {
	const url = new URL(location);
	const tamper = findTamper(url);
	if (!tamper) {
		return location;
	}
	if (tamper.state !== undefined) {
		url.searchParams.set("state", tamper.state);
	}
	if (tamper.callbackPath !== undefined) {
		url.pathname = tamper.callbackPath;
	}
	return url.toString();
}

function tamperRedirect(res: ServerResponse) {
	const setHeader = res.setHeader.bind(res);
	res.setHeader = (name, value) =>
		setHeader(name, name.toLowerCase() === "location" ? tamperLocation(String(value)) : value);
}

export function handleOidcApi(req: IncomingMessage, res: ServerResponse) {
	const { pathname, searchParams } = new URL(req.url ?? "/", OIDC_MOCK_ISSUER);

	if (pathname === "/auth") {
		recordAuthRequest(Object.fromEntries(searchParams));
	}

	const interaction = pathname.match(/^\/interaction\/([\w-]+)$/);
	if (interaction) {
		return handleInteraction(req, res, interaction[1]);
	}
	if (/^\/auth\/[\w-]+$/.test(pathname)) {
		tamperRedirect(res);
	}
	return handleProvider(req, res);
}
