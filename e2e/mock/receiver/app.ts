/**
 * Minimal webhook receiver for the notification E2E tests, started by Playwright (see playwright.config.ts).
 */
import { createServer } from "node:http";
import { RECEIVER_MOCK_API_PORT, RECEIVER_MOCK_CONTROL_PORT } from "../../tests/const.ts";
import { send } from "../gitea/http.ts";
import { handleControlApi } from "./control-api.ts";
import { handleReceiverApi } from "./receiver-api.ts";

function serve(handler: typeof handleReceiverApi) {
	return createServer(async (req, res) => {
		try {
			await handler(req, res);
		} catch (err) {
			process.stderr.write(`${String(err)}\n`);
			send(res, 500, { error: String(err) });
		}
	});
}

// The hub reaches the receiver through the Docker host gateway, so it can't be bound to loopback only
serve(handleReceiverApi).listen(RECEIVER_MOCK_API_PORT, "0.0.0.0");
serve(handleControlApi).listen(RECEIVER_MOCK_CONTROL_PORT, "127.0.0.1", () => {
	process.stdout.write(
		`Receiver mock: hooks on :${RECEIVER_MOCK_API_PORT}, control on :${RECEIVER_MOCK_CONTROL_PORT}\n`,
	);
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
	process.on(signal, () => process.exit(0));
}
