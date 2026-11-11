import { useState } from "react";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { m } from "@/lib/paraglide/messages";
import { dayInSeconds, hourInSeconds } from "@/lib/release-age";

type Unit = "hours" | "days";

const unitSeconds: Record<Unit, number> = { hours: hourInSeconds, days: dayInSeconds };

// ReleaseAgeInput edits an age in seconds as a number of hours or days.
export function ReleaseAgeInput({
	id,
	seconds,
	onChange,
	onBlur,
}: {
	id: string;
	seconds: number;
	onChange: (seconds: number) => void;
	onBlur?: () => void;
}) {
	// Whole days are shown as days, anything else as hours.
	const [unit, setUnit] = useState<Unit>(() => (seconds % dayInSeconds === 0 ? "days" : "hours"));
	const amount = Math.round(seconds / unitSeconds[unit]);

	return (
		<div className="flex gap-2">
			<Input
				id={id}
				type="number"
				min={0}
				className="w-28"
				value={amount}
				onBlur={onBlur}
				onChange={(e) => onChange(Number(e.target.value) * unitSeconds[unit])}
			/>
			<Select
				items={[
					{ value: "hours", label: m.releaseAgeUnitHours() },
					{ value: "days", label: m.releaseAgeUnitDays() },
				]}
				value={unit}
				onValueChange={(value: Unit | null) => {
					if (value) {
						setUnit(value);
						onChange(amount * unitSeconds[value]);
					}
				}}
			>
				<SelectTrigger className="w-32" aria-label={m.releaseAgeUnit()}>
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="hours">{m.releaseAgeUnitHours()}</SelectItem>
					<SelectItem value="days">{m.releaseAgeUnitDays()}</SelectItem>
				</SelectContent>
			</Select>
		</div>
	);
}
