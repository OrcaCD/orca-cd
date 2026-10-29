import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import {
	ArrowRight,
	Check,
	CheckIcon,
	ChevronDown,
	CircleCheck,
	CopyIcon,
	ExternalLink,
	OctagonAlert,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { CodeBlock, CodeBlockContent, CodeBlockLine } from "@/components/ui/code-block";
import { useHighlightedCode } from "@/hooks/use-highlighted-code";
import { useFetch } from "@/lib/api";
import { AgentStatus, type Agent, type AgentWithTokenResponse } from "@/lib/agents";
import { m } from "@/lib/paraglide/messages";

const agentEnvDocsUrl = "https://orcacd.dev/docs/configuration/env-variables#agent";

export function buildAgentComposeFile({
	image,
	hubUrl,
	authToken,
}: {
	image: string;
	hubUrl: string;
	authToken: string;
}): string {
	return `services:
  agent:
    image: ${image}
    restart: unless-stopped
    use_api_socket: true
    environment:
      HUB_URL: ${hubUrl}
      AUTH_TOKEN: ${authToken}
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    cap_add:
      # Inspect deployment paths even when application containers restrict directory permissions.
      - DAC_READ_SEARCH
    volumes:
      - ./deployments:/deployments
`;
}

type StepState = "done" | "active" | "pending";

function SetupStep({ index, label, state }: { index: number; label: string; state: StepState }) {
	return (
		<li
			aria-current={state === "active" ? "step" : undefined}
			className={cn(
				"flex min-w-0 items-center gap-2 rounded-lg border px-3 py-2 text-sm",
				state === "done" && "border-transparent bg-muted text-emerald-600 dark:text-emerald-400",
				state === "active" && "border-primary font-medium",
				state === "pending" && "border-transparent bg-muted text-muted-foreground",
			)}
		>
			{state === "done" ? (
				<Check className="size-3.5 shrink-0" aria-hidden="true" />
			) : (
				<span className="shrink-0 whitespace-nowrap" aria-hidden="true">
					{index} ·
				</span>
			)}
			<span className="truncate">{label}</span>
		</li>
	);
}

function CopyButtons({ composeFile, authToken }: { composeFile: string; authToken: string }) {
	const [copied, setCopied] = useState(false);

	useEffect(() => {
		if (!copied) {
			return;
		}
		const timeout = setTimeout(() => setCopied(false), 2000);
		return () => clearTimeout(timeout);
	}, [copied]);

	const copy = async (value: string) => {
		await navigator.clipboard.writeText(value);
		setCopied(true);
	};

	return (
		<ButtonGroup>
			<Button type="button" variant="outline" size="sm" onClick={() => copy(composeFile)}>
				{copied ? <CheckIcon className="text-emerald-500" /> : <CopyIcon />}
				{m.copy()}
			</Button>
			<DropdownMenu>
				<DropdownMenuTrigger
					render={
						<Button type="button" variant="outline" size="icon-sm" aria-label={m.moreCopyOptions()}>
							<ChevronDown />
						</Button>
					}
				/>
				<DropdownMenuContent align="end" className="w-auto">
					<DropdownMenuItem onClick={() => copy(authToken)}>
						<CopyIcon />
						{m.copyAgentAuthToken()}
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</ButtonGroup>
	);
}

function ConnectedState({
	agent,
	onNavigate,
}: {
	agent: Pick<Agent, "name" | "status">;
	onNavigate: () => void;
}) {
	if (agent.status === AgentStatus.Error) {
		return (
			<div role="status" className="flex flex-col items-center gap-2 py-6 text-center">
				<OctagonAlert className="size-10 text-red-500" aria-hidden="true" />
				<p className="font-medium">{m.agentSetupConnectedWithError({ name: agent.name })}</p>
				<p className="max-w-md text-sm text-muted-foreground">{m.agentStatusErrorDescription()}</p>
			</div>
		);
	}

	return (
		<div role="status" className="flex flex-col items-center gap-2 py-6 text-center">
			<CircleCheck className="size-10 text-emerald-500" aria-hidden="true" />
			<p className="font-medium">{m.agentSetupConnected({ name: agent.name })}</p>
			<p className="text-sm text-muted-foreground">{m.agentSetupConnectedDescription()}</p>
			<Button
				className="mt-2"
				nativeButton={false}
				render={<Link to="/applications" onClick={onNavigate} />}
			>
				{m.agentSetupCreateFirstApplication()}
				<ArrowRight />
			</Button>
		</div>
	);
}

export default function AgentSetupDialog({
	agent,
	open,
	onOpenChange,
	onOpenChangeComplete,
}: {
	agent: AgentWithTokenResponse | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onOpenChangeComplete?: (open: boolean) => void;
}) {
	// The SSE connection revalidates this as soon as the agent connects.
	const { data: liveAgent } = useFetch<Agent>(open && agent ? `/agents/${agent.id}` : null);
	const status = liveAgent?.status ?? agent?.status ?? AgentStatus.Offline;
	const isConnected = status !== AgentStatus.Offline;

	const composeFile = agent
		? buildAgentComposeFile({
				image: agent.agentImage,
				hubUrl: window.location.origin,
				authToken: agent.authToken,
			})
		: "";
	const composeLines = useHighlightedCode(composeFile, "yaml");

	return (
		<Dialog open={open} onOpenChange={onOpenChange} onOpenChangeComplete={onOpenChangeComplete}>
			<DialogContent className="flex max-h-[90dvh] flex-col gap-5 overflow-hidden sm:max-w-2xl">
				<DialogHeader className="shrink-0">
					<DialogTitle>{m.agentSetupTitle({ name: agent?.name ?? "" })}</DialogTitle>
				</DialogHeader>

				<ol className="grid shrink-0 gap-2 sm:grid-cols-3">
					<SetupStep index={1} label={m.agentSetupStepCreated()} state="done" />
					<SetupStep
						index={2}
						label={m.agentSetupStepStart()}
						state={isConnected ? "done" : "active"}
					/>
					<SetupStep
						index={3}
						label={m.agentSetupStepConnected()}
						state={isConnected ? "active" : "pending"}
					/>
				</ol>

				{agent && isConnected ? (
					<ConnectedState
						agent={{ name: agent.name, status }}
						onNavigate={() => onOpenChange(false)}
					/>
				) : (
					<>
						<div className="flex min-h-0 flex-1 flex-col gap-2">
							<div className="flex items-center justify-between gap-2">
								<p className="text-sm text-muted-foreground">{m.agentSetupComposeLabel()}</p>
								<CopyButtons composeFile={composeFile} authToken={agent?.authToken ?? ""} />
							</div>
							<CodeBlock className="min-h-0 flex-1">
								<CodeBlockContent className="min-h-0 flex-1">
									{composeLines.map((line, index) => (
										<CodeBlockLine key={index}>{line}</CodeBlockLine>
									))}
								</CodeBlockContent>
							</CodeBlock>
							<a
								href={agentEnvDocsUrl}
								target="_blank"
								rel="noopener noreferrer"
								className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
							>
								{m.agentSetupMoreOptions()}
								<ExternalLink className="size-3" aria-hidden="true" />
							</a>
						</div>

						<div
							role="status"
							className="flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 text-sm font-medium"
						>
							<span className="relative flex size-2.5 shrink-0" aria-hidden="true">
								<span className="absolute inline-flex size-full animate-ping rounded-full bg-amber-500 opacity-75" />
								<span className="relative inline-flex size-2.5 rounded-full bg-amber-500" />
							</span>
							{m.agentSetupWaiting({ name: agent?.name ?? "" })}
						</div>
					</>
				)}
			</DialogContent>
		</Dialog>
	);
}
