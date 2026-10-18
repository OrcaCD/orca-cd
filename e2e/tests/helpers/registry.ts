import { REGISTRY_CONTAINER, REGISTRY_HOST, REGISTRY_PORT } from "../const.ts";
import { BUSYBOX_IMAGE, docker } from "./docker.ts";

const REGISTRY_IMAGE = "registry:3";
const BUILD_CONTAINER = "orca-registry-e2e-build";

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

export async function startRegistry() {
	docker(["rm", "-f", REGISTRY_CONTAINER]);
	run(["pull", REGISTRY_IMAGE]);
	run([
		"run",
		"-d",
		"--name",
		REGISTRY_CONTAINER,
		"-p",
		`127.0.0.1:${REGISTRY_PORT}:5000`,
		REGISTRY_IMAGE,
	]);

	const deadline = Date.now() + 30_000;
	while (Date.now() < deadline) {
		const response = await fetch(`http://${REGISTRY_HOST}/v2/`).catch(() => null);
		if (response?.ok) {
			return;
		}
		await new Promise((resolve) => setTimeout(resolve, 250));
	}
	throw new Error("the local registry did not become ready");
}

export function stopRegistry() {
	docker(["rm", "-f", REGISTRY_CONTAINER]);
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

export function removeLocalImage(ref: string) {
	docker(["image", "rm", ref]);
}

export function containerImageId(containerId: string) {
	return run(["inspect", "-f", "{{.Image}}", containerId]);
}

export function imageExists(id: string) {
	return docker(["image", "inspect", id]).ok;
}
