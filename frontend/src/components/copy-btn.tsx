import { useState } from "react";
import { Button } from "@/components/ui/button";
import { CheckIcon, ClipboardIcon } from "lucide-react";

export default function CopyButton({
	text,
	title = "Copy to clipboard",
}: {
	text: string;
	title?: string;
}) {
	const [copied, setCopied] = useState(false);

	const handleCopy = async () => {
		await navigator.clipboard.writeText(text);
		setCopied(true);
		setTimeout(() => setCopied(false), 2000);
	};

	return (
		<Button
			type="button"
			variant="ghost"
			size="icon"
			className="h-7 w-7 shrink-0 text-muted-foreground hover:text-foreground"
			onClick={handleCopy}
			title={title}
		>
			{copied ? (
				<CheckIcon className="h-4 w-4 text-green-500" />
			) : (
				<ClipboardIcon className="h-4 w-4" />
			)}
		</Button>
	);
}
