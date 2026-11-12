import { useEffect, useRef, useState } from "react";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { m } from "@/lib/paraglide/messages";

const WIDTH = 640;
const HEIGHT = 320;
const ORCA_X = 48;
const ORCA_SIZE = 40;
const ORCA_SPEED = 280;
const HIGH_SCORE_STORAGE_KEY = "orca-game-high-score";

type Status = "idle" | "running" | "over";

type Container = { x: number; y: number; w: number; h: number; color: string; passed: boolean };

function readHighScore(): number {
	try {
		return Number(window.localStorage.getItem(HIGH_SCORE_STORAGE_KEY)) || 0;
	} catch {
		return 0;
	}
}

function writeHighScore(score: number) {
	try {
		window.localStorage.setItem(HIGH_SCORE_STORAGE_KEY, String(score));
	} catch {
		// The high score is only a convenience, losing it is fine.
	}
}

function random(min: number, max: number) {
	return min + Math.random() * (max - min);
}

function OrcaGameCanvas({ ref }: { ref: React.Ref<HTMLDivElement> }) {
	const canvasRef = useRef<HTMLCanvasElement>(null);
	const startRef = useRef<(() => void) | null>(null);
	const keysRef = useRef({ up: false, down: false });
	const pointerYRef = useRef<number | null>(null);
	const [status, setStatus] = useState<Status>("idle");
	const [score, setScore] = useState(0);
	const [highScore, setHighScore] = useState(readHighScore);

	useEffect(() => {
		const canvas = canvasRef.current;
		const ctx = canvas?.getContext("2d");
		if (!canvas || !ctx) {
			return;
		}

		const dpr = window.devicePixelRatio || 1;
		canvas.width = WIDTH * dpr;
		canvas.height = HEIGHT * dpr;
		ctx.scale(dpr, dpr);

		const orca = new Image();
		orca.src = "/assets/logo-dark.svg";

		const clock = { frame: 0, last: null as number | null };
		let gameStatus: Status = "idle";
		let orcaY = (HEIGHT - ORCA_SIZE) / 2;
		let orcaVelocity = 0;
		let containers: Container[] = [];
		let speed = 0;
		let spawnIn = 0;
		let elapsed = 0;
		let points = 0;
		let best = readHighScore();
		let colors: string[] = [];

		startRef.current = () => {
			// Read the colors on every start so the game follows theme changes.
			const style = getComputedStyle(canvas);
			colors = ["--chart-2", "--chart-3", "--chart-4", "--primary"].map((name) =>
				style.getPropertyValue(name).trim(),
			);
			orcaY = (HEIGHT - ORCA_SIZE) / 2;
			containers = [];
			speed = 180;
			spawnIn = 0;
			elapsed = 0;
			points = 0;
			gameStatus = "running";
			setScore(0);
			setStatus("running");
		};

		const update = (dt: number) => {
			elapsed += dt;
			speed += 8 * dt;

			const { up, down } = keysRef.current;
			const pointerY = pointerYRef.current;
			let direction = Number(down) - Number(up);
			if (direction === 0 && pointerY !== null) {
				const delta = pointerY - (orcaY + ORCA_SIZE / 2);
				direction = Math.abs(delta) < 4 ? 0 : Math.sign(delta) * Math.min(1, Math.abs(delta) / 40);
			}
			orcaVelocity = direction * ORCA_SPEED;
			orcaY = Math.max(0, Math.min(HEIGHT - ORCA_SIZE, orcaY + orcaVelocity * dt));

			spawnIn -= dt;
			if (spawnIn <= 0) {
				const h = random(28, 48);
				containers.push({
					x: WIDTH,
					y: random(0, HEIGHT - h),
					w: random(60, 120),
					h,
					color: colors[Math.floor(Math.random() * colors.length)],
					passed: false,
				});
				spawnIn = Math.max(0.4, 1.1 - elapsed / 60);
			}

			// The logo has rounded corners and some padding, so the hitbox is smaller than the sprite.
			const inset = ORCA_SIZE * 0.18;
			const orcaTop = orcaY + inset;
			const orcaBottom = orcaY + ORCA_SIZE - inset;
			const orcaLeft = ORCA_X + inset;
			const orcaRight = ORCA_X + ORCA_SIZE - inset;

			for (const container of containers) {
				container.x -= speed * dt;
				if (!container.passed && container.x + container.w < orcaLeft) {
					container.passed = true;
					points++;
					setScore(points);
				}
				if (
					orcaRight > container.x &&
					orcaLeft < container.x + container.w &&
					orcaBottom > container.y &&
					orcaTop < container.y + container.h
				) {
					gameStatus = "over";
					setStatus("over");
					if (points > best) {
						best = points;
						writeHighScore(best);
						setHighScore(best);
					}
					break;
				}
			}
			containers = containers.filter((container) => container.x + container.w > 0);
		};

		const draw = () => {
			ctx.clearRect(0, 0, WIDTH, HEIGHT);

			for (const container of containers) {
				ctx.fillStyle = container.color;
				ctx.fillRect(container.x, container.y, container.w, container.h);
				// Corrugated walls make the boxes read as shipping containers.
				ctx.fillStyle = "rgb(0 0 0 / 0.15)";
				for (let x = container.x + 6; x < container.x + container.w - 4; x += 8) {
					ctx.fillRect(x, container.y + 3, 2, container.h - 6);
				}
			}

			if (orca.complete) {
				ctx.save();
				ctx.translate(ORCA_X + ORCA_SIZE / 2, orcaY + ORCA_SIZE / 2);
				ctx.rotate((orcaVelocity / ORCA_SPEED) * 0.25);
				ctx.drawImage(orca, -ORCA_SIZE / 2, -ORCA_SIZE / 2, ORCA_SIZE, ORCA_SIZE);
				ctx.restore();
			}
		};

		const loop = (time: number) => {
			const dt = clock.last === null ? 0 : Math.min(0.05, (time - clock.last) / 1000);
			clock.last = time;
			if (gameStatus === "running") {
				update(dt);
			}
			draw();
			clock.frame = requestAnimationFrame(loop);
		};
		clock.frame = requestAnimationFrame(loop);

		return () => cancelAnimationFrame(clock.frame);
	}, []);

	const handleKey = (event: React.KeyboardEvent, pressed: boolean) => {
		if (event.key === "ArrowUp" || event.key === "ArrowDown") {
			event.preventDefault();
			keysRef.current[event.key === "ArrowUp" ? "up" : "down"] = pressed;
		} else if (event.key === " " || event.key === "Enter") {
			event.preventDefault();
			if (pressed && status !== "running") {
				startRef.current?.();
			}
		}
	};

	const handlePointer = (event: React.PointerEvent<HTMLDivElement>) => {
		const rect = event.currentTarget.getBoundingClientRect();
		pointerYRef.current = ((event.clientY - rect.top) / rect.height) * HEIGHT;
	};

	return (
		<div
			ref={ref}
			tabIndex={0}
			className="relative aspect-2/1 w-full touch-none overflow-hidden rounded-lg bg-muted outline-none select-none focus-visible:ring-3 focus-visible:ring-ring/50"
			onKeyDown={(event) => handleKey(event, true)}
			onKeyUp={(event) => handleKey(event, false)}
			onPointerMove={handlePointer}
			onPointerDown={(event) => {
				handlePointer(event);
				if (status !== "running") {
					startRef.current?.();
				}
			}}
			onPointerLeave={() => {
				pointerYRef.current = null;
			}}
		>
			<canvas ref={canvasRef} className="size-full" />
			<div className="absolute top-2 right-3 text-xs font-medium text-muted-foreground tabular-nums">
				{m.orcaGameScore({ score })} · {m.orcaGameHighScore({ score: highScore })}
			</div>
			{status !== "running" && (
				<div className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-background/60 text-center">
					{status === "over" && <p className="text-base font-semibold">{m.orcaGameOver()}</p>}
					<p className="text-muted-foreground">
						{status === "over" ? m.orcaGameRestart() : m.orcaGameStart()}
					</p>
				</div>
			)}
		</div>
	);
}

export default function OrcaGame({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const gameRef = useRef<HTMLDivElement>(null);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-2xl" initialFocus={gameRef}>
				<DialogHeader>
					<DialogTitle>{m.orcaGameTitle()}</DialogTitle>
					<DialogDescription>{m.orcaGameDescription()}</DialogDescription>
				</DialogHeader>
				<OrcaGameCanvas ref={gameRef} />
			</DialogContent>
		</Dialog>
	);
}
