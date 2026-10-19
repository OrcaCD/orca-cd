/**
 * Minimal OpenID Connect identity provider for the SSO E2E tests, started by Playwright
 */
import { createServer } from "node:http";
import { OIDC_MOCK_API_PORT, OIDC_MOCK_CONTROL_PORT } from "../../tests/const.ts";
import { send } from "../gitea/http.ts";
import { handleControlApi } from "./control-api.ts";
import { handleOidcApi } from "./provider.ts";

function serve(handler: typeof handleOidcApi) {
	return createServer(async (req, res) => {
		try {
			await handler(req, res);
		} catch (err) {
			process.stderr.write(`${String(err)}\n`);
			if (!res.headersSent) {
				send(res, 500, { error: String(err) });
			}
		}
	});
}

// The hub reaches the provider through the Docker host gateway, so it can't be bound to loopback only
serve(handleOidcApi).listen(OIDC_MOCK_API_PORT, "0.0.0.0");
serve(handleControlApi).listen(OIDC_MOCK_CONTROL_PORT, "127.0.0.1", () => {
	process.stdout.write(
		`OIDC mock: provider on :${OIDC_MOCK_API_PORT}, control on :${OIDC_MOCK_CONTROL_PORT}\n`,
	);
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
	process.on(signal, () => process.exit(0));
}
