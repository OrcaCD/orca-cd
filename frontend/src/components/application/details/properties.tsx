import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export function Properties() {
	return (
		<Tabs defaultValue="manifest" className="space-y-4">
			<TabsList className="bg-muted">
				<TabsTrigger value="manifest">Manifest</TabsTrigger>
			</TabsList>

			<TabsContent value="manifest" className="space-y-4">
				<div className="bg-card border border-border rounded-lg p-4">
					<pre className="text-sm font-mono text-muted-foreground overflow-x-auto">
						{`version: "3.8"
services:
  api-gateway:
    image: org/api-gateway:v2.1.0
    ports:
      - "8080:80"
    environment:
      - NODE_ENV=production
    depends_on:
      - redis-cache

  redis-cache:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

  nginx-proxy:
    image: nginx:alpine
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf

volumes:
  redis-data:`}
					</pre>
				</div>
			</TabsContent>
		</Tabs>
	);
}
