import { spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { join } from "node:path";
import { CLI_DATA_VOLUME, CLI_HUB_CONTAINER, CLI_PORT, CLI_URL, HUB_IMAGE } from "../const.ts";
import { BUSYBOX_IMAGE, docker, pullImage } from "./docker.ts";

const mockCertPath = join(import.meta.dirname, "../../mock/certs/mock.pem");

// Distroless nonroot UID/GID used by hub.Dockerfile
const HUB_UID_GID = "65532:65532";

// oxlint-disable-next-line no-control-regex
const ANSI_ESCAPE = /\x1b\[[0-9;?]*[ -/]*[@-~]/g;

export interface CliResult {
	status: number;
	stdout: string;
	stderr: string;
}

export function stripAnsi(text: string) {
	return text.replace(ANSI_ESCAPE, "");
}

function ensureBusybox() {
	if (!docker(["image", "inspect", BUSYBOX_IMAGE]).ok) {
		pullImage(BUSYBOX_IMAGE);
	}
}

export function initCliDataVolume() {
	docker(["rm", "-f", CLI_HUB_CONTAINER]);
	docker(["volume", "rm", "-f", CLI_DATA_VOLUME]);

	const created = docker(["volume", "create", CLI_DATA_VOLUME]);
	if (!created.ok) {
		throw new Error(`creating CLI data volume failed: ${created.stderr}`);
	}

	ensureBusybox();
	const chown = docker([
		"run",
		"--rm",
		"-v",
		`${CLI_DATA_VOLUME}:/data`,
		BUSYBOX_IMAGE,
		"chown",
		HUB_UID_GID,
		"/data",
	]);
	if (!chown.ok) {
		throw new Error(`initializing CLI data volume failed: ${chown.stderr}`);
	}
}

export function cliHubLogs() {
	const result = spawnSync("docker", ["logs", CLI_HUB_CONTAINER], { encoding: "utf8" });
	return stripAnsi(`${result.stdout ?? ""}${result.stderr ?? ""}`);
}

export async function startCliHub(appSecret: string) {
	docker(["rm", "-f", CLI_HUB_CONTAINER]);
	const created = docker([
		"run",
		"-d",
		"--name",
		CLI_HUB_CONTAINER,
		"-p",
		`127.0.0.1:${CLI_PORT}:8080`,
		"--add-host",
		"host.docker.internal:host-gateway",
		"-v",
		`${CLI_DATA_VOLUME}:/app/data`,
		"-v",
		`${mockCertPath}:/certs/mock.pem:ro`,
		"-e",
		`APP_URL=${CLI_URL}`,
		"-e",
		`APP_SECRET=${appSecret}`,
		"-e",
		"ALLOWED_INTERNAL_IPS=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
		"-e",
		"TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
		"-e",
		"SSL_CERT_FILE=/certs/mock.pem",
		HUB_IMAGE,
	]);
	if (!created.ok) {
		throw new Error(`starting the CLI hub failed: ${created.stderr}`);
	}

	const deadline = Date.now() + 60_000;
	while (Date.now() < deadline) {
		const state = docker(["inspect", "-f", "{{.State.Status}}", CLI_HUB_CONTAINER]);
		if (state.ok && state.stdout === "exited") {
			throw new Error(`the CLI hub exited before becoming healthy:\n${cliHubLogs()}`);
		}
		try {
			if ((await fetch(`${CLI_URL}/api/v1/health`)).ok) {
				return;
			}
		} catch {
			// Not listening yet
		}
		await new Promise((resolve) => setTimeout(resolve, 250));
	}
	throw new Error(`the CLI hub did not become healthy:\n${cliHubLogs()}`);
}

export function stopCliHub() {
	// Graceful stop so SQLite checkpoints WAL and closes cleanly before offline CLI commands
	docker(["stop", "-t", "10", CLI_HUB_CONTAINER]);
	docker(["rm", "-f", CLI_HUB_CONTAINER]);
}

export function cleanupCliHub() {
	docker(["rm", "-f", CLI_HUB_CONTAINER]);
	docker(["volume", "rm", "-f", CLI_DATA_VOLUME]);
}

export function execHubCli(
	args: string[],
	options: { env?: Record<string, string>; input?: string; timeoutMs?: number } = {},
): CliResult {
	const dockerArgs = ["exec"];
	if (options.input !== undefined) {
		dockerArgs.push("-i");
	}
	for (const [key, value] of Object.entries(options.env ?? {})) {
		dockerArgs.push("-e", `${key}=${value}`);
	}
	dockerArgs.push(CLI_HUB_CONTAINER, "/app/hub", ...args);

	const result = spawnSync("docker", dockerArgs, {
		encoding: "utf8",
		input: options.input,
		timeout: options.timeoutMs ?? 30_000,
	});
	return {
		status: result.status ?? 1,
		stdout: stripAnsi(result.stdout ?? "").trim(),
		stderr: stripAnsi(result.stderr ?? "").trim(),
	};
}

export function runHubCli(
	args: string[],
	options: {
		mountData?: boolean;
		env?: Record<string, string>;
		input?: string;
		timeoutMs?: number;
	} = {},
): CliResult {
	const name = `orca-hub-e2e-cli-cmd-${randomUUID().slice(0, 8)}`;
	const dockerArgs = ["run", "--rm", "--name", name];
	if (options.input !== undefined) {
		dockerArgs.push("-i");
	}
	if (options.mountData) {
		dockerArgs.push("-v", `${CLI_DATA_VOLUME}:/app/data`);
	}
	for (const [key, value] of Object.entries(options.env ?? {})) {
		dockerArgs.push("-e", `${key}=${value}`);
	}
	dockerArgs.push(HUB_IMAGE, ...args);

	try {
		const result = spawnSync("docker", dockerArgs, {
			encoding: "utf8",
			input: options.input,
			timeout: options.timeoutMs ?? 30_000,
		});
		if (result.error) {
			docker(["rm", "-f", name]);
		}
		return {
			status: result.status ?? 1,
			stdout: stripAnsi(result.stdout ?? "").trim(),
			stderr: stripAnsi(result.stderr ?? "").trim(),
		};
	} catch (error) {
		docker(["rm", "-f", name]);
		throw error;
	}
}

export function listDataFiles(subdir = "") {
	ensureBusybox();
	const target = subdir ? `/data/${subdir}` : "/data";
	const result = docker([
		"run",
		"--rm",
		"-v",
		`${CLI_DATA_VOLUME}:/data`,
		BUSYBOX_IMAGE,
		"ls",
		"-1",
		target,
	]);
	if (!result.ok) {
		throw new Error(`listing ${target} failed: ${result.stderr}`);
	}
	return result.stdout.split("\n").filter(Boolean);
}

export function dataFileSize(relativePath: string) {
	ensureBusybox();
	const result = docker([
		"run",
		"--rm",
		"-v",
		`${CLI_DATA_VOLUME}:/data`,
		BUSYBOX_IMAGE,
		"stat",
		"-c",
		"%s",
		`/data/${relativePath}`,
	]);
	if (!result.ok) {
		throw new Error(`stat /data/${relativePath} failed: ${result.stderr}`);
	}
	return Number(result.stdout);
}

export function readDataFile(relativePath: string): Buffer {
	ensureBusybox();
	const result = spawnSync(
		"docker",
		[
			"run",
			"--rm",
			"-v",
			`${CLI_DATA_VOLUME}:/data`,
			BUSYBOX_IMAGE,
			"cat",
			`/data/${relativePath}`,
		],
		{ maxBuffer: 50 * 1024 * 1024 },
	);
	if (result.status !== 0) {
		throw new Error(`reading /data/${relativePath} failed: ${result.stderr.toString("utf8")}`);
	}
	return result.stdout;
}

export function writeDataFile(relativePath: string, content: string | Buffer) {
	ensureBusybox();
	const result = spawnSync(
		"docker",
		[
			"run",
			"--rm",
			"-i",
			"-v",
			`${CLI_DATA_VOLUME}:/data`,
			BUSYBOX_IMAGE,
			"sh",
			"-c",
			`mkdir -p "$(dirname "/data/$1")" && cat > "/data/$1" && chown ${HUB_UID_GID} "/data/$1"`,
			"--",
			relativePath,
		],
		{ input: content },
	);
	if (result.status !== 0) {
		throw new Error(`writing /data/${relativePath} failed: ${result.stderr.toString("utf8")}`);
	}
}
