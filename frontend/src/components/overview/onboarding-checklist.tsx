import { Link } from "@tanstack/react-router";
import { ArrowRight, Check } from "lucide-react";
import type { ReactNode } from "react";
import UpsertApplicationDialog from "@/components/dialogs/upsert-application";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
	Item,
	ItemActions,
	ItemContent,
	ItemDescription,
	ItemGroup,
	ItemMedia,
	ItemTitle,
} from "@/components/ui/item";
import type { Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import type { Notification } from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import type { Repository } from "@/lib/repositories";
import { cn } from "@/lib/utils";

interface OnboardingStep {
	key: string;
	title: string;
	description: string;
	done: boolean | undefined;
	action: ReactNode;
}

// Shown instead of the dashboard until the first application exists.
export function OnboardingChecklist({ agents }: { agents: Agent[] | undefined }) {
	const { data: repositories } = useFetch<Repository[]>("/repositories");
	const { data: notifications } = useFetch<Notification[]>("/notifications");

	const steps: OnboardingStep[] = [
		{
			key: "agent",
			title: m.overviewOnboardingAgentTitle(),
			description: m.overviewOnboardingAgentDescription(),
			done: agents && agents.length > 0,
			action: <StepLink to="/agents" label={m.navAgents()} />,
		},
		{
			key: "repository",
			title: m.overviewOnboardingRepositoryTitle(),
			description: m.overviewOnboardingRepositoryDescription(),
			done: repositories && repositories.length > 0,
			action: <StepLink to="/repositories" label={m.navRepositories()} />,
		},
		{
			key: "application",
			title: m.overviewOnboardingApplicationTitle(),
			description: m.overviewOnboardingApplicationDescription(),
			done: false,
			action: <UpsertApplicationDialog application={null} />,
		},
		{
			key: "notification",
			title: m.overviewOnboardingNotificationTitle(),
			description: m.overviewOnboardingNotificationDescription(),
			done: notifications && notifications.length > 0,
			action: <StepLink to="/notifications" label={m.navNotifications()} />,
		},
	];
	const nextStep = steps.find((step) => step.done === false);

	return (
		<Card>
			<CardHeader>
				<CardTitle>{m.overviewOnboardingTitle()}</CardTitle>
				<CardDescription>{m.overviewOnboardingDescription()}</CardDescription>
			</CardHeader>
			<CardContent>
				<ItemGroup className="gap-2">
					{steps.map((step, index) => {
						const isNext = step === nextStep;
						return (
							<Item key={step.key} variant={isNext ? "outline" : "default"}>
								<ItemMedia
									className={cn(
										"size-7 rounded-full border text-xs font-medium",
										step.done
											? "border-emerald-500/40 bg-emerald-500/10 text-emerald-400"
											: isNext
												? "border-primary text-primary"
												: "text-muted-foreground",
									)}
								>
									{step.done ? <Check className="size-3.5" /> : index + 1}
								</ItemMedia>
								<ItemContent>
									<ItemTitle className={cn(step.done && "text-muted-foreground line-through")}>
										{step.title}
									</ItemTitle>
									<ItemDescription>{step.description}</ItemDescription>
								</ItemContent>
								{!step.done ? <ItemActions>{step.action}</ItemActions> : null}
							</Item>
						);
					})}
				</ItemGroup>
			</CardContent>
		</Card>
	);
}

function StepLink({
	to,
	label,
}: {
	to: "/agents" | "/repositories" | "/notifications";
	label: string;
}) {
	return (
		<Button variant="outline" size="sm" nativeButton={false} render={<Link to={to} />}>
			{label}
			<ArrowRight />
		</Button>
	);
}
