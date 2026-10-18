export interface RecordedRequest {
	method: string;
	path: string;
	headers: Record<string, string>;
	body: string;
}

export interface Hook {
	requests: RecordedRequest[];
	// Status code the receiver answers with, anything but 2xx fails the delivery
	status: number;
	// Sent as Location header, for answering with a redirect
	location?: string;
}

const hooks = new Map<string, Hook>();

export function getHook(name: string) {
	let hook = hooks.get(name);
	if (!hook) {
		hook = { requests: [], status: 200 };
		hooks.set(name, hook);
	}
	return hook;
}
