import { z } from "zod";

const ipOrCidrSchema = z.union([z.ipv4(), z.ipv6(), z.cidrv4(), z.cidrv6()]);

export function isIpOrCidr(entry: string): boolean {
	return ipOrCidrSchema.safeParse(entry).success;
}

export function parseIpList(value: string): string[] {
	return value
		.split(/\r?\n|,/)
		.map((entry) => entry.trim())
		.filter((entry) => entry !== "");
}
