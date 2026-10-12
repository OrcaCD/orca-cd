import { execFileSync, spawn, spawnSync } from "node:child_process";
import { join } from "node:path";
import { expect } from "@playwright/test";
import {
	AGENT_CONTAINER,
	AGENT_IMAGE,
	APP_URL,
	DUPLICATE_AGENT_CONTAINER,
	HUB_CONTAINER,
} from "../const.ts";

const e2eDir = join(import.meta.dirname, "../..");
const composeArgs = ["compose", "-f", "docker-compose.e2e.yml", "--profile", "agent"];

function docker(args: string[], env: NodeJS.ProcessEnv = process.env) {
	return execFileSync("docker", args, { cwd: e2eDir, env, encoding: "utf8" });
}

/** (Re)creates the agent container with the given config. */
export function startAgent({
	hubUrl = "",
	authToken = "",
}: {
	hubUrl?: string;
	authToken?: string;
}) {
	docker([...composeArgs, "up", "-d", "--force-recreate", "--no-build", "agent"], {
		...process.env,
		AGENT_HUB_URL: hubUrl,
		AGENT_AUTH_TOKEN: authToken,
	});
}

export function stopAgent() {
	docker([...composeArgs, "rm", "-sf", "agent"]);
}

export function stopAgentInBackground() {
	return new Promise<void>((resolve, reject) => {
		const child = spawn("docker", [...composeArgs, "rm", "-sf", "agent"], {
			cwd: e2eDir,
			stdio: "ignore",
		});
		child.once("error", reject);
		child.once("exit", (code) =>
			code === 0 ? resolve() : reject(new Error(`docker compose rm exited with code ${code}`)),
		);
	});
}

export function agentLogs() {
	const result = spawnSync("docker", ["logs", AGENT_CONTAINER], { cwd: e2eDir, encoding: "utf8" });
	return `${result.stdout}${result.stderr}`;
}

export function agentState() {
	const [status, exitCode] = docker([
		"inspect",
		"-f",
		"{{.State.Status}} {{.State.ExitCode}}",
		AGENT_CONTAINER,
	])
		.trim()
		.split(" ");
	return { status, exitCode: Number(exitCode) };
}

export async function restartHub() {
	docker(["restart", HUB_CONTAINER]);
	await expect
		.poll(async () => (await fetch(`${APP_URL}/api/v1/health`).catch(() => null))?.ok, {
			timeout: 60_000,
		})
		.toBe(true);
}

/** Starts a second agent container next to the compose one, e.g. to test duplicate connections */
export function startDuplicateAgent({ hubUrl, authToken }: { hubUrl: string; authToken: string }) {
	const network = docker([
		"inspect",
		"-f",
		"{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}",
		HUB_CONTAINER,
	]).trim();
	// Connecting to the hub works without the Docker socket, the agent only keeps retrying the daemon ping
	docker([
		"run",
		"-d",
		"--name",
		DUPLICATE_AGENT_CONTAINER,
		"--network",
		network,
		"-e",
		`HUB_URL=${hubUrl}`,
		"-e",
		`AUTH_TOKEN=${authToken}`,
		AGENT_IMAGE,
	]);
}

export function duplicateAgentLogs() {
	const result = spawnSync("docker", ["logs", DUPLICATE_AGENT_CONTAINER], { encoding: "utf8" });
	return `${result.stdout}${result.stderr}`;
}

export function removeDuplicateAgent() {
	spawnSync("docker", ["rm", "-f", DUPLICATE_AGENT_CONTAINER], { encoding: "utf8" });
}

export function agentHealthcheck() {
	const result = spawnSync("docker", ["exec", AGENT_CONTAINER, "/app/agent", "healthcheck"], {
		cwd: e2eDir,
		encoding: "utf8",
	});
	return result.status === 0;
}
