import type { ComponentProps } from "react";
import { Eye, EyeOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { m } from "@/lib/paraglide/messages";

export function PasswordInput({
	showPassword,
	onToggle,
	...props
}: ComponentProps<typeof Input> & {
	showPassword: boolean;
	onToggle: () => void;
}) {
	return (
		<div className="relative">
			<Input {...props} type={showPassword ? "text" : "password"} required />
			<Button
				className="absolute top-0 right-0 h-full px-3 hover:bg-transparent"
				onClick={onToggle}
				size="icon"
				type="button"
				variant="ghost"
				aria-label={showPassword ? m.hidePassword() : m.showPassword()}
				aria-pressed={showPassword}
			>
				{showPassword ? (
					<EyeOff className="h-4 w-4 text-muted-foreground" />
				) : (
					<Eye className="h-4 w-4 text-muted-foreground" />
				)}
			</Button>
		</div>
	);
}
