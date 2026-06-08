"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, Cluster, ClusterInfo, Engine, IndexerConnection } from "@/lib/api";
import { useCluster, clusterQ, backendQ } from "@/lib/cluster";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader, QueryState } from "@/components/page";
import { useT } from "@/lib/i18n";

export default function EnginesPage() {
  const t = useT();
  const qc = useQueryClient();
  const { cluster, multiCluster, clusters: clusterInfos } = useCluster();
  const engines = useQuery({
    queryKey: ["engines", cluster],
    queryFn: () => api.get<Engine[]>(clusterQ("/engines", cluster)),
  });
  const formClusters = useQuery({
    queryKey: ["engine-form-clusters"],
    queryFn: () => api.get<Cluster[]>("/clusters"),
  });
  const indexerConnections = useQuery({
    queryKey: ["indexer-connections"],
    queryFn: () => api.get<IndexerConnection[]>("/admin/connections"),
    retry: false,
  });
  const [open, setOpen] = React.useState(false);

  const patch = useMutation({
    mutationFn: (v: { id: string; body: Record<string, unknown>; backend?: string }) =>
      api.patch(backendQ(`/engines/${v.id}`, v.backend), v.body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["engines"] }),
    onError: (e: Error) =>
      toast.error(t("engines.toast.update_failed"), { description: e.message }),
  });

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("engines.title")}
        subtitle={t("engines.subtitle")}
        actions={
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger asChild>
              <Button>
                <Plus />
                {t("engines.btn.register")}
              </Button>
            </SheetTrigger>
            <SheetContent className="w-full sm:max-w-lg">
              <SheetHeader>
                <SheetTitle>{t("engines.sheet.title")}</SheetTitle>
                <SheetDescription>{t("engines.sheet.desc")}</SheetDescription>
              </SheetHeader>
              <RegisterForm
                clusters={formClusters.data ?? []}
                clusterInfos={clusterInfos}
                connections={indexerConnections.data ?? []}
                registryAvailable={indexerConnections.isSuccess}
                multiCluster={multiCluster}
                onDone={() => {
                  setOpen(false);
                  qc.invalidateQueries({ queryKey: ["engines"] });
                }}
              />
            </SheetContent>
          </Sheet>
        }
      />

      <IndexerConnectionsPanel
        connections={indexerConnections.data ?? []}
        error={indexerConnections.error}
        isError={indexerConnections.isError}
        isLoading={indexerConnections.isLoading}
        onRetry={() => indexerConnections.refetch()}
      />

      <Card>
        <CardContent className="px-0">
          <QueryState
            isLoading={engines.isLoading}
            isError={engines.isError}
            error={engines.error}
            onRetry={() => engines.refetch()}
          >
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">{t("engines.col.engine")}</TableHead>
                {multiCluster && <TableHead>{t("engines.col.indexer")}</TableHead>}
                <TableHead>{t("engines.col.cluster")}</TableHead>
                <TableHead>{t("engines.col.framework")}</TableHead>
                <TableHead>{t("engines.col.models")}</TableHead>
                <TableHead>{t("engines.col.endpoint")}</TableHead>
                <TableHead>{t("engines.col.kv_stream")}</TableHead>
                <TableHead>{t("engines.col.state")}</TableHead>
                <TableHead className="pr-6 text-right">
                  {t("engines.col.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(engines.data ?? []).map((e) => (
                <TableRow key={`${e._backend ?? ""}/${e.engine_id}`}>
                  <TableCell className="pl-6 font-mono text-xs">
                    {e.engine_id}
                  </TableCell>
                  {multiCluster && (
                    <TableCell className="space-y-1 text-xs">
                      <Badge variant="outline">{e._backend ?? "—"}</Badge>
                      <div className="text-muted-foreground">
                        {e._cluster ?? "—"}
                      </div>
                    </TableCell>
                  )}
                  <TableCell>{e.cluster_id}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">{e.framework}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {e.served_models.join(", ")}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {e.api_endpoint}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {e.kv_event_endpoint}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      <Badge variant={e.enabled ? "success" : "outline"}>
                        {e.enabled ? t("common.enabled") : t("common.disabled")}
                      </Badge>
                      {e.draining && (
                        <Badge variant="warning">
                          {t("engines.status.draining")}
                        </Badge>
                      )}
                      {!e.healthy && (
                        <Badge variant="destructive">
                          {t("engines.status.unhealthy")}
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="pr-6">
                    <div className="flex justify-end gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          patch.mutate({
                            id: e.engine_id,
                            body: { enabled: !e.enabled },
                            backend: e._backend,
                          })
                        }
                      >
                        {e.enabled
                          ? t("engines.action.disable")
                          : t("engines.action.enable")}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          patch.mutate({
                            id: e.engine_id,
                            body: { draining: !e.draining },
                            backend: e._backend,
                          })
                        }
                      >
                        {e.draining
                          ? t("engines.action.undrain")
                          : t("engines.action.drain")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {(engines.data ?? []).length === 0 && (
            <EmptyState>{t("engines.empty")}</EmptyState>
          )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}

type ConnectionDraft = {
  id: string;
  cluster: string;
  url: string;
  token: string;
  enabled: boolean;
};

function emptyConnectionDraft(): ConnectionDraft {
  return { id: "", cluster: "", url: "", token: "", enabled: true };
}

function IndexerConnectionsPanel({
  connections,
  error,
  isError,
  isLoading,
  onRetry,
}: {
  connections: IndexerConnection[];
  error: unknown;
  isError: boolean;
  isLoading: boolean;
  onRetry: () => void;
}) {
  const t = useT();
  const qc = useQueryClient();
  const [open, setOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<IndexerConnection | null>(null);
  const [form, setForm] = React.useState<ConnectionDraft>(emptyConnectionDraft);
  const [err, setErr] = React.useState("");
  const [healthChecks, setHealthChecks] = React.useState<
    Record<string, { ok: boolean; message: string; checkedAt: string }>
  >({});

  const refreshRegistry = React.useCallback(() => {
    qc.invalidateQueries({ queryKey: ["indexer-connections"] });
    qc.invalidateQueries({ queryKey: ["clusters"] });
    qc.invalidateQueries({ queryKey: ["engine-form-clusters"] });
    qc.invalidateQueries({ queryKey: ["engines"] });
  }, [qc]);

  const upsert = useMutation({
    mutationFn: (body: Record<string, unknown>) =>
      api.post("/admin/connections", body),
    onSuccess: () => {
      setOpen(false);
      setErr("");
      refreshRegistry();
      toast.success(t("indexers.toast.saved"));
    },
    onError: (e: Error) => setErr(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: string) =>
      api.del(`/admin/connections/${encodeURIComponent(id)}`),
    onSuccess: () => {
      refreshRegistry();
      toast.success(t("indexers.toast.deleted"));
    },
    onError: (e: Error) =>
      toast.error(t("indexers.toast.delete_failed"), { description: e.message }),
  });

  const toggle = useMutation({
    mutationFn: (c: IndexerConnection) =>
      api.post("/admin/connections", {
        id: c.id,
        cluster: c.cluster,
        url: c.url,
        enabled: !c.enabled,
      }),
    onSuccess: refreshRegistry,
    onError: (e: Error) =>
      toast.error(t("indexers.toast.update_failed"), { description: e.message }),
  });

  const checkHealth = useMutation({
    mutationFn: async (c: IndexerConnection) => {
      const groups = await api.get<ClusterInfo[]>("/clusters-health");
      const backend = groups
        .flatMap((group) =>
          group.backends.map((backend) => ({
            ...backend,
            cluster: group.cluster,
          })),
        )
        .find((backend) => backend.id === c.id);
      if (!backend) {
        throw new Error(t("indexers.health.missing"));
      }
      if (!backend.healthy) {
        throw new Error(backend.error || t("indexers.health.unhealthy"));
      }
      return {
        connection: c,
        message: t("indexers.health.ok_detail", { url: backend.url }),
      };
    },
    onSuccess: ({ connection, message }) => {
      setHealthChecks((prev) => ({
        ...prev,
        [connection.id]: {
          ok: true,
          message,
          checkedAt: new Date().toLocaleTimeString(),
        },
      }));
      toast.success(t("indexers.health.ok_toast", { id: connection.id }));
    },
    onError: (e: Error, c) => {
      setHealthChecks((prev) => ({
        ...prev,
        [c.id]: {
          ok: false,
          message: e.message,
          checkedAt: new Date().toLocaleTimeString(),
        },
      }));
      toast.error(t("indexers.health.fail_toast", { id: c.id }), {
        description: e.message,
      });
    },
  });

  const openNew = () => {
    setEditing(null);
    setForm(emptyConnectionDraft());
    setErr("");
    setOpen(true);
  };

  const openEdit = (c: IndexerConnection) => {
    setEditing(c);
    setForm({
      id: c.id,
      cluster: c.cluster,
      url: c.url,
      token: "",
      enabled: c.enabled,
    });
    setErr("");
    setOpen(true);
  };

  const set =
    <K extends keyof ConnectionDraft>(k: K) =>
    (v: ConnectionDraft[K]) =>
      setForm((prev) => ({ ...prev, [k]: v }));

  const submit = () => {
    setErr("");
    const body: Record<string, unknown> = {
      id: form.id.trim(),
      cluster: form.cluster.trim(),
      url: form.url.trim(),
      enabled: form.enabled,
    };
    const token = form.token.trim();
    if (token) body.token = token;
    upsert.mutate(body);
  };

  return (
    <Card>
      <CardHeader>
        <div className="space-y-1.5">
          <CardTitle>{t("indexers.title")}</CardTitle>
          <CardDescription>{t("indexers.desc")}</CardDescription>
        </div>
        <CardAction>
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger asChild>
              <Button variant="outline" onClick={openNew}>
                <Plus />
                {t("indexers.btn.add")}
              </Button>
            </SheetTrigger>
            <SheetContent className="w-full sm:max-w-lg">
              <SheetHeader>
                <SheetTitle>
                  {editing
                    ? t("indexers.sheet.edit", { id: editing.id })
                    : t("indexers.sheet.new")}
                </SheetTitle>
                <SheetDescription>{t("indexers.sheet.desc")}</SheetDescription>
              </SheetHeader>
              <div className="grid gap-4 overflow-y-auto px-6 pb-6 sm:grid-cols-2">
                <Field label={t("indexers.field.id")}>
                  <Input
                    value={form.id}
                    disabled={!!editing}
                    onChange={(e) => set("id")(e.target.value)}
                    placeholder="vllm-hkg-0"
                  />
                </Field>
                <Field label={t("indexers.field.cluster")}>
                  <Input
                    value={form.cluster}
                    onChange={(e) => set("cluster")(e.target.value)}
                    placeholder="hkg-vllm"
                  />
                </Field>
                <Field label={t("indexers.field.url")}>
                  <Input
                    value={form.url}
                    onChange={(e) => set("url")(e.target.value)}
                    placeholder="http://10.0.0.12:8090"
                  />
                </Field>
                <Field label={t("indexers.field.token")}>
                  <Input
                    value={form.token}
                    onChange={(e) => set("token")(e.target.value)}
                    placeholder={
                      editing && editing.has_token
                        ? t("indexers.field.token_keep")
                        : t("indexers.field.token_optional")
                    }
                  />
                </Field>
                <div className="flex items-center justify-between rounded-md border px-3 py-2 sm:col-span-2">
                  <div className="space-y-0.5">
                    <Label>{t("indexers.field.enabled")}</Label>
                    <p className="text-muted-foreground text-xs">
                      {t("indexers.field.enabled_hint")}
                    </p>
                  </div>
                  <Switch
                    checked={form.enabled}
                    onCheckedChange={(checked) => set("enabled")(checked)}
                  />
                </div>
              </div>
              {err && (
                <div className="text-destructive px-6 pb-2 text-sm">{err}</div>
              )}
              <SheetFooter className="border-t">
                <div className="flex w-full justify-end gap-2">
                  <Button variant="outline" onClick={() => setOpen(false)}>
                    {t("common.cancel")}
                  </Button>
                  <Button
                    onClick={submit}
                    disabled={
                      upsert.isPending ||
                      !form.id.trim() ||
                      !form.cluster.trim() ||
                      !form.url.trim()
                    }
                  >
                    {t("common.save")}
                  </Button>
                </div>
              </SheetFooter>
            </SheetContent>
          </Sheet>
        </CardAction>
      </CardHeader>
      <CardContent className="px-0">
        <QueryState
          isLoading={isLoading}
          isError={isError}
          error={error}
          onRetry={onRetry}
        >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">{t("indexers.col.id")}</TableHead>
                <TableHead>{t("indexers.col.cluster")}</TableHead>
                <TableHead>{t("indexers.col.url")}</TableHead>
                <TableHead>{t("indexers.col.token")}</TableHead>
                <TableHead>{t("indexers.col.state")}</TableHead>
                <TableHead>{t("indexers.col.health")}</TableHead>
                <TableHead className="pr-6 text-right">
                  {t("indexers.col.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {connections.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="pl-6 font-mono text-xs">
                    {c.id}
                  </TableCell>
                  <TableCell>{c.cluster}</TableCell>
                  <TableCell className="font-mono text-xs">{c.url}</TableCell>
                  <TableCell>
                    <Badge variant={c.has_token ? "secondary" : "outline"}>
                      {c.has_token
                        ? t("indexers.token.set")
                        : t("indexers.token.none")}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Switch
                        size="sm"
                        checked={c.enabled}
                        disabled={toggle.isPending}
                        onCheckedChange={() => toggle.mutate(c)}
                      />
                      <span className="text-sm">
                        {c.enabled ? t("common.enabled") : t("common.disabled")}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    {healthChecks[c.id] ? (
                      <div className="flex max-w-[240px] flex-col gap-1">
                        <Badge
                          variant={healthChecks[c.id].ok ? "success" : "destructive"}
                        >
                          {healthChecks[c.id].ok
                            ? t("indexers.health.ok")
                            : t("indexers.health.failed")}
                        </Badge>
                        <span className="text-muted-foreground truncate text-xs">
                          {healthChecks[c.id].checkedAt} ·{" "}
                          {healthChecks[c.id].message}
                        </span>
                      </div>
                    ) : (
                      <Badge variant="outline">{t("indexers.health.unknown")}</Badge>
                    )}
                  </TableCell>
                  <TableCell className="pr-6">
                    <div className="flex justify-end gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={
                          !c.enabled ||
                          (checkHealth.isPending &&
                            checkHealth.variables?.id === c.id)
                        }
                        onClick={() => checkHealth.mutate(c)}
                      >
                        {checkHealth.isPending &&
                        checkHealth.variables?.id === c.id ? (
                          <Loader2 data-icon="inline-start" className="animate-spin" />
                        ) : (
                          <Activity data-icon="inline-start" />
                        )}
                        {t("indexers.btn.check")}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => openEdit(c)}
                      >
                        <Pencil />
                        {t("common.edit")}
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => {
                          if (window.confirm(t("indexers.confirm.delete", { id: c.id }))) {
                            remove.mutate(c.id);
                          }
                        }}
                      >
                        <Trash2 />
                        {t("common.delete")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {connections.length === 0 && (
            <EmptyState>{t("indexers.empty")}</EmptyState>
          )}
        </QueryState>
      </CardContent>
    </Card>
  );
}

function RegisterForm({
  clusters,
  clusterInfos,
  connections,
  registryAvailable,
  multiCluster,
  onDone,
}: {
  clusters: Cluster[];
  clusterInfos: import("@/lib/api").ClusterInfo[];
  connections: IndexerConnection[];
  registryAvailable: boolean;
  multiCluster: boolean;
  onDone: () => void;
}) {
  const t = useT();
  const backends = React.useMemo(() => {
    if (registryAvailable) {
      return connections
        .filter((c) => c.enabled)
        .map((c) => ({ id: c.id, cluster: c.cluster, url: c.url }));
    }
    return clusterInfos.flatMap((c) =>
      c.backends.map((b) => ({ id: b.id, cluster: c.cluster, url: b.url })),
    );
  }, [clusterInfos, connections, registryAvailable]);
  const requiresBackend = registryAvailable || multiCluster;
  const [targetBackend, setTargetBackend] = React.useState("");
  const [f, setF] = React.useState({
    engine_id: "",
    cluster_id: "",
    framework: "vllm",
    api_endpoint: "http://127.0.0.1:8000",
    tokenizer_endpoint: "http://127.0.0.1:8000",
    kv_event_endpoint: "tcp://127.0.0.1:5559",
    replay_endpoint: "tcp://127.0.0.1:5560",
    topic: "kv-events",
    served_models: "qwen3.5-4b",
  });
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    if (!requiresBackend) return;
    if (targetBackend && backends.some((b) => b.id === targetBackend)) return;
    setTargetBackend(backends[0]?.id ?? "");
  }, [backends, requiresBackend, targetBackend]);

  const clusterOptions = React.useMemo(() => {
    if (!requiresBackend || !targetBackend) return clusters;
    const byBackend = clusters.filter((c) => c._backend === targetBackend);
    if (byBackend.length > 0) return byBackend;
    const target = backends.find((b) => b.id === targetBackend);
    if (!target) return [];
    return clusters.filter(
      (c) => c.cluster_id === target.cluster || c._cluster === target.cluster,
    );
  }, [backends, clusters, requiresBackend, targetBackend]);

  React.useEffect(() => {
    if (clusterOptions.length === 0) return;
    if (clusterOptions.some((c) => c.cluster_id === f.cluster_id)) return;
    setF((prev) => ({ ...prev, cluster_id: clusterOptions[0].cluster_id }));
  }, [clusterOptions, f.cluster_id]);

  const submit = useMutation({
    mutationFn: () =>
      api.post(
        backendQ(
          "/engines/register",
          requiresBackend ? targetBackend : undefined,
        ),
        {
          ...f,
          cluster_id: f.cluster_id.trim(),
          engine_id: f.engine_id.trim(),
          served_models: f.served_models
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean),
          enabled: true,
          healthy: true,
          dp_ranks: 1,
        },
      ),
    onSuccess: onDone,
    onError: (e: Error) => setErr(e.message),
  });
  const set =
    <K extends keyof typeof f>(k: K) =>
    (v: string) =>
      setF((prev) => ({ ...prev, [k]: v }));

  const runSubmit = () => {
    setErr("");
    if (requiresBackend && !targetBackend) {
      setErr(t("engines.error.no_indexer"));
      return;
    }
    if (!f.cluster_id.trim()) {
      setErr(t("engines.error.no_cluster"));
      return;
    }
    submit.mutate();
  };

  return (
    <div className="flex flex-1 flex-col">
      <div className="grid gap-4 overflow-y-auto px-6 pb-6 sm:grid-cols-2">
        {requiresBackend && (
          <Field label={t("engines.field.target_backend")}>
            <Select
              value={targetBackend}
              onValueChange={setTargetBackend}
              disabled={backends.length === 0}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {backends.map((b) => (
                  <SelectItem key={b.id} value={b.id}>
                    {b.cluster} · {b.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {backends.length === 0 && (
              <p className="text-muted-foreground text-xs">
                {t("engines.no_indexers")}
              </p>
            )}
          </Field>
        )}
        <Field label={t("engines.field.engine_id")}>
          <Input
            value={f.engine_id}
            onChange={(e) => set("engine_id")(e.target.value)}
            placeholder="vllm-qwen-1"
          />
        </Field>
        <Field label={t("engines.field.cluster")}>
          {clusterOptions.length > 0 ? (
            <Select value={f.cluster_id} onValueChange={set("cluster_id")}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {clusterOptions.map((c) => (
                  <SelectItem key={c.cluster_id} value={c.cluster_id}>
                    {c.display_name || c.cluster_id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              value={f.cluster_id}
              onChange={(e) => set("cluster_id")(e.target.value)}
              placeholder="local-vllm"
            />
          )}
        </Field>
        <Field label={t("engines.field.framework")}>
          <Select value={f.framework} onValueChange={set("framework")}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="vllm">vllm</SelectItem>
              <SelectItem value="sglang">sglang</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("engines.field.served")}>
          <Input
            value={f.served_models}
            onChange={(e) => set("served_models")(e.target.value)}
          />
        </Field>
        <Field label={t("engines.field.api")}>
          <Input
            value={f.api_endpoint}
            onChange={(e) => set("api_endpoint")(e.target.value)}
          />
        </Field>
        <Field label={t("engines.field.tokenizer")}>
          <Input
            value={f.tokenizer_endpoint}
            onChange={(e) => set("tokenizer_endpoint")(e.target.value)}
          />
        </Field>
        <Field label={t("engines.field.kv")}>
          <Input
            value={f.kv_event_endpoint}
            onChange={(e) => set("kv_event_endpoint")(e.target.value)}
          />
        </Field>
        <Field label={t("engines.field.replay")}>
          <Input
            value={f.replay_endpoint}
            onChange={(e) => set("replay_endpoint")(e.target.value)}
          />
        </Field>
      </div>
      {err && (
        <div className="text-destructive px-6 pb-2 text-sm">{err}</div>
      )}
      <SheetFooter className="border-t">
        <div className="flex w-full justify-end gap-2">
          <Button variant="outline" onClick={onDone}>
            {t("common.cancel")}
          </Button>
          <Button
            onClick={runSubmit}
            disabled={
              submit.isPending ||
              !f.engine_id.trim() ||
              !f.cluster_id.trim() ||
              (requiresBackend && !targetBackend)
            }
          >
            {t("engines.btn.register")}
          </Button>
        </div>
      </SheetFooter>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      {children}
    </div>
  );
}
