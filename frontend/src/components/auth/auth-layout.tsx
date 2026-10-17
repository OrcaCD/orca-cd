import type { ReactNode } from "react";
import { FileTextIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { m } from "@/lib/paraglide/messages";

export function AuthLayout({ children }: { children: ReactNode }) {
	return (
		<div className="grid min-h-svh lg:grid-cols-2">
			<div className="flex flex-col gap-4 p-6 md:p-10">
				<div className="flex justify-center gap-2 md:justify-start">
					<div className="flex items-center gap-3 font-medium text-xl">
						<div className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
							<img src="/assets/logo-dark.svg" alt="OrcaCD Logo" />
						</div>
						OrcaCD
					</div>
				</div>
				<div className="flex flex-1 items-center justify-center">
					<div className="w-full max-w-xs">{children}</div>
				</div>
				<div className="flex items-center gap-1">
					<a
						href="https://github.com/OrcaCD/orca-cd"
						target="_blank"
						rel="noopener noreferrer nofollow"
					>
						<Button variant="ghost" size="icon-lg">
							<img
								src="/assets/icons/repositories/github.svg"
								alt={m.githubLogoAlt()}
								className="size-6 dark:invert"
							/>
						</Button>
					</a>
					<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer nofollow">
						<Button variant="ghost" size="icon-lg">
							<FileTextIcon className="size-6" />
						</Button>
					</a>
				</div>
			</div>
			<div className="relative hidden bg-muted lg:block">
				<img
					src="/assets/wallpaper/lachlan-gowen-lleHA3cpZXo-unsplash.jpg"
					alt={m.loginWallpaperAlt()}
					title={m.loginWallpaperAlt()}
					className="absolute inset-0 h-full w-full object-cover"
				/>
			</div>
		</div>
	);
}

export function AuthErrorBanner({ message }: { message: string }) {
	return (
		<div
			role="alert"
			className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive"
		>
			{message}
		</div>
	);
}
