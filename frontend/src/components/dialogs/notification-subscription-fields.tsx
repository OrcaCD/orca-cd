import { z } from "zod";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Combobox,
	ComboboxChip,
	ComboboxChips,
	ComboboxChipsInput,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxItem,
	ComboboxList,
	ComboboxValue,
	useComboboxAnchor,
} from "@/components/ui/combobox";
import { Field, FieldDescription, FieldError } from "@/components/ui/field";
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { Agent } from "@/lib/agents";
import { useFetch } from "@/lib/api";
import type { ApplicationListItem } from "@/lib/applications";
import {
	getNotificationResourceEvents,
	type NotificationEvent,
	notificationEvents,
	type NotificationResource,
	notificationResources,
	type NotificationSubscription,
} from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";
import type { Repository } from "@/lib/repositories";

export const notificationSubscriptionSchema = z.object({
	events: z.array(z.enum(notificationEvents)).min(1, m.validationNotificationEventsRequired()),
	allApplications: z.boolean(),
	applicationIds: z.array(z.string()),
	allAgents: z.boolean(),
	agentIds: z.array(z.string()),
	allRepositories: z.boolean(),
	repositoryIds: z.array(z.string()),
});

const notificationEventLabels: Record<NotificationEvent, () => string> = {
	"application.deployment.succeeded": () => m.notificationEventDeploymentSucceeded(),
	"application.deployment.failed": () => m.notificationEventDeploymentFailed(),
	"application.image_update.succeeded": () => m.notificationEventImageUpdateSucceeded(),
	"application.image_update.failed": () => m.notificationEventImageUpdateFailed(),
	"application.sync.failed": () => m.notificationEventSyncFailed(),
	"application.health.unhealthy": () => m.notificationEventHealthUnhealthy(),
	"application.health.recovered": () => m.notificationEventHealthRecovered(),
	"agent.offline": () => m.notificationEventAgentOffline(),
	"agent.online": () => m.notificationEventAgentOnline(),
	"repository.sync.failed": () => m.notificationEventRepositorySyncFailed(),
	"repository.sync.recovered": () => m.notificationEventRepositorySyncRecovered(),
};

interface ResourceConfig {
	allKey: "allApplications" | "allAgents" | "allRepositories";
	idsKey: "applicationIds" | "agentIds" | "repositoryIds";
	title: () => string;
	all: () => string;
	allDescription: () => string;
	select: () => string;
	empty: () => string;
}

const resourceConfigs: Record<NotificationResource, ResourceConfig> = {
	applications: {
		allKey: "allApplications",
		idsKey: "applicationIds",
		title: () => m.navApplications(),
		all: () => m.allApplications(),
		allDescription: () => m.allApplicationsDescription(),
		select: () => m.selectApplications(),
		empty: () => m.noApplicationsAvailable(),
	},
	agents: {
		allKey: "allAgents",
		idsKey: "agentIds",
		title: () => m.navAgents(),
		all: () => m.allAgents(),
		allDescription: () => m.allAgentsDescription(),
		select: () => m.selectAgents(),
		empty: () => m.noAgentsAvailable(),
	},
	repositories: {
		allKey: "allRepositories",
		idsKey: "repositoryIds",
		title: () => m.navRepositories(),
		all: () => m.allRepositories(),
		allDescription: () => m.allRepositoriesDescription(),
		select: () => m.selectRepositories(),
		empty: () => m.noRepositoriesAvailable(),
	},
};

interface ResourceOption {
	id: string;
	name: string;
	description?: string;
}

export function NotificationSubscriptionFields({
	value,
	onChange,
	errors,
	modal,
}: {
	value: NotificationSubscription;
	onChange: (value: NotificationSubscription) => void;
	errors?: Array<{ message?: string } | undefined>;
	modal?: boolean;
}) {
	const isInvalid = (errors?.length ?? 0) > 0;

	const { data: applications } = useFetch<ApplicationListItem[]>("/applications");
	const { data: agents } = useFetch<Agent[]>("/agents");
	const { data: repositories } = useFetch<Repository[]>("/repositories");

	const options: Record<NotificationResource, ResourceOption[] | undefined> = {
		applications: applications?.map((app) => ({
			id: app.id,
			name: app.name,
			description: `${app.repositoryName} / ${app.agentName}`,
		})),
		agents: agents?.map((agent) => ({ id: agent.id, name: agent.name })),
		repositories: repositories?.map((repo) => ({
			id: repo.id,
			name: repo.name,
			description: repo.url,
		})),
	};

	return (
		<Field data-invalid={isInvalid}>
			<Label>{m.notificationEvents()}</Label>
			<FieldDescription>{m.notificationEventsDescription()}</FieldDescription>
			<Tabs defaultValue="applications">
				<TabsList className="w-full">
					{notificationResources.map((resource) => {
						const selected = getNotificationResourceEvents(resource).filter((event) =>
							value.events.includes(event),
						).length;
						return (
							<TabsTrigger key={resource} value={resource}>
								{resourceConfigs[resource].title()}
								<Badge variant={selected > 0 ? "secondary" : "outline"}>{selected}</Badge>
							</TabsTrigger>
						);
					})}
				</TabsList>
				{notificationResources.map((resource) => (
					<TabsContent key={resource} value={resource} className="grid gap-3 pt-1">
						<ResourceSubscriptionFields
							resource={resource}
							value={value}
							onChange={onChange}
							options={options[resource]}
							modal={modal}
						/>
					</TabsContent>
				))}
			</Tabs>
			{isInvalid && <FieldError errors={errors} />}
		</Field>
	);
}

function ResourceSubscriptionFields({
	resource,
	value,
	onChange,
	options,
	modal,
}: {
	resource: NotificationResource;
	value: NotificationSubscription;
	onChange: (value: NotificationSubscription) => void;
	options: ResourceOption[] | undefined;
	modal?: boolean;
}) {
	const config = resourceConfigs[resource];

	const toggleEvent = (event: NotificationEvent, checked: boolean) => {
		// Keep the catalog order so the payload is stable regardless of click order.
		onChange({
			...value,
			events: notificationEvents.filter((e) => (e === event ? checked : value.events.includes(e))),
		});
	};

	return (
		<>
			<div className="grid gap-3 rounded-md border p-3">
				{getNotificationResourceEvents(resource).map((event) => (
					<div key={event} className="flex items-center gap-2">
						<Checkbox
							id={`notification-event-${event}`}
							checked={value.events.includes(event)}
							onCheckedChange={(checked) => toggleEvent(event, checked === true)}
						/>
						<Label htmlFor={`notification-event-${event}`} className="font-normal">
							{notificationEventLabels[event]()}
						</Label>
					</div>
				))}
			</div>

			<div className="flex items-center justify-between gap-4 rounded-md border p-3">
				<div>
					<p className="text-sm font-medium">{config.all()}</p>
					<p className="text-xs text-muted-foreground">{config.allDescription()}</p>
				</div>
				<Switch
					checked={value[config.allKey]}
					onCheckedChange={(checked) => onChange({ ...value, [config.allKey]: checked })}
				/>
			</div>

			{!value[config.allKey] && (
				<ResourceSelectField
					label={config.title()}
					placeholder={config.select()}
					empty={config.empty()}
					value={value[config.idsKey]}
					onChange={(ids) => onChange({ ...value, [config.idsKey]: ids })}
					options={options}
					modal={modal}
				/>
			)}
		</>
	);
}

function ResourceSelectField({
	label,
	placeholder,
	empty,
	value,
	onChange,
	options,
	modal,
}: {
	label: string;
	placeholder: string;
	empty: string;
	value: string[];
	onChange: (ids: string[]) => void;
	options: ResourceOption[] | undefined;
	modal?: boolean;
}) {
	const anchor = useComboboxAnchor();

	return (
		<Field>
			<Label>{label}</Label>
			<Combobox
				items={options}
				multiple
				autoHighlight
				value={value}
				onValueChange={onChange}
				modal={modal}
			>
				<ComboboxChips ref={anchor}>
					<ComboboxValue>
						{(values) => (
							<>
								{values.map((id: string) => (
									<ComboboxChip key={id}>
										{options?.find((option) => option.id === id)?.name ?? id}
									</ComboboxChip>
								))}
								<ComboboxChipsInput placeholder={value.length === 0 ? placeholder : ""} />
							</>
						)}
					</ComboboxValue>
				</ComboboxChips>
				<ComboboxContent anchor={anchor}>
					<ComboboxEmpty>{empty}</ComboboxEmpty>
					<ComboboxList>
						{(item: ResourceOption) => (
							<ComboboxItem key={item.id} value={item.id}>
								<Item className="p-0">
									<ItemContent>
										<ItemTitle className="whitespace-nowrap">{item.name}</ItemTitle>
										{item.description && <ItemDescription>{item.description}</ItemDescription>}
									</ItemContent>
								</Item>
							</ComboboxItem>
						)}
					</ComboboxList>
				</ComboboxContent>
			</Combobox>
		</Field>
	);
}
