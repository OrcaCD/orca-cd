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
import type { ApplicationListItem } from "@/lib/applications";
import { type NotificationEvent, notificationEvents } from "@/lib/notifications";
import { m } from "@/lib/paraglide/messages";

const notificationEventLabels: Record<NotificationEvent, () => string> = {
	"application.deployment.succeeded": () => m.notificationEventDeploymentSucceeded(),
	"application.deployment.failed": () => m.notificationEventDeploymentFailed(),
	"application.image_update.succeeded": () => m.notificationEventImageUpdateSucceeded(),
	"application.image_update.failed": () => m.notificationEventImageUpdateFailed(),
	"application.sync.failed": () => m.notificationEventSyncFailed(),
};

export function NotificationEventsField({
	value,
	onChange,
	errors,
}: {
	value: NotificationEvent[];
	onChange: (events: NotificationEvent[]) => void;
	errors?: Array<{ message?: string } | undefined>;
}) {
	const isInvalid = (errors?.length ?? 0) > 0;

	const toggle = (event: NotificationEvent, checked: boolean) => {
		// Keep the catalog order so the payload is stable regardless of click order.
		onChange(notificationEvents.filter((e) => (e === event ? checked : value.includes(e))));
	};

	return (
		<Field data-invalid={isInvalid}>
			<Label>{m.notificationEvents()}</Label>
			<FieldDescription>{m.notificationEventsDescription()}</FieldDescription>
			<div className="grid gap-3 rounded-md border p-3">
				{notificationEvents.map((event) => (
					<div key={event} className="flex items-center gap-2">
						<Checkbox
							id={`notification-event-${event}`}
							checked={value.includes(event)}
							onCheckedChange={(checked) => toggle(event, checked === true)}
						/>
						<Label htmlFor={`notification-event-${event}`} className="font-normal">
							{notificationEventLabels[event]()}
						</Label>
					</div>
				))}
			</div>
			{isInvalid && <FieldError errors={errors} />}
		</Field>
	);
}

export function NotificationAllApplicationsField({
	value,
	onChange,
}: {
	value: boolean;
	onChange: (value: boolean) => void;
}) {
	return (
		<div className="flex items-center justify-between gap-4 rounded-md border p-3">
			<div>
				<p className="text-sm font-medium">{m.allApplications()}</p>
				<p className="text-xs text-muted-foreground">{m.allApplicationsDescription()}</p>
			</div>
			<Switch checked={value} onCheckedChange={onChange} />
		</div>
	);
}

export function NotificationApplicationsField({
	value,
	onChange,
	applications,
	modal,
}: {
	value: string[];
	onChange: (applicationIds: string[]) => void;
	applications: ApplicationListItem[] | undefined;
	modal?: boolean;
}) {
	const anchor = useComboboxAnchor();

	return (
		<Field>
			<Label>{m.navApplications()}</Label>
			<Combobox
				items={applications}
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
										{applications?.find((app) => app.id === id)?.name ?? id}
									</ComboboxChip>
								))}
								<ComboboxChipsInput
									placeholder={value.length === 0 ? m.selectApplications() : ""}
								/>
							</>
						)}
					</ComboboxValue>
				</ComboboxChips>
				<ComboboxContent anchor={anchor}>
					<ComboboxEmpty>{m.noApplicationsAvailable()}</ComboboxEmpty>
					<ComboboxList>
						{(item) => (
							<ComboboxItem key={item.id} value={item.id}>
								<Item className="p-0">
									<ItemContent>
										<ItemTitle className="whitespace-nowrap">{item.name}</ItemTitle>
										<ItemDescription>
											{item.repositoryName} / {item.agentName}
										</ItemDescription>
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
