import { spawnSync } from "node:child_process";
import { expect } from "@playwright/test";
import { HUB_CONTAINER } from "../const.ts";

// oxlint-disable-next-line no-control-regex
const ANSI_ESCAPE = /\x1b\[[0-9;]*m/g;
const SETUP_CODE_LOG = /\scode=([0-9A-Z]{4}(?:-[0-9A-Z]{4}){3})/;

/** Reads the first-run setup code the way a real user does, from the hub's startup logs */
export async function readSetupCode(): Promise<string> {
	let code: string | undefined;

	await expect
		.poll(
			() => {
				const logs = spawnSync("docker", ["logs", HUB_CONTAINER], { encoding: "utf8" });
				const output = `${logs.stdout}${logs.stderr}`.replace(ANSI_ESCAPE, "");
				code = SETUP_CODE_LOG.exec(output)?.[1];
				return code;
			},
			{ timeout: 30_000 },
		)
		.toBeTruthy();

	return code!;
}
