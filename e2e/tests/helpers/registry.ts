import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { REGISTRY_CONTAINER, REGISTRY_HOST, REGISTRY_PORT } from "../const.ts";
import { BUSYBOX_IMAGE, docker } from "./docker.ts";

const REGISTRY_IMAGE = "registry:3";
const REGISTRY_VOLUME = "orca-registry-e2e-data";
const BUILD_CONTAINER = "orca-registry-e2e-build";

// bcrypt hash of a throwaway password. Nobody logs in: the registry only has to
// reject the agent's anonymous requests.
const HTPASSWD = "e2e:$2a$10$pYAQYmLZIrnCG2LDaR7U4en5Ii1zcPCWqNIjo9hpSJj/J61tN5MH6\n";

function run(args: string[]) {
	const result = docker(args);
	if (!result.ok) {
		throw new Error(`docker ${args.join(" ")} failed: ${result.stderr}`);
	}
	return result.stdout;
}

export function imageRef(name: string, tag = "latest") {
	return `${REGISTRY_HOST}/${name}:${tag}`;
}

async function runRegistry(requireAuth: boolean) {
	docker(["rm", "-f", REGISTRY_CONTAINER]);
	const args = [
		"create",
		"--name",
		REGISTRY_CONTAINER,
		"-p",
		`127.0.0.1:${REGISTRY_PORT}:5000`,
		"-v",
		`${REGISTRY_VOLUME}:/var/lib/registry`,
	];
	if (requireAuth) {
		args.push(
			"-e",
			"REGISTRY_AUTH=htpasswd",
			"-e",
			"REGISTRY_AUTH_HTPASSWD_REALM=e2e",
			"-e",
			"REGISTRY_AUTH_HTPASSWD_PATH=/htpasswd",
		);
	}
	run([...args, REGISTRY_IMAGE]);
	if (requireAuth) {
		const dir = mkdtempSync(join(tmpdir(), "orca-registry-"));
		try {
			const file = join(dir, "htpasswd");
			writeFileSync(file, HTPASSWD);
			run(["cp", file, `${REGISTRY_CONTAINER}:/htpasswd`]);
		} finally {
			rmSync(dir, { recursive: true, force: true });
		}
	}
	run(["start", REGISTRY_CONTAINER]);

	const deadline = Date.now() + 30_000;
	while (Date.now() < deadline) {
		const response = await fetch(`http://${REGISTRY_HOST}/v2/`).catch(() => null);
		if (response && (response.ok || (requireAuth && response.status === 401))) {
			return;
		}
		await new Promise((resolve) => setTimeout(resolve, 250));
	}
	throw new Error("the local registry did not become ready");
}

export async function startRegistry() {
	docker(["rm", "-f", REGISTRY_CONTAINER]);
	docker(["volume", "rm", "-f", REGISTRY_VOLUME]);
	run(["pull", REGISTRY_IMAGE]);
	await runRegistry(false);
}

export function requireRegistryAuth() {
	return runRegistry(true);
}

export function allowAnonymousRegistry() {
	return runRegistry(false);
}

export function stopRegistry() {
	docker(["rm", "-f", REGISTRY_CONTAINER]);
	docker(["volume", "rm", "-f", REGISTRY_VOLUME]);
}

/**
 * Pushes a new image version to the registry and removes the tag locally again.
 * A deployed container keeps its image, but the local daemon no longer has the
 * tag, so the agent sees the registry's version as newer.
 */
export function pushImage(ref: string, version: string) {
	docker(["rm", "-f", BUILD_CONTAINER]);
	run(["create", "--name", BUILD_CONTAINER, BUSYBOX_IMAGE]);
	try {
		run(["commit", "--change", `ENV IMAGE_VERSION=${version}`, BUILD_CONTAINER, ref]);
	} finally {
		docker(["rm", "-f", BUILD_CONTAINER]);
	}
	run(["push", ref]);
	docker(["image", "rm", ref]);
}

export function createLocalImage(ref: string) {
	const dir = mkdtempSync(join(tmpdir(), "orca-local-image-"));
	try {
		writeFileSync(join(dir, "Dockerfile"), `FROM ${BUSYBOX_IMAGE}\nENV IMAGE_VERSION=local\n`);
		run(["build", "-q", "-t", ref, dir]);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}

export function removeLocalImage(ref: string) {
	docker(["image", "rm", ref]);
}

export function containerImageId(containerId: string) {
	return run(["inspect", "-f", "{{.Image}}", containerId]);
}

export function imageExists(id: string) {
	return docker(["image", "inspect", id]).ok;
}
