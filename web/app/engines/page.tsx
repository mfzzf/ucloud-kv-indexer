"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { api, Cluster, Engine } from "@/lib/api";
import { useCluster, clusterQ, backendQ } from "@/lib/cluster";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
  const clusters = useQuery({
    queryKey: ["clusters", cluster],
    queryFn: () => api.get<Cluster[]>(clusterQ("/clusters", cluster)),
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
                clusters={clusters.data ?? []}
                clusterInfos={clusterInfos}
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
                {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
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
                    <TableCell className="text-xs">
                      <Badge variant="outline">{e._cluster ?? "—"}</Badge>
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

function RegisterForm({
  clusters,
  clusterInfos,
  multiCluster,
  onDone,
}: {
  clusters: Cluster[];
  clusterInfos: import("@/lib/api").ClusterInfo[];
  multiCluster: boolean;
  onDone: () => void;
}) {
  const t = useT();
  // Flatten clusters -> backends so the operator can pick exactly where to register.
  const backends = React.useMemo(
    () =>
      clusterInfos.flatMap((c) =>
        c.backends.map((b) => ({ id: b.id, cluster: c.cluster })),
      ),
    [clusterInfos],
  );
  const [targetBackend, setTargetBackend] = React.useState(
    backends[0]?.id ?? "",
  );
  React.useEffect(() => {
    if (!targetBackend && backends[0]) setTargetBackend(backends[0].id);
  }, [backends, targetBackend]);

  // Clusters available for the chosen backend (in single-backend mode, all of them).
  const clusterOptions = multiCluster
    ? clusters.filter((c) => c._backend === targetBackend)
    : clusters;

  const [f, setF] = React.useState({
    engine_id: "",
    cluster_id: clusters[0]?.cluster_id ?? "local",
    framework: "vllm",
    api_endpoint: "http://127.0.0.1:8000",
    tokenizer_endpoint: "http://127.0.0.1:8000",
    kv_event_endpoint: "tcp://127.0.0.1:5559",
    replay_endpoint: "tcp://127.0.0.1:5560",
    topic: "kv-events",
    served_models: "qwen3.5-4b",
  });
  const [err, setErr] = React.useState("");
  const submit = useMutation({
    mutationFn: () =>
      api.post(
        backendQ("/engines/register", multiCluster ? targetBackend : undefined),
        {
          ...f,
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
      setF({ ...f, [k]: v });

  return (
    <div className="flex flex-1 flex-col">
      <div className="grid gap-4 overflow-y-auto px-6 pb-6 sm:grid-cols-2">
        {multiCluster && (
          <Field label={t("engines.field.target_backend")}>
            <Select value={targetBackend} onValueChange={setTargetBackend}>
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
          <Select value={f.cluster_id} onValueChange={set("cluster_id")}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {clusterOptions.map((c) => (
                <SelectItem key={c.cluster_id} value={c.cluster_id}>
                  {c.display_name}
                </SelectItem>
              ))}
              {clusterOptions.length === 0 && (
                <SelectItem value="local">local</SelectItem>
              )}
            </SelectContent>
          </Select>
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
          <Button onClick={() => submit.mutate()} disabled={!f.engine_id}>
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
