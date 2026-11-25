import { Download, FileJson, WrapText } from "lucide-react";
import { useState } from "react";
import ErrorAlert from "@/components/alerts/error-alert";
import { Button, buttonVariants } from "@/components/ui/button";
import {
	CodeBlock,
	CodeBlockActions,
	CodeBlockContent,
	CodeBlockCopyButton,
	CodeBlockHeader,
	CodeBlockLine,
	CodeBlockTitle,
} from "@/components/ui/code-block";
import { Skeleton } from "@/components/ui/skeleton";
import { useHighlightedCode } from "@/hooks/use-highlighted-code";
import { useContainerInspect, type ContainerInfo } from "@/lib/application-containers";
import { m } from "@/lib/paraglide/messages";

export function InspectPanel({
	applicationId,
	container,
}: {
	applicationId: string;
	container: ContainerInfo;
}) {
	const { data, error, isLoading } = useContainerInspect(applicationId, container);
	const [wrap, setWrap] = useState(false);
	const json = data?.inspectJson ?? "";
	const lines = useHighlightedCode(json, "json");
	const filename = `${container.name.replace(/^\//, "")}-inspect.json`;

	if (error && !data) {
		return (
			<ErrorAlert title={m.monitoringInspectFailed()} description={(error as Error).message} />
		);
	}

	if (isLoading || !data) {
		return <Skeleton className="h-64 w-full" />;
	}

	return (
		<CodeBlock>
			<CodeBlockHeader>
				<CodeBlockTitle>
					<FileJson />
					{filename}
				</CodeBlockTitle>
				<CodeBlockActions>
					<Button
						variant="ghost"
						size="icon-xs"
						aria-label={m.wrapLines()}
						aria-pressed={wrap}
						className="aria-pressed:bg-muted aria-pressed:text-foreground"
						onClick={() => setWrap(!wrap)}
					>
						<WrapText />
					</Button>
					<CodeBlockCopyButton value={json} />
					<a
						href={`data:application/json;charset=utf-8,${encodeURIComponent(json)}`}
						download={filename}
						aria-label={m.monitoringInspectDownload()}
						className={buttonVariants({ variant: "ghost", size: "icon-xs" })}
					>
						<Download />
					</a>
				</CodeBlockActions>
			</CodeBlockHeader>
			<CodeBlockContent lineNumbers wrap={wrap} className="max-h-[32rem] scroll-fade-none">
				{lines.map((line, index) => (
					<CodeBlockLine key={index}>{line}</CodeBlockLine>
				))}
			</CodeBlockContent>
		</CodeBlock>
	);
}
