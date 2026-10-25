// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { InfoIcon } from "lucide-react";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Separator } from "@/components/ui/separator";
import { toast } from "@/components/ui/toast";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import ErrorAlert from "@/components/alerts/error-alert";
import { updateRepository, type CommitSigner, type Repository } from "@/lib/repositories";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/repositories/$id/settings/signing")({
	component: RepositorySigningPage,
	head: () => ({
		meta: [{ title: `${m.pageRepositories()} - ${m.editRepositorySigningShort()}` }],
	}),
});

const MAX_SIGNING_KEYS_LENGTH = 64 * 1024;

const signingSchema = z
	.object({
		requireSignedCommits: z.boolean(),
		commitSigningKeys: z.string().max(MAX_SIGNING_KEYS_LENGTH, m.validationSigningKeysMaxLength()),
	})
	.superRefine((val, ctx) => {
		if (val.requireSignedCommits && val.commitSigningKeys.trim() === "") {
			ctx.addIssue({
				code: "custom",
				message: m.validationSigningKeysRequired(),
				path: ["commitSigningKeys"],
			});
		}
	});

function RepositorySigningPage() {
	const { id } = Route.useParams();
	const { data: repository, mutate } = useFetch<Repository>(`/repositories/${id}`);

	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | undefined>();
	const supported = repository?.commitSigningSupported ?? false;

	const form = useForm({
		defaultValues: {
			requireSignedCommits: repository?.requireSignedCommits ?? false,
			commitSigningKeys: repository?.commitSigningKeys ?? "",
		},
		validators: { onSubmit: signingSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			setError(undefined);
			try {
				const updated = await updateRepository(id, {
					requireSignedCommits: value.requireSignedCommits,
					commitSigningKeys: value.commitSigningKeys,
				});
				await mutate(updated, { revalidate: false });
				form.reset({
					requireSignedCommits: updated.requireSignedCommits,
					commitSigningKeys: updated.commitSigningKeys,
				});
				toast.add({ type: "success", title: m.repositorySigningUpdated() });
			} catch (err) {
				setError(err instanceof Error ? err.message : m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<div className="space-y-6">
			<Card>
				<CardHeader>
					<CardTitle>{m.editRepositorySigning()}</CardTitle>
					<CardDescription>{m.editRepositorySigningDescription()}</CardDescription>
				</CardHeader>
				<Separator />
				<CardContent>
					<form
						onSubmit={async (e) => {
							e.preventDefault();
							await form.handleSubmit();
						}}
					>
						<FieldGroup className="max-w-xl">
							{repository && !supported && (
								<ErrorAlert description={m.commitSigningUnsupportedProvider()} />
							)}

							<form.Field name="requireSignedCommits">
								{(field) => (
									<div className="flex items-center justify-between gap-4">
										<div className="flex items-center gap-1.5">
											<Label htmlFor={field.name} className="cursor-pointer">
												{m.requireSignedCommits()}
											</Label>
											<Tooltip>
												<TooltipTrigger
													render={
														<InfoIcon className="size-3.5 text-muted-foreground cursor-help" />
													}
												/>
												<TooltipContent>{m.requireSignedCommitsTooltip()}</TooltipContent>
											</Tooltip>
										</div>
										<Switch
											id={field.name}
											checked={field.state.value}
											onCheckedChange={field.handleChange}
											disabled={!supported}
										/>
									</div>
								)}
							</form.Field>

							<form.Field name="commitSigningKeys">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.trustedSigningKeys()}</Label>
											<Textarea
												id={field.name}
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => {
													setError(undefined);
													field.handleChange(e.target.value);
												}}
												placeholder={m.trustedSigningKeysPlaceholder()}
												className="min-h-40 font-mono text-xs"
												spellCheck={false}
												disabled={!supported}
											/>
											<p className="text-muted-foreground text-xs">{m.trustedSigningKeysHint()}</p>
											{repository?.provider === "gitlab" && (
												<p className="text-muted-foreground text-xs">
													{m.trustedSigningKeysGitLabHint()}
												</p>
											)}
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							</form.Field>

							{repository && repository.commitSigners.length > 0 && (
								<TrustedSignersList signers={repository.commitSigners} />
							)}

							{error && <ErrorAlert description={error} />}
							<form.Subscribe selector={(state) => state.isDefaultValue}>
								{(unchanged) => (
									<div className="flex flex-wrap gap-2 pt-2">
										<Button type="submit" disabled={isLoading || unchanged || !supported}>
											{m.updateRepository()}
										</Button>
										<Button
											type="button"
											variant="outline"
											onClick={() => {
												setError(undefined);
												form.reset();
											}}
											disabled={isLoading}
										>
											{m.cancel()}
										</Button>
									</div>
								)}
							</form.Subscribe>
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}

function TrustedSignersList({ signers }: { signers: CommitSigner[] }) {
	return (
		<Field>
			<Label>{m.configuredSigningKeys()}</Label>
			<ul className="divide-y rounded-md border">
				{signers.map((signer) => (
					<li key={signer.fingerprint} className="flex items-start gap-3 p-3">
						<Badge variant="outline" className="uppercase">
							{signer.type}
						</Badge>
						<div className="min-w-0 space-y-0.5">
							{signer.identity && <p className="truncate text-sm">{signer.identity}</p>}
							<p className="break-all font-mono text-xs text-muted-foreground">
								{signer.fingerprint}
							</p>
						</div>
					</li>
				))}
			</ul>
		</Field>
	);
}
