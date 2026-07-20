// oxlint-disable react/no-children-prop
import { createFileRoute } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { toast } from "sonner";
import { z } from "zod";
import { Field, FieldError, FieldGroup } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { updateRepository, testRepositoryConnection, type Repository } from "@/lib/repositories";
import ErrorAlert from "@/components/alerts/error-alert";
import { m } from "@/lib/paraglide/messages";
import { useFetch } from "@/lib/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";

export const Route = createFileRoute("/_authenticated/repositories/$id/settings/auth")({
	component: RepositoryAuthPage,
	head: () => ({
		meta: [{ title: `${m.pageRepositories()} - ${m.editRepositoryAuthShort()}` }],
	}),
});

const authSchema = z.object({
	authToken: z.string().trim().max(1024, m.validationAuthTokenMaxLength()),
});

function RepositoryAuthPage() {
	const { id } = Route.useParams();
	const { data: repository } = useFetch<Repository>(`/repositories/${id}`);

	const [isLoading, setIsLoading] = useState(false);
	const [error, setError] = useState<string | undefined>();

	const form = useForm({
		defaultValues: {
			authToken: "",
		},
		validators: { onSubmit: authSchema },
		onSubmit: async ({ value }) => {
			setIsLoading(true);
			try {
				const authToken = value.authToken.trim() || undefined;
				const authMethod = authToken ? "token" : "none";
				try {
					await testRepositoryConnection({
						url: repository!.url,
						provider: repository!.provider,
						authMethod,
						authToken,
					});
				} catch (err: any) {
					setError(err?.message || m.failedConnectRepository());
					return;
				}
				await updateRepository(id, { authMethod, authToken });
				toast.success(m.repositoryAuthUpdated());
				form.reset();
			} catch {
				toast.error(m.failedUpdateRepository());
			} finally {
				setIsLoading(false);
			}
		},
	});

	return (
		<div className="space-y-6">
			<Card>
				<CardHeader>
					<CardTitle>{m.editRepositoryAuth()}</CardTitle>
					<CardDescription>{m.editRepositoryAuthDescription()}</CardDescription>
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
							<form.Field name="authToken">
								{(field) => {
									const isInvalid = field.state.meta.isTouched && !field.state.meta.isValid;
									return (
										<Field data-invalid={isInvalid}>
											<Label htmlFor={field.name}>{m.authToken()}</Label>
											<Input
												id={field.name}
												type="password"
												value={field.state.value}
												onBlur={field.handleBlur}
												onChange={(e) => {
													setError(undefined);
													field.handleChange(e.target.value);
												}}
												placeholder={m.authTokenPlaceholder()}
											/>
											<p className="text-muted-foreground text-xs">{m.authTokenLeaveBlankHint()}</p>
											{isInvalid && <FieldError errors={field.state.meta.errors} />}
										</Field>
									);
								}}
							</form.Field>
							{error && <ErrorAlert title={m.cantConnectToRepository()} description={error} />}
							<div className="flex flex-wrap gap-2 pt-2">
								<Button type="submit" disabled={isLoading || !repository}>
									{m.updateAuthentication()}
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
						</FieldGroup>
					</form>
				</CardContent>
			</Card>
		</div>
	);
}
