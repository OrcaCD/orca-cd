import { createPrivateKey, createPublicKey, hkdfSync, sign } from "node:crypto";
import { decodeJwt, SignJWT, UnsecuredJWT } from "jose";
import { ADMIN_EMAIL, ADMIN_NAME, APP_URL } from "../const.ts";

export const AUTH_COOKIE = "orcacd_auth";

// Must match APP_SECRET in docker-compose.e2e.yml
const APP_SECRET = "supersecretkey_minimum_32_characters";

// PKCS#8 prefix for an Ed25519 private key, followed by the 32 byte seed
const ED25519_PKCS8_PREFIX = Buffer.from("302e020100300506032b657004220420", "hex");

function deriveSigningKey(secret: string) {
	return createPrivateKey({
		key: Buffer.concat([
			ED25519_PKCS8_PREFIX,
			Buffer.from(hkdfSync("sha256", secret, Buffer.alloc(0), "JWT_SIGNING_KEY", 32)),
		]),
		format: "der",
		type: "pkcs8",
	});
}

const hubKey = deriveSigningKey(APP_SECRET);

const base64url = (value: Buffer | string) => Buffer.from(value).toString("base64url");

export type Claims = Record<string, unknown>;

export interface TokenOptions {
	subject: string;
	/** Seconds relative to now, negative values produce an expired token */
	expiresIn?: number;
	role?: string;
	/** Defaults to the hub's APP_SECRET */
	secret?: string;
}

/** The claims of a valid user token, so tests can break one claim at a time */
export function userClaims({ subject, expiresIn = 3600, role = "admin" }: TokenOptions): Claims {
	const now = Math.floor(Date.now() / 1000);
	return {
		iss: APP_URL,
		sub: subject,
		aud: ["user"],
		iat: now - 7200,
		nbf: now - 7200,
		exp: now + expiresIn,
		name: ADMIN_NAME,
		email: ADMIN_EMAIL,
		role,
		passwordChangeRequired: false,
		isLocal: true,
	};
}

/** Signs a user token the same way the hub does */
export function signUserToken(options: TokenOptions) {
	return new SignJWT(userClaims(options))
		.setProtectedHeader({ alg: "EdDSA", typ: "JWT" })
		.sign(deriveSigningKey(options.secret ?? APP_SECRET));
}

/** Signs arbitrary claims with the hub's key and an arbitrary header */
export function forgeToken(claims: Claims, header: Claims = { alg: "EdDSA", typ: "JWT" }) {
	return forgeRawPayload(JSON.stringify(claims), header);
}

/** Signs a payload that does not have to be a JSON object with the hub's key */
export function forgeRawPayload(payload: string, header: Claims = { alg: "EdDSA", typ: "JWT" }) {
	const signingInput = `${base64url(JSON.stringify(header))}.${base64url(payload)}`;
	return `${signingInput}.${base64url(sign(null, Buffer.from(signingInput), hubKey))}`;
}

/** Algorithm confusion: an HS256 token that uses the hub's public key as the HMAC secret */
export function hmacTokenWithPublicKey(claims: Claims) {
	const publicKey = createPublicKey(hubKey).export({ format: "der", type: "spki" }).subarray(-32);
	return new SignJWT(claims).setProtectedHeader({ alg: "HS256", typ: "JWT" }).sign(publicKey);
}

/** Replaces claims of a token without re-signing it */
export function withClaims(token: string, changes: Claims) {
	const [header, , signature] = token.split(".");
	const payload = base64url(JSON.stringify({ ...decodeJwt(token), ...changes }));
	return `${header}.${payload}.${signature}`;
}

/** Keeps the claims of a token but drops the signature and claims alg "none" */
export function unsignedToken(token: string) {
	return new UnsecuredJWT(decodeJwt(token)).encode();
}
