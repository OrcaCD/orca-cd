import {
  deleteApplication,
  deployApplication,
  generateImageWebhook,
  revokeImageWebhook,
  HealthStatus,
  SyncStatus,
  type Application,
} from "@/lib/applications";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import {
  Clock,
  ExternalLink,
  GitBranch,
  GitCommit,
  Pencil,
  RefreshCw,
  Server,
  Trash2,
  Webhook,
} from "lucide-react";
import { ApplicationStatusBadge } from "@/components/badges/application-status-badge";
import { Button } from "@/components/ui/button";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useMemo, useState } from "react";
import { useTheme } from "@/components/theme-provider";
import { highlighter } from "@/lib/highlighter";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useFetch } from "@/lib/api";
import { toast } from "sonner";
import ConfirmationDialog from "@/components/dialogs/confirm-dialog";
import CopyValueDialog from "@/components/dialogs/copy-value-dialog";
import CopyButton from "@/components/copy-btn";
import { m } from "@/lib/paraglide/messages";
import { transformerNotationDiff, transformerRenderWhitespace } from "@shikijs/transformers";
import { diffArrays } from "diff";
import { StaticLucideIcon } from "@/components/lucide-icon-picker";
import { Separator } from "@/components/ui/separator";

export const Route = createFileRoute("/_authenticated/applications/$id/details/")({
  component: ApplicationDetailsPage,
  head: () => ({
    meta: [
      {
        title: `${m.pageApplications()} - ${m.details()}`,
      },
    ],
  }),
});

function InfoCard({
  icon,
  label,
  value,
  subValue,
  link,
  isMonoValue,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  subValue?: string;
  link?: string;
  isMonoValue?: boolean;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <div className="flex items-center gap-2 text-muted-foreground mb-2">
            {icon}
            <span className="text-sm">{label}</span>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="font-medium truncate">
          {link ? (
            <a
              href={link}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-primary flex items-center gap-1"
            >
              {value} <ExternalLink className="h-3 w-3" />
            </a>
          ) : (
            <span className={isMonoValue ? "font-mono" : ""}>{value}</span>
          )}
        </div>
        {subValue && <p className="text-sm text-muted-foreground mt-1 truncate">{subValue}</p>}
      </CardContent>
    </Card>
  );
}

function withDiffMarker(line: string, marker: "++" | "--"): string {
  if (line.length === 0) {
    return line;
  }

  return `${line} # [!code ${marker}]`;
}

function splitComposeLines(composeFile: string): string[] {
  if (composeFile.length === 0) {
    return [];
  }

  const lines = composeFile.split("\n");
  if (lines.at(-1) === "") {
    lines.pop();
  }

  return lines;
}

function buildComposeDiff(previousComposeFile: string, composeFile: string): string {
  const previousLines = splitComposeLines(previousComposeFile);
  const currentLines = splitComposeLines(composeFile);

  const lines: string[] = [];

  for (const change of diffArrays(previousLines, currentLines)) {
    if (change.added) {
      lines.push(...change.value.map((line) => withDiffMarker(line, "++")));
      continue;
    }

    if (change.removed) {
      lines.push(...change.value.map((line) => withDiffMarker(line, "--")));
      continue;
    }

    lines.push(...change.value);
  }

  return lines.join("\n");
}

function ApplicationDetailsPage() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const { theme } = useTheme();

  const { data } = useFetch<Application>("/applications/" + id);

  const [deploying, setDeploying] = useState(false);
  const [webhookSecret, setWebhookSecret] = useState<string | null>(null);
  const [webhookSecretOpen, setWebhookSecretOpen] = useState(false);

  const handleDeploy = async () => {
    setDeploying(true);
    try {
      await deployApplication(id);
      toast.success(m.deploymentStarted());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : m.failedDeployApplication());
    } finally {
      setDeploying(false);
    }
  };

  const deploymentInProgress = deploying || data?.syncStatus === SyncStatus.Syncing;

  const manifestHtml = useMemo(() => {
    return highlighter.codeToHtml(data?.composeFile ?? "", {
      lang: "yaml",
      theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
      transformers: [transformerRenderWhitespace()],
    });
  }, [data?.composeFile, theme]);

  const diffHtml = useMemo(() => {
    const composeDiff = buildComposeDiff(data?.previousComposeFile ?? "", data?.composeFile ?? "");
    return highlighter.codeToHtml(composeDiff, {
      lang: "yaml",
      theme: theme === "dark" ? "vitesse-dark" : "vitesse-light",
      transformers: [transformerNotationDiff(), transformerRenderWhitespace()],
    });
  }, [data?.previousComposeFile, data?.composeFile, theme]);

  async function handleGenerateWebhook() {
    try {
      const result = await generateImageWebhook(id);
      setWebhookSecret(result.secret);
      setWebhookSecretOpen(true);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : m.failedGenerateWebhook());
    }
  }

  async function handleRevokeWebhook() {
    try {
      await revokeImageWebhook(id);
      toast.success(m.webhookRevoked());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : m.failedRevokeWebhook());
    }
  }

  async function deleteApp() {
    try {
      await deleteApplication(id);
      toast.success(m.toastApplicationDeleted({ name: data?.name ?? "" }));
      await navigate({ to: "/applications" });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : m.toastDeleteApplicationFailed());
    }
  }
  return (
    <div className="p-6 space-y-6">
      <div className="flex flex-col lg:flex-row lg:items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <div className="h-14 w-14 rounded-xl bg-primary/10 flex items-center justify-center">
            <StaticLucideIcon name={data?.icon} className="h-7 w-7 text-primary" />
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold">{data?.name}</h1>
              <ApplicationStatusBadge status={data?.syncStatus ?? SyncStatus.Unknown} type="sync" />
              <ApplicationStatusBadge
                status={data?.healthStatus ?? HealthStatus.Unknown}
                type="health" />
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={handleDeploy} disabled={deploymentInProgress}>
            <RefreshCw className={`mr-2 h-4 w-4 ${deploymentInProgress ? "animate-spin" : ""}`} />
            {deploymentInProgress ? m.deploying() : m.deploy()}
          </Button>

          <Separator orientation="vertical" />

          <Button
            variant="outline"
            onClick={() => navigate({
              to: "/applications/$id/settings/general",
              params: { id: data!.id },
            })}
          >
            <Pencil />
            {m.edit()}
          </Button>

          <ConfirmationDialog
            onConfirm={async () => await deleteApp()}
            triggerProps={{ variant: "destructive" }}
            triggerText={<>
              <Trash2 />
              {m.delete()}
            </>}
            description={m.deleteApplicationDescription()}
          ></ConfirmationDialog>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <InfoCard
          icon={<GitBranch className="h-4 w-4" />}
          label={m.applicationInfoRepository()}
          value={data?.repositoryName ?? ""}
          subValue={data?.branch}
          link={data?.repositoryUrl} />
        <InfoCard
          icon={<GitCommit className="h-4 w-4" />}
          label={m.applicationInfoLatestCommit()}
          value={data?.commit?.slice(0, 7) ?? ""}
          subValue={data?.commitMessage}
          isMonoValue />
        <InfoCard
          icon={<Server className="h-4 w-4" />}
          label={m.applicationInfoTargetHost()}
          value={data?.agentName ?? ""}
          subValue={data?.path} />
        <InfoCard
          icon={<Clock className="h-4 w-4" />}
          label={m.applicationInfoLastSync()}
          value={data?.lastSyncedAt ? new Date(data.lastSyncedAt).toLocaleString() : m.never()}
          subValue={m.applicationInfoAutoSyncEnabled()} />
      </div>

      <Tabs defaultValue="manifest" className="space-y-4">
        <TabsList className="bg-muted">
          <TabsTrigger value="manifest">{m.manifest()}</TabsTrigger>
          <TabsTrigger value="diff">Diff</TabsTrigger>
          <TabsTrigger value="webhook">{m.webhook()}</TabsTrigger>
        </TabsList>

        <TabsContent value="manifest" className="space-y-4">
          <div className="dark:bg-[#121212] border border-border rounded-lg p-4">
            <div className="text-sm font-mono text-muted-foreground overflow-x-auto">
              <div dangerouslySetInnerHTML={{ __html: manifestHtml }} />
            </div>
          </div>
        </TabsContent>

        <TabsContent value="diff" className="space-y-4">
          <div className="dark:bg-[#121212] border border-border rounded-lg p-4">
            <div className="text-sm font-mono text-muted-foreground overflow-x-auto">
              <div dangerouslySetInnerHTML={{ __html: diffHtml }} />
            </div>
          </div>
        </TabsContent>

        <TabsContent value="webhook" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>
                <div className="flex items-center gap-2">
                  <Webhook className="h-5 w-5" />
                  {m.imagePullWebhookSectionTitle()}
                </div>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">{m.imagePullWebhookDescription()}</p>

              {data?.imageWebhookEnabled && data.imageWebhookUrl ? (
                <div className="space-y-1">
                  <p className="text-xs font-medium">{m.imagePullWebhookUrl()}</p>
                  <div className="flex items-center gap-1 rounded-md border bg-muted/50 px-3 py-1">
                    <code className="flex-1 truncate font-mono text-sm">
                      {data.imageWebhookUrl}
                    </code>
                    <CopyButton text={data.imageWebhookUrl} title={m.imagePullWebhookUrl()} />
                  </div>
                </div>
              ) : null}

              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleGenerateWebhook}>
                  {data?.imageWebhookEnabled ? m.regenerateWebhook() : m.generateWebhook()}
                </Button>
                {data?.imageWebhookEnabled && (
                  <ConfirmationDialog
                    onConfirm={handleRevokeWebhook}
                    description={m.revokeWebhookConfirmDescription()}
                    triggerText={m.revokeWebhook()}
                    triggerProps={{ variant: "destructive", size: "sm" }} />
                )}
              </div>
            </CardContent>
          </Card>

          <CopyValueDialog
            open={webhookSecretOpen}
            onOpenChange={(nextOpen) => {
              setWebhookSecretOpen(nextOpen);
              if (!nextOpen) {
                setWebhookSecret(null);
              }
            }}
            title={m.imagePullWebhookSecretModalTitle()}
            description={m.imagePullWebhookSecretModalDescription()}
            label={m.imagePullWebhookSecret()}
            value={webhookSecret ?? ""}
            inputId={`app-pull-webhook-secret-${id}`}
            copyTitle={m.imagePullWebhookSecret()} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
