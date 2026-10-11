/**
 * Minimal Gitea API mock for the E2E tests, started by Playwright (see playwright.config.ts).
 *
 * Two listeners share one in-memory state:
 *  - the Gitea API over TLS (the hub only accepts https:// repository URLs), reachable from the hub container
 *  - a plain HTTP control API on loopback that the tests use to create repositories and push commits
 */
import { readFileSync } from "node:fs";
import { createServer as createHttpServer } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { join } from "node:path";
import { GITEA_MOCK_API_PORT, GITEA_MOCK_CONTROL_PORT } from "../../tests/const.ts";
import { handleControlApi } from "./control-api.ts";
import { handleGiteaApi } from "./gitea-api.ts";
import { send } from "./http.ts";

const certDir = join(import.meta.dirname, "../certs");

const apiServer = createHttpsServer(
	{ key: readFileSync(join(certDir, "mock.key")), cert: readFileSync(join(certDir, "mock.pem")) },
	handleGiteaApi,
);

const controlServer = createHttpServer(async (req, res) => {
	try {
		await handleControlApi(req, res);
	} catch (err) {
		process.stderr.write(`${String(err)}\n`);
		send(res, 500, { error: String(err) });
	}
});

// The hub reaches the API through the Docker host gateway, so it can't be bound to loopback only
apiServer.listen(GITEA_MOCK_API_PORT, "0.0.0.0");
controlServer.listen(GITEA_MOCK_CONTROL_PORT, "127.0.0.1", () => {
	process.stdout.write(
		`Gitea mock: API on :${GITEA_MOCK_API_PORT}, control on :${GITEA_MOCK_CONTROL_PORT}\n`,
	);
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
	process.on(signal, () => process.exit(0));
}
