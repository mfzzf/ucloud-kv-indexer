"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api, EffectivePolicy, Policy } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { useCluster, clusterQ } from "@/lib/cluster";

export default function PoliciesPage() {
  const t = useT();
  const qc = useQueryClient();
  const { cluster, multiCluster } = useCluster();
  const policies = useQuery({
    queryKey: ["policies", cluster],
    queryFn: () => api.get<Policy[]>(clusterQ("/policies", cluster)),
  });
  const [open, setOpen] = React.useState(false);

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("policies.title")}
        subtitle={t("policies.subtitle")}
        actions={
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger asChild>
              <Button>
                <Plus />
                {t("policies.btn.new")}
              </Button>
            </SheetTrigger>
            <SheetContent className="w-full sm:max-w-lg">
              <SheetHeader>
                <SheetTitle>{t("policies.sheet.title")}</SheetTitle>
                <SheetDescription>{t("policies.sheet.desc")}</SheetDescription>
              </SheetHeader>
              <PolicyForm
                cluster={cluster}
                onDone={() => {
                  setOpen(false);
                  qc.invalidateQueries({ queryKey: ["policies"] });
                }}
                onCancel={() => setOpen(false)}
              />
            </SheetContent>
          </Sheet>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>{t("policies.list.title")}</CardTitle>
            <CardDescription>{t("policies.list.desc")}</CardDescription>
          </CardHeader>
          <CardContent className="px-0">
            <QueryState
              isLoading={policies.isLoading}
              isError={policies.isError}
              error={policies.error}
              onRetry={() => policies.refetch()}
            >
              <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">
                    {t("policies.col.policy")}
                  </TableHead>
                  {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                  <TableHead>{t("policies.col.scope")}</TableHead>
                  <TableHead className="text-right">
                    {t("policies.col.long")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("policies.col.hard")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("policies.col.minhit")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("policies.col.ttl")}
                  </TableHead>
                  <TableHead className="pr-6">
                    {t("policies.col.enabled")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(policies.data ?? []).map((p) => (
                  <TableRow key={`${p._backend ?? ""}/${p.policy_id}`}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {p.policy_id}
                    </TableCell>
                    {multiCluster && (
                      <TableCell className="text-xs">
                        <Badge variant="outline">{p._cluster ?? "—"}</Badge>
                      </TableCell>
                    )}
                    <TableCell className="text-xs">
                      <ScopeLabel p={p} />
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {p.long_prompt_threshold_tokens ?? "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {p.hard_long_prompt_threshold_tokens ?? "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {p.min_hit_ratio_for_long_prompt != null
                        ? p.min_hit_ratio_for_long_prompt.toFixed(2)
                        : "—"}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {p.event_freshness_ttl_ms ?? "—"}
                    </TableCell>
                    <TableCell className="pr-6">
                      <Badge
                        variant={p.enabled === false ? "outline" : "success"}
                      >
                        {p.enabled === false
                          ? t("common.off")
                          : t("common.on")}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {(policies.data ?? []).length === 0 && (
              <EmptyState>{t("policies.list.empty")}</EmptyState>
            )}
            </QueryState>
          </CardContent>
        </Card>

        <EffectivePreview cluster={cluster} />
      </div>
    </div>
  );
}

function ScopeLabel({ p }: { p: Policy }) {
  const t = useT();
  const s = p.scope || {};
  const parts: string[] = [];
  if (s.cluster_id) parts.push(`cluster=${s.cluster_id}`);
  if (s.model_id) parts.push(`model=${s.model_id}`);
  if (s.tenant_id) parts.push(`tenant=${s.tenant_id}`);
  return parts.length ? (
    <>{parts.join(" · ")}</>
  ) : (
    <span className="text-muted-foreground">{t("common.global")}</span>
  );
}

function EffectivePreview({ cluster }: { cluster: string }) {
  const t = useT();
  const [scope, setScope] = React.useState({
    cluster_id: "",
    model_id: "qwen3.5-4b",
    tenant_id: "",
  });
  const [eff, setEff] = React.useState<EffectivePolicy | null>(null);
  const [err, setErr] = React.useState("");
  const run = useMutation({
    mutationFn: () =>
      api.post<EffectivePolicy>(
        clusterQ("/config/effective-policy/preview", cluster),
        scope,
      ),
    onSuccess: (d) => {
      setEff(d);
      setErr("");
    },
    onError: (e: Error) => setErr(e.message),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("policies.preview.title")}</CardTitle>
        <CardDescription>{t("policies.preview.desc")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-2">
          <Label>{t("policies.field.model")}</Label>
          <Input
            value={scope.model_id}
            onChange={(e) => setScope({ ...scope, model_id: e.target.value })}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.tenant")}</Label>
          <Input
            value={scope.tenant_id}
            placeholder={t("common.default")}
            onChange={(e) => setScope({ ...scope, tenant_id: e.target.value })}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.cluster")}</Label>
          <Input
            value={scope.cluster_id}
            placeholder={t("common.any")}
            onChange={(e) => setScope({ ...scope, cluster_id: e.target.value })}
          />
        </div>
        <Button onClick={() => run.mutate()}>{t("policies.preview.btn")}</Button>
        {err && <div className="text-destructive text-sm">{err}</div>}
        {eff && (
          <div className="space-y-1 pt-2 text-xs font-mono">
            <Row k="long_prompt_threshold" v={eff.long_prompt_threshold_tokens} />
            <Row k="hard_threshold" v={eff.hard_long_prompt_threshold_tokens} />
            <Row k="min_hit_ratio" v={eff.min_hit_ratio_for_long_prompt} />
            <Row k="event_freshness_ttl_ms" v={eff.event_freshness_ttl_ms} />
            <Row k="stale_behavior" v={eff.stale_event_behavior} />
            <Row
              k="gpu/cpu/disk weight"
              v={`${eff.gpu_hit_weight}/${eff.cpu_hit_weight}/${eff.disk_hit_weight}`}
            />
            <Row k={t("policies.col.enabled")} v={String(eff.enabled)} />
            <div className="text-muted-foreground pt-1">
              {t("policies.preview.merge")}: {eff.source_policy_ids.join(" → ")}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{k}</span>
      <span className="font-medium">{v}</span>
    </div>
  );
}

function PolicyForm({
  cluster,
  onDone,
  onCancel,
}: {
  cluster: string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [f, setF] = React.useState({
    policy_id: "",
    cluster_id: "",
    model_id: "",
    tenant_id: "",
    long_prompt_threshold_tokens: 1024,
    hard_long_prompt_threshold_tokens: 7168,
    min_hit_ratio_for_long_prompt: 0.5,
    event_freshness_ttl_ms: 5000,
    enabled: true,
  });
  const [err, setErr] = React.useState("");
  const save = useMutation({
    mutationFn: () =>
      api.post(clusterQ("/policies", cluster), {
        policy_id: f.policy_id,
        scope: {
          cluster_id: f.cluster_id || undefined,
          model_id: f.model_id || undefined,
          tenant_id: f.tenant_id || undefined,
        },
        long_prompt_threshold_tokens: f.long_prompt_threshold_tokens,
        hard_long_prompt_threshold_tokens: f.hard_long_prompt_threshold_tokens,
        min_hit_ratio_for_long_prompt: f.min_hit_ratio_for_long_prompt,
        event_freshness_ttl_ms: f.event_freshness_ttl_ms,
        enabled: f.enabled,
      }),
    onSuccess: onDone,
    onError: (e: Error) => setErr(e.message),
  });
  const setS = (k: keyof typeof f) => (v: string) => setF({ ...f, [k]: v });
  const setN = (k: keyof typeof f) => (v: string) =>
    setF({ ...f, [k]: Number(v) });

  return (
    <div className="flex flex-1 flex-col">
      <div className="grid gap-4 overflow-y-auto px-6 pb-6 sm:grid-cols-2">
        <div className="space-y-2 sm:col-span-2">
          <Label>{t("policies.field.id")}</Label>
          <Input
            value={f.policy_id}
            placeholder={t("policies.field.id_ph")}
            onChange={(e) => setS("policy_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.scope_model")}</Label>
          <Input
            value={f.model_id}
            placeholder={t("policies.field.ph_any")}
            onChange={(e) => setS("model_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.scope_tenant")}</Label>
          <Input
            value={f.tenant_id}
            placeholder={t("policies.field.ph_any")}
            onChange={(e) => setS("tenant_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.long")}</Label>
          <Input
            type="number"
            value={f.long_prompt_threshold_tokens}
            onChange={(e) =>
              setN("long_prompt_threshold_tokens")(e.target.value)
            }
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.hard")}</Label>
          <Input
            type="number"
            value={f.hard_long_prompt_threshold_tokens}
            onChange={(e) =>
              setN("hard_long_prompt_threshold_tokens")(e.target.value)
            }
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.minhit")}</Label>
          <Input
            type="number"
            step="0.05"
            value={f.min_hit_ratio_for_long_prompt}
            onChange={(e) =>
              setN("min_hit_ratio_for_long_prompt")(e.target.value)
            }
          />
        </div>
        <div className="space-y-2">
          <Label>{t("policies.field.ttl")}</Label>
          <Input
            type="number"
            value={f.event_freshness_ttl_ms}
            onChange={(e) => setN("event_freshness_ttl_ms")(e.target.value)}
          />
        </div>
        {err && (
          <div className="text-destructive sm:col-span-2 text-sm">{err}</div>
        )}
      </div>
      <SheetFooter className="border-t">
        <div className="flex w-full justify-end gap-2">
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => save.mutate()} disabled={!f.policy_id}>
            {t("policies.btn.save")}
          </Button>
        </div>
      </SheetFooter>
    </div>
  );
}
