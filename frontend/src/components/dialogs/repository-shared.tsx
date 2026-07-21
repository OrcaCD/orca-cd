import { ExternalLink, EyeIcon, EyeOffIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldLabel,
	FieldTitle,
} from "@/components/ui/field";
import CopyButton from "@/components/copy-btn";
import { m } from "@/lib/paraglide/messages";
import type { RepositorySyncType } from "@/lib/repositories";
import { cn } from "@/lib/utils";

const webhookDocsUrl = "https://orcacd.dev/docs/configuration/webhooks";

export function RepositoryDialogLoadingOverlay({ isLoading }: { isLoading: boolean }) {
	if (!isLoading) {
		return null;
	}
	return (
		<div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-background/80 backdrop-blur-sm">
			<Loader2Icon className="h-8 w-8 animate-spin text-primary" />
			<p className="text-sm text-muted-foreground">{m.loadingDots()}</p>
		</div>
	);
}

export function WebhookSetupDetails({
	webhookUrl,
	webhookSecret,
}: {
	webhookUrl: string | undefined;
	webhookSecret: string | undefined;
}) {
	const [visible, setVisible] = useState(false);

	if (!webhookSecret) {
		return null;
	}

	return (
		<div className="space-y-3 mt-4">
			<p className="text-sm text-muted-foreground">{m.repositoryWebhookSetupDescription()}</p>

			<div className="space-y-1">
				<p className="text-xs font-medium">{m.webhookUrl()}</p>
				<div className="flex items-center gap-1 rounded-md border bg-muted/50 px-3 py-1">
					<code className="flex-1 truncate font-mono text-sm">{webhookUrl}</code>
					<CopyButton text={webhookUrl ?? ""} title={m.copyWebhookUrl()} />
				</div>
			</div>

			<div className="space-y-1">
				<p className="text-xs font-medium">{m.webhookSecret()}</p>
				<div className="flex items-center gap-1 rounded-md border bg-muted/50 px-3 py-1">
					<code className="flex-1 truncate font-mono text-sm">
						{visible ? webhookSecret : "•".repeat(32)}
					</code>
					<Button
						type="button"
						variant="ghost"
						size="icon"
						className="h-7 w-7 shrink-0 text-muted-foreground hover:text-foreground"
						onClick={() => setVisible((v) => !v)}
						title={visible ? m.hideSecret() : m.revealSecret()}
					>
						{visible ? <EyeOffIcon className="h-4 w-4" /> : <EyeIcon className="h-4 w-4" />}
					</Button>
					<CopyButton text={webhookSecret} title={m.copyWebhookSecret()} />
				</div>
				<p className="text-xs text-muted-foreground">{m.saveSecretNow()}</p>
			</div>
			<a
				href={webhookDocsUrl}
				target="_blank"
				rel="noopener noreferrer"
				className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
			>
				{m.webhookDocsLink()}
				<ExternalLink className="h-3 w-3" />
			</a>
		</div>
	);
}

function getSyncTypes(): { id: RepositorySyncType; label: string; description: string }[] {
	return [
		{
			id: "webhook",
			label: m.syncTypeWebhookRecommended(),
			description: m.syncTypeWebhookDescription(),
		},
		{
			id: "polling",
			label: m.syncTypePolling(),
			description: m.syncTypePollingDescription(),
		},
		{
			id: "manual",
			label: m.syncTypeManual(),
			description: m.syncTypeManualDescription(),
		},
	];
}

export function SyncTypeRadioGroup({
	value,
	onChange,
	onBlur,
	className,
}: {
	value: RepositorySyncType;
	onChange: (v: RepositorySyncType) => void;
	onBlur: () => void;
	className?: string;
}) {
	const syncTypes = getSyncTypes();
	return (
		<RadioGroup
			value={value}
			onBlur={onBlur}
			onValueChange={(v) => onChange(v as RepositorySyncType)}
			className={cn("w-fit", className)}
		>
			{syncTypes.map((type) => (
				<FieldLabel
					htmlFor={`syncType-${type.id}`}
					key={type.id}
					className="cursor-pointer transition-colors"
				>
					<Field orientation="horizontal">
						<FieldContent className="ps-1">
							<FieldTitle>{type.label}</FieldTitle>
							<FieldDescription>{type.description}</FieldDescription>
						</FieldContent>
						<RadioGroupItem value={type.id} id={`syncType-${type.id}`} />
					</Field>
				</FieldLabel>
			))}
		</RadioGroup>
	);
}
