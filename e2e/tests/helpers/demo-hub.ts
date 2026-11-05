import { spawnSync } from "node:child_process";
import { join } from "node:path";
import { DEMO_COMPOSE_PROJECT, DEMO_URL } from "../const.ts";

const e2eDir = join(import.meta.dirname, "../..");

function compose(...args: string[]) {
	return spawnSync(
		"docker",
		[
			"compose",
			"-p",
			DEMO_COMPOSE_PROJECT,
			"-f",
			"docker-compose.e2e.yml",
			"--profile",
			"demo",
			...args,
		],
		{ cwd: e2eDir, encoding: "utf8" },
	);
}

export async function startDemoHub() {
	const result = compose("up", "--detach", "--force-recreate", "--no-build", "hub-demo");
	if (result.status !== 0) {
		throw new Error(`starting the demo hub failed: ${result.stderr}`);
	}

	const deadline = Date.now() + 60_000;
	while (Date.now() < deadline) {
		try {
			if ((await fetch(`${DEMO_URL}/api/v1/health`)).ok) {
				return;
			}
		} catch {
			// Not listening yet
		}
		await new Promise((resolve) => setTimeout(resolve, 500));
	}
	throw new Error("the demo hub did not become healthy");
}

export function stopDemoHub() {
	compose("down");
}
