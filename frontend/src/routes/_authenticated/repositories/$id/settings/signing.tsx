// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { InfoIcon, Trash2 } from "lucide-react";
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
import { InfoTooltip, InfoTooltipContent, InfoTooltipTrigger } from "@/components/info-tooltip";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import ErrorAlert from "@/components/alerts/error-alert";
import { updateRepository, type CommitSigningKey, type Repository } from "@/lib/repositories";
import { useFetch } from "@/lib/api";
import { m } from "@/lib/paraglide/messages";

export const Route = createFileRoute("/_authenticated/repositories/$id/settings/signing")({
	component: RepositorySigningPage,
	head: () => ({
		meta: [{ title: `${m.pageRepositories()} - ${m.editRepositorySigningShort()}` }],
	}),
});

const MAX_SIGNING_KEY_LENGTH = 32 * 1024;
const MAX_SIGNING_KEYS = 50;

const signingSchema = z
	.object({
		requireSignedCommits: z.boolean(),
		commitSigningKeys: z
			.array(z.string())
			.max(MAX_SIGNING_KEYS, m.validationSigningKeysMaxCount({ max: MAX_SIGNING_KEYS })),
	})
	.superRefine((val, ctx) => {
		if (val.requireSignedCommits && val.commitSigningKeys.length === 0) {
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
	const [newKey, setNewKey] = useState("");
	const [newKeyError, setNewKeyError] = useState<string | undefined>();
	const supported = repository?.commitSigningSupported ?? false;
	const savedKeys = new Map(repository?.commitSigningKeys?.map((key) => [key.key, key]));

	const form = useForm({
		defaultValues: {
			requireSignedCommits: repository?.requireSignedCommits ?? false,
			commitSigningKeys: repository?.commitSigningKeys?.map((key) => key.key) ?? [],
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
					commitSigningKeys: updated.commitSigningKeys?.map((key) => key.key) ?? [],
				});
				toast.add({ type: "success", title: m.repositorySigningUpdated() });
			} catch (err) {
				setError(err instanceof Error ? err.message : m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	const resetNewKey = () => {
		setNewKey("");
		setNewKeyError(undefined);
	};

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
											<InfoTooltip>
												<InfoTooltipTrigger
													type="button"
													aria-label={m.details()}
													className="inline-flex min-h-6 min-w-6 items-center justify-center cursor-help"
												>
													<InfoIcon
														aria-hidden="true"
														className="size-3.5 text-muted-foreground cursor-help"
													/>
												</InfoTooltipTrigger>
												<InfoTooltipContent>{m.requireSignedCommitsTooltip()}</InfoTooltipContent>
											</InfoTooltip>
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

							<form.Subscribe selector={(state) => state.values.requireSignedCommits}>
								{(enabled) => (
									<form.Field name="commitSigningKeys" mode="array">
										{(field) => {
											const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
											const editable = supported && enabled;
											const addKey = () => {
												const key = newKey.trim();
												if (key.length > MAX_SIGNING_KEY_LENGTH) {
													setNewKeyError(m.validationSigningKeyMaxLength());
													return;
												}
												if (field.state.value.includes(key)) {
													setNewKeyError(m.validationSigningKeyDuplicate());
													return;
												}
												setError(undefined);
												field.pushValue(key);
												resetNewKey();
											};

											return (
												<>
													<Field data-invalid={isInvalid}>
														<Label>{m.trustedSigningKeys()}</Label>
														<SigningKeyList
															keys={field.state.value}
															savedKeys={savedKeys}
															disabled={!editable}
															onRemove={(index) => {
																setError(undefined);
																field.removeValue(index);
															}}
														/>
														{supported && !enabled && (
															<p className="text-muted-foreground text-xs">
																{m.signingKeysDisabledHint()}
															</p>
														)}
														{isInvalid && <FieldError errors={field.state.meta.errors} />}
													</Field>

													<Field data-invalid={!!newKeyError}>
														<Label htmlFor="newSigningKey">{m.newSigningKey()}</Label>
														<Textarea
															id="newSigningKey"
															value={newKey}
															onChange={(e) => {
																setNewKeyError(undefined);
																setNewKey(e.target.value);
															}}
															placeholder={m.trustedSigningKeysPlaceholder()}
															className="min-h-28 font-mono text-xs"
															spellCheck={false}
															disabled={!editable}
														/>
														<p className="text-muted-foreground text-xs">
															{m.trustedSigningKeysHint()}
														</p>
														{repository?.provider === "gitlab" && (
															<p className="text-muted-foreground text-xs">
																{m.trustedSigningKeysGitLabHint()}
															</p>
														)}
														{newKeyError && <FieldError errors={[{ message: newKeyError }]} />}
														<div>
															<Button
																type="button"
																variant="outline"
																onClick={addKey}
																disabled={!editable || newKey.trim() === ""}
															>
																{m.addSigningKey()}
															</Button>
														</div>
													</Field>
												</>
											);
										}}
									</form.Field>
								)}
							</form.Subscribe>

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
												resetNewKey();
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

function SigningKeyList({
	keys,
	savedKeys,
	disabled,
	onRemove,
}: {
	keys: string[];
	savedKeys: Map<string, CommitSigningKey>;
	disabled: boolean;
	onRemove: (index: number) => void;
}) {
	if (keys.length === 0) {
		return <p className="text-muted-foreground text-sm">{m.noSigningKeys()}</p>;
	}

	return (
		<ul className="divide-y rounded-md border">
			{keys.map((key, index) => {
				const saved = savedKeys.get(key);
				return (
					<li
						key={key}
						className="flex items-start gap-3 p-3 data-disabled:opacity-60"
						data-disabled={disabled || undefined}
					>
						<Badge variant="outline" className="uppercase">
							{saved?.type ?? (key.startsWith("-----BEGIN PGP") ? "pgp" : "ssh")}
						</Badge>
						<div className="min-w-0 flex-1 space-y-0.5">
							{saved ? (
								<>
									{saved.identity && <p className="truncate text-sm">{saved.identity}</p>}
									<p className="break-all font-mono text-xs text-muted-foreground">
										{saved.fingerprint}
									</p>
								</>
							) : (
								<>
									<Badge variant="secondary">{m.signingKeyNotSaved()}</Badge>
									<p className="truncate font-mono text-xs text-muted-foreground">{key}</p>
								</>
							)}
						</div>
						<Button
							type="button"
							variant="ghost"
							size="icon-sm"
							aria-label={m.removeSigningKey()}
							onClick={() => onRemove(index)}
							disabled={disabled}
						>
							<Trash2 />
						</Button>
					</li>
				);
			})}
		</ul>
	);
}
