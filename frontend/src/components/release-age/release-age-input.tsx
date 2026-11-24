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

// Whole days are shown as days, anything else as hours.
function unitFor(seconds: number): Unit {
	return seconds % dayInSeconds === 0 ? "days" : "hours";
}

// ReleaseAgeInput edits an age in seconds as a number of hours or days.
export function ReleaseAgeInput({
	id,
	seconds,
	onChange,
	onBlur,
	disabled,
}: {
	id: string;
	seconds: number;
	onChange: (seconds: number) => void;
	onBlur?: () => void;
	disabled?: boolean;
}) {
	// The unit picked for the age this input set. When the age changes from
	// outside, e.g. on Cancel or a change by someone else, the unit follows it.
	const [picked, setPicked] = useState<{ unit: Unit; seconds: number }>(() => ({
		unit: unitFor(seconds),
		seconds,
	}));
	const unit = picked.seconds === seconds ? picked.unit : unitFor(seconds);
	// Rounded only to hide floating point noise, so the box shows the age that is saved.
	const amount = Number((seconds / unitSeconds[unit]).toFixed(4));

	function change(value: number, newUnit: Unit) {
		const newSeconds = Math.round(value * unitSeconds[newUnit]);
		setPicked({ unit: newUnit, seconds: newSeconds });
		onChange(newSeconds);
	}

	return (
		<div className="flex gap-2">
			<Input
				id={id}
				type="number"
				min={0}
				step="any"
				className="w-28"
				value={amount}
				disabled={disabled}
				onBlur={onBlur}
				onChange={(e) => change(Number(e.target.value), unit)}
			/>
			<Select
				items={[
					{ value: "hours", label: m.releaseAgeUnitHours() },
					{ value: "days", label: m.releaseAgeUnitDays() },
				]}
				value={unit}
				disabled={disabled}
				onValueChange={(value: Unit | null) => {
					if (value) {
						change(amount, value);
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
