import type { ReactNode } from "react";
import { FileTextIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { m } from "@/lib/paraglide/messages";

export function AuthLayout({ children }: { children: ReactNode }) {
	return (
		<div className="grid min-h-svh bg-background lg:grid-cols-2">
			<div className="depth-canvas flex flex-col gap-4 p-6 md:p-10">
				<div className="flex justify-center md:justify-start">
					<div className="flex items-center gap-2.5 text-lg font-semibold tracking-tight">
						<img src="/assets/logo-dark.svg" alt={m.appLogoAlt()} className="size-8" />
						OrcaCD
					</div>
				</div>
				<div className="flex flex-1 items-center justify-center py-8">
					<div className="w-full max-w-[25rem]">{children}</div>
				</div>
				<div className="flex items-center justify-center gap-1 md:justify-start">
					<Button
						variant="ghost"
						size="icon-lg"
						aria-label={m.githubLogoAlt()}
						nativeButton={false}
						render={
							<a
								href="https://github.com/OrcaCD/orca-cd"
								target="_blank"
								rel="noopener noreferrer nofollow"
							/>
						}
					>
						<img
							src="/assets/icons/repositories/github.svg"
							alt=""
							className="size-5 opacity-70 dark:invert"
						/>
					</Button>
					<Button
						variant="ghost"
						size="icon-lg"
						className="text-muted-foreground"
						aria-label={m.navDocumentation()}
						nativeButton={false}
						render={
							<a href="https://orcacd.dev" target="_blank" rel="noopener noreferrer nofollow" />
						}
					>
						<FileTextIcon />
					</Button>
				</div>
			</div>
			<div className="relative hidden p-3 lg:block">
				<div className="relative h-full overflow-hidden rounded-2xl bg-muted ring-1 ring-foreground/10">
					<img
						src="/assets/wallpaper/lachlan-gowen-lleHA3cpZXo-unsplash.webp"
						alt={m.loginWallpaperAlt()}
						title={m.loginWallpaperAlt()}
						className="absolute inset-0 size-full object-cover"
					/>
					<div
						aria-hidden="true"
						className="absolute inset-0 bg-linear-to-t from-[oklch(0.14_0.02_248)] via-[oklch(0.14_0.02_248/35%)] to-transparent"
					/>
					<div className="absolute inset-x-0 bottom-0 p-10 text-[oklch(0.96_0.008_245)] xl:p-14">
						<p className="max-w-lg text-4xl leading-tight font-medium tracking-tight whitespace-pre-line xl:text-5xl">
							{m.authPanelDescription()}
						</p>
					</div>
				</div>
			</div>
		</div>
	);
}

/** Height of the inputs and buttons of the auth forms: roomier for touch, app sized from `sm` on. */
export const authControlClass = "h-10 sm:h-9";

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
