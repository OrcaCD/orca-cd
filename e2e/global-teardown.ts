import { spawnSync } from "child_process";

export default function globalTeardown() {
	spawnSync(
		"docker",
		["compose", "-f", "docker-compose.e2e.yml", "down"],
		{ stdio: "inherit" },
	);
}
