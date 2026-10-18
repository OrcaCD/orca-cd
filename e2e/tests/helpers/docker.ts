import { spawnSync } from "node:child_process";
import { AGENT_CONTAINER } from "../const.ts";

export const BUSYBOX_IMAGE = "busybox:1.38";

function docker(args: string[]) {
	const result = spawnSync("docker", args, { encoding: "utf8" });
	return { ok: result.status === 0, stdout: result.stdout.trim(), stderr: result.stderr.trim() };
}

export interface ProjectContainer {
	id: string;
	state: string;
	status: string;
}

export function pullImage(image: string) {
	const result = docker(["pull", image]);
	if (!result.ok) {
		throw new Error(`docker pull ${image} failed: ${result.stderr}`);
	}
}

export function appContainers(applicationId: string): ProjectContainer[] {
	if (!applicationId) {
		return [];
	}
	const { stdout } = docker([
		"ps",
		"-a",
		"--filter",
		`label=orca-cd.application-id=${applicationId}`,
		"--format",
		"{{.ID}}|{{.State}}|{{.Status}}",
	]);
	return stdout
		.split("\n")
		.filter(Boolean)
		.map((line) => {
			const [id, state, status] = line.split("|");
			return { id, state, status };
		});
}

/** Every container of a compose project, also those created before the agent labels them. */
export function projectContainers(project: string): ProjectContainer[] {
	const { stdout } = docker([
		"ps",
		"-a",
		"--filter",
		`label=com.docker.compose.project=${project}`,
		"--format",
		"{{.ID}}|{{.State}}|{{.Status}}",
	]);
	return stdout
		.split("\n")
		.filter(Boolean)
		.map((line) => {
			const [id, state, status] = line.split("|");
			return { id, state, status };
		});
}

export function execInContainer(id: string, command: string[]) {
	const result = docker(["exec", id, ...command]);
	if (!result.ok) {
		throw new Error(`docker exec ${id} ${command.join(" ")} failed: ${result.stderr}`);
	}
	return result.stdout;
}

export function containerMounts(id: string) {
	const { ok, stdout } = docker(["inspect", "-f", "{{json .Mounts}}", id]);
	if (!ok) {
		throw new Error(`docker inspect ${id} failed`);
	}
	return JSON.parse(stdout) as {
		Type: string;
		Name?: string;
		Source: string;
		Destination: string;
	}[];
}

export function removeVolumes(...names: string[]) {
	docker(["volume", "rm", "-f", ...names]);
}

export function stopContainer(id: string) {
	const result = docker(["stop", id]);
	if (!result.ok) {
		throw new Error(`docker stop ${id} failed: ${result.stderr}`);
	}
}

export function startContainer(id: string) {
	const result = docker(["start", id]);
	if (!result.ok) {
		throw new Error(`docker start ${id} failed: ${result.stderr}`);
	}
}

export function inspectContainer(id: string) {
	const { ok, stdout } = docker(["inspect", id]);
	if (!ok) {
		throw new Error(`docker inspect ${id} failed`);
	}
	const [info] = JSON.parse(stdout) as {
		Config: { Env: string[]; Image: string; Labels: Record<string, string> };
		State: { Status: string; Health?: { Status: string } };
	}[];
	return {
		image: info.Config.Image,
		labels: info.Config.Labels,
		env: Object.fromEntries(
			info.Config.Env.map((entry) => {
				const separator = entry.indexOf("=");
				return [entry.slice(0, separator), entry.slice(separator + 1)];
			}),
		),
		state: info.State.Status,
		health: info.State.Health?.Status,
	};
}

// The deployments directory is owned by root on Linux hosts and the agent image has no shell,
// so a throwaway container with the agent's volumes reads it
function runWithAgentVolumes(command: string[]) {
	return spawnSync(
		"docker",
		["run", "--rm", "--volumes-from", AGENT_CONTAINER, BUSYBOX_IMAGE, ...command],
		{ encoding: "utf8" },
	);
}

export function deployedComposeFile(project: string) {
	const result = runWithAgentVolumes(["cat", `/deployments/${project}/compose.yaml`]);
	return result.status === 0 ? result.stdout : undefined;
}

export function deploymentDirExists(project: string) {
	return runWithAgentVolumes(["test", "-d", `/deployments/${project}`]).status === 0;
}

export function composeProject(applicationId: string) {
	const [container] = appContainers(applicationId);
	return container
		? inspectContainer(container.id).labels["com.docker.compose.project"]
		: undefined;
}

export function removeApplicationContainers(applicationId: string) {
	const project = composeProject(applicationId);
	const ids = appContainers(applicationId).map((container) => container.id);
	if (ids.length > 0) {
		docker(["rm", "-f", ...ids]);
	}
	if (project) {
		docker(["network", "rm", `${project}_default`]);
	}
}
