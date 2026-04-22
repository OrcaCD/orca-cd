import { spawnSync } from "node:child_process";

export default function globalTeardown() {
	const result = spawnSync("docker", ["compose", "-f", "docker-compose.e2e.yml", "down"], { stdio: "inherit" });
	if (result.status !== 0) {
		throw new Error(`docker compose down failed with exit code ${result.status}`);
	}
}
