import type { Adapter, AdapterPayload } from "oidc-provider";

export interface MockUser {
	email?: string;
	email_verified?: boolean;
	name?: string;
	picture?: string;
	groups?: string[];
}

/** Where the profile claims of a user are served: the id_token or only the userinfo endpoint */
export type ProfileSource = "id_token" | "userinfo";

export interface MockClient {
	secret: string;
	redirectUris: string[];
	profileSource?: ProfileSource;
}

export type AuthRequest = Record<string, string>;

/** Changes the redirect back to the client, to test how the client handles a broken callback */
export interface Tamper {
	state?: string;
	callbackPath?: string;
}

const users = new Map<string, MockUser>();
const clients = new Map<string, MockClient>();
const authRequests = new Map<string, AuthRequest[]>();
const tampering = new Map<string, Tamper>();

export function listUsers() {
	return [...users.keys()];
}

export function getUser(sub: string) {
	return users.get(sub);
}

/** Replaces the user, so claims omitted in the new definition are gone */
export function setUser(sub: string, user: MockUser) {
	users.set(sub, user);
}

export function getClient(clientId: string) {
	return clients.get(clientId);
}

export function setClient(clientId: string, client: MockClient) {
	clients.set(clientId, client);
	authRequests.set(clientId, []);
	tampering.delete(clientId);
}

export function setTamper(clientId: string, tamper: Tamper) {
	tampering.set(clientId, tamper);
}

/** The tampering of the client that the redirect goes to, if there is any */
export function findTamper(location: URL) {
	for (const [clientId, { redirectUris }] of clients) {
		if (
			redirectUris.some((uri) => new URL(uri).href === `${location.origin}${location.pathname}`)
		) {
			return tampering.get(clientId);
		}
	}
	return undefined;
}

export function recordAuthRequest(request: AuthRequest) {
	authRequests.get(request.client_id)?.push(request);
}

export function getAuthRequests(clientId: string) {
	return authRequests.get(clientId) ?? [];
}

interface Entry {
	payload: AdapterPayload;
	expiresAt: number;
}

const entries = new Map<string, Entry>();

/**
 * In-memory storage for the provider. Clients are not stored here but looked up in the
 * store above, so the tests can register them at runtime.
 */
export class MockAdapter implements Adapter {
	private readonly model: string;

	constructor(model: string) {
		this.model = model;
	}

	private key(id: string) {
		return `${this.model}:${id}`;
	}

	async upsert(id: string, payload: AdapterPayload, expiresIn?: number) {
		entries.set(this.key(id), {
			payload,
			expiresAt: expiresIn ? Date.now() + expiresIn * 1000 : Infinity,
		});
	}

	async find(id: string) {
		if (this.model === "Client") {
			const client = getClient(id);
			if (!client) {
				return undefined;
			}
			const metadata: AdapterPayload = {
				client_id: id,
				client_secret: client.secret,
				redirect_uris: client.redirectUris,
				grant_types: ["authorization_code"],
				response_types: ["code"],
				token_endpoint_auth_method: "client_secret_basic",
			};
			return metadata;
		}

		const entry = entries.get(this.key(id));
		if (!entry || entry.expiresAt < Date.now()) {
			return undefined;
		}
		return entry.payload;
	}

	private findBy(field: "uid" | "userCode", value: string) {
		for (const [key, { payload, expiresAt }] of entries) {
			if (key.startsWith(`${this.model}:`) && payload[field] === value && expiresAt > Date.now()) {
				return payload;
			}
		}
		return undefined;
	}

	async findByUid(uid: string) {
		return this.findBy("uid", uid);
	}

	async findByUserCode(userCode: string) {
		return this.findBy("userCode", userCode);
	}

	async consume(id: string) {
		const entry = entries.get(this.key(id));
		if (entry) {
			entry.payload.consumed = Math.floor(Date.now() / 1000);
		}
	}

	async destroy(id: string) {
		entries.delete(this.key(id));
	}

	async revokeByGrantId(grantId: string) {
		for (const [key, { payload }] of entries) {
			if (payload.grantId === grantId) {
				entries.delete(key);
			}
		}
	}
}
