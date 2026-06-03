"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleHelp, Pencil, Plus, Trash2 } from "lucide-react";
import { api, EffectivePolicy, ModelProfile, Policy } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { EmptyState, PageHeader, QueryState } from "@/components/page";
import { useT } from "@/lib/i18n";
import { backendQ, useCluster, clusterQ } from "@/lib/cluster";

export default function PoliciesPage() {
  const t = useT();
  const qc = useQueryClient();
  const { cluster, multiCluster } = useCluster();
  const policies = useQuery({
    queryKey: ["policies", cluster],
    queryFn: () => api.get<Policy[]>(clusterQ("/policies", cluster)),
  });
  const [open, setOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<Policy | null>(null);
  const [deleteErr, setDeleteErr] = React.useState("");
  const deletePolicy = useMutation({
    mutationFn: (p: Policy) => {
      const path = `/policies/${encodeURIComponent(p.policy_id)}`;
      const target = p._backend
        ? backendQ(path, p._backend)
        : clusterQ(path, cluster);
      return api.del(target);
    },
    onSuccess: () => {
      setDeleteErr("");
      qc.invalidateQueries({ queryKey: ["policies"] });
    },
    onError: (e: Error) => setDeleteErr(e.message),
  });

  const confirmDelete = (p: Policy) => {
    if (
      !window.confirm(
        t("policies.confirm.delete").replace("{id}", p.policy_id),
      )
    ) {
      return;
    }
    deletePolicy.mutate(p);
  };

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
            {deleteErr && (
              <div className="text-destructive px-6 pb-3 text-sm">
                {deleteErr}
              </div>
            )}
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
                      <HeadWithHelp
                        label={t("policies.col.policy")}
                        help={t("policies.help.rule_id")}
                      />
                    </TableHead>
                    {multiCluster && (
                      <TableHead>
                        <HeadWithHelp
                          label={t("cluster.col")}
                          help={t("policies.help.cluster")}
                        />
                      </TableHead>
                    )}
                    <TableHead>
                      <HeadWithHelp
                        label={t("policies.col.scope")}
                        help={t("policies.help.scope")}
                      />
                    </TableHead>
                    <TableHead className="text-right">
                      <HeadWithHelp
                        align="right"
                        label={t("policies.col.long")}
                        help={t("policies.help.check_after")}
                      />
                    </TableHead>
                    <TableHead className="text-right">
                      <HeadWithHelp
                        align="right"
                        label={t("policies.col.hard")}
                        help={t("policies.help.reject_after")}
                      />
                    </TableHead>
                    <TableHead className="text-right">
                      <HeadWithHelp
                        align="right"
                        label={t("policies.col.minhit")}
                        help={t("policies.help.required_hit")}
                      />
                    </TableHead>
                    <TableHead className="text-right">
                      <HeadWithHelp
                        align="right"
                        label={t("policies.col.ttl")}
                        help={t("policies.help.event_age")}
                      />
                    </TableHead>
                    <TableHead className="pr-6">
                      <HeadWithHelp
                        label={t("policies.col.enabled")}
                        help={t("policies.help.status")}
                      />
                    </TableHead>
                    <TableHead className="pr-6 text-right">
                      {t("engines.col.actions")}
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
                      <TableCell className="pr-6">
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("common.edit")}
                            onClick={() => setEditing(p)}
                          >
                            <Pencil />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="text-destructive hover:text-destructive"
                            aria-label={t("common.delete")}
                            disabled={deletePolicy.isPending}
                            onClick={() => confirmDelete(p)}
                          >
                            <Trash2 />
                          </Button>
                        </div>
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

      <Sheet open={editing !== null} onOpenChange={(v) => !v && setEditing(null)}>
        <SheetContent className="w-full sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>
              {editing
                ? t("policies.sheet.edit").replace("{id}", editing.policy_id)
                : t("common.edit")}
            </SheetTitle>
            <SheetDescription>{t("policies.sheet.desc")}</SheetDescription>
          </SheetHeader>
          {editing && (
            <PolicyForm
              cluster={cluster}
              initial={editing}
              onDone={() => {
                setEditing(null);
                qc.invalidateQueries({ queryKey: ["policies"] });
              }}
              onCancel={() => setEditing(null)}
            />
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

function HelpTip({ text }: { text: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          role="button"
          aria-label={text}
          className="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex size-4 shrink-0 cursor-help items-center justify-center rounded-full outline-none transition-colors focus-visible:ring-2 focus-visible:ring-offset-2"
        >
          <CircleHelp className="size-3.5" aria-hidden="true" />
        </span>
      </TooltipTrigger>
      <TooltipContent
        side="top"
        sideOffset={6}
        className="max-w-72 whitespace-normal leading-relaxed"
      >
        {text}
      </TooltipContent>
    </Tooltip>
  );
}

function HeadWithHelp({
  label,
  help,
  align = "left",
}: {
  label: string;
  help: string;
  align?: "left" | "right";
}) {
  return (
    <div
      className={
        align === "right"
          ? "flex items-center justify-end gap-1.5"
          : "flex items-center gap-1.5"
      }
    >
      <span>{label}</span>
      <HelpTip text={help} />
    </div>
  );
}

function LabelWithHelp({
  label,
  help,
  htmlFor,
}: {
  label: string;
  help: string;
  htmlFor?: string;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      <HelpTip text={help} />
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
  const profiles = useQuery({
    queryKey: ["profiles", cluster],
    queryFn: () => api.get<ModelProfile[]>(clusterQ("/model-profiles", cluster)),
  });
  const profileIDs = React.useMemo(
    () => [...new Set((profiles.data ?? []).map((p) => p.model_id))].sort(),
    [profiles.data],
  );
  const [scope, setScope] = React.useState({
    cluster_id: "",
    model_id: "qwen3.5-4b",
    tenant_id: "",
  });
  const [eff, setEff] = React.useState<EffectivePolicy | null>(null);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    if (profileIDs.length === 0) return;
    if (!profileIDs.includes(scope.model_id)) {
      setScope((s) => ({ ...s, model_id: profileIDs[0] }));
      setEff(null);
      setErr("");
    }
  }, [profileIDs, scope.model_id]);

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
          <LabelWithHelp
            label={t("policies.field.model")}
            help={t("policies.help.model")}
          />
          {profileIDs.length > 0 ? (
            <Select
              value={scope.model_id}
              onValueChange={(v) => setScope({ ...scope, model_id: v })}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {profileIDs.map((id) => (
                  <SelectItem key={id} value={id}>
                    {id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              value={scope.model_id}
              onChange={(e) => setScope({ ...scope, model_id: e.target.value })}
            />
          )}
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.tenant")}
            help={t("policies.help.tenant")}
          />
          <Input
            value={scope.tenant_id}
            placeholder={t("common.default")}
            onChange={(e) => setScope({ ...scope, tenant_id: e.target.value })}
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.cluster")}
            help={t("policies.help.cluster")}
          />
          <Input
            value={scope.cluster_id}
            placeholder={t("common.any")}
            onChange={(e) => setScope({ ...scope, cluster_id: e.target.value })}
          />
        </div>
        <Button onClick={() => run.mutate()}>{t("policies.preview.btn")}</Button>
        {err && <div className="text-destructive text-sm">{err}</div>}
        {eff && (
          <div className="space-y-1 pt-2 text-xs">
            <Row
              k={t("policies.preview.long")}
              help={t("policies.help.check_after")}
              v={eff.long_prompt_threshold_tokens}
            />
            <Row
              k={t("policies.preview.hard")}
              help={t("policies.help.reject_after")}
              v={eff.hard_long_prompt_threshold_tokens}
            />
            <Row
              k={t("policies.preview.minhit")}
              help={t("policies.help.required_hit")}
              v={eff.min_hit_ratio_for_long_prompt}
            />
            <Row
              k={t("policies.preview.ttl")}
              help={t("policies.help.event_age")}
              v={eff.event_freshness_ttl_ms}
            />
            <Row
              k={t("policies.preview.stale")}
              help={t("policies.help.stale_behavior")}
              v={eff.stale_event_behavior}
            />
            <Row
              k={t("policies.preview.weights")}
              help={t("policies.help.weights")}
              v={`${eff.gpu_hit_weight}/${eff.cpu_hit_weight}/${eff.disk_hit_weight}`}
            />
            <Row
              k={t("policies.preview.enabled")}
              help={t("policies.help.status")}
              v={String(eff.enabled)}
            />
            <div className="text-muted-foreground flex items-center gap-1.5 pt-1">
              <span>
                {t("policies.preview.merge")}:{" "}
                {eff.source_policy_ids.join(" → ")}
              </span>
              <HelpTip text={t("policies.help.applied_rules")} />
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Row({
  k,
  v,
  help,
}: {
  k: string;
  v: React.ReactNode;
  help?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-muted-foreground flex items-center gap-1.5">
        <span>{k}</span>
        {help && <HelpTip text={help} />}
      </span>
      <span className="font-mono font-medium">{v}</span>
    </div>
  );
}

function PolicyForm({
  cluster,
  initial,
  onDone,
  onCancel,
}: {
  cluster: string;
  initial?: Policy;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [f, setF] = React.useState({
    policy_id: initial?.policy_id ?? "",
    cluster_id: initial?.scope?.cluster_id ?? "",
    model_id: initial?.scope?.model_id ?? "",
    tenant_id: initial?.scope?.tenant_id ?? "",
    long_prompt_threshold_tokens:
      initial?.long_prompt_threshold_tokens ?? 1024,
    hard_long_prompt_threshold_tokens:
      initial?.hard_long_prompt_threshold_tokens ?? 7168,
    min_hit_ratio_for_long_prompt:
      initial?.min_hit_ratio_for_long_prompt ?? 0.5,
    event_freshness_ttl_ms: initial?.event_freshness_ttl_ms ?? 5000,
    enabled: initial?.enabled !== false,
  });
  const [err, setErr] = React.useState("");
  const enabledId = React.useId();
  const payload = () => ({
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
  });
  const save = useMutation({
    mutationFn: () => {
      if (!initial) {
        return api.post(clusterQ("/policies", cluster), payload());
      }
      const path = `/policies/${encodeURIComponent(initial.policy_id)}`;
      const target = initial._backend
        ? backendQ(path, initial._backend)
        : clusterQ(path, cluster);
      return api.patch(target, payload());
    },
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
          <LabelWithHelp
            label={t("policies.field.id")}
            help={t("policies.help.rule_id")}
          />
          <Input
            value={f.policy_id}
            placeholder={t("policies.field.id_ph")}
            disabled={Boolean(initial)}
            onChange={(e) => setS("policy_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.scope_model")}
            help={t("policies.help.model")}
          />
          <Input
            value={f.model_id}
            placeholder={t("policies.field.ph_any")}
            onChange={(e) => setS("model_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.scope_tenant")}
            help={t("policies.help.tenant")}
          />
          <Input
            value={f.tenant_id}
            placeholder={t("policies.field.ph_any")}
            onChange={(e) => setS("tenant_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.long")}
            help={t("policies.help.check_after")}
          />
          <Input
            type="number"
            value={f.long_prompt_threshold_tokens}
            onChange={(e) =>
              setN("long_prompt_threshold_tokens")(e.target.value)
            }
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.hard")}
            help={t("policies.help.reject_after")}
          />
          <Input
            type="number"
            value={f.hard_long_prompt_threshold_tokens}
            onChange={(e) =>
              setN("hard_long_prompt_threshold_tokens")(e.target.value)
            }
          />
        </div>
        <div className="space-y-2">
          <LabelWithHelp
            label={t("policies.field.minhit")}
            help={t("policies.help.required_hit")}
          />
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
          <LabelWithHelp
            label={t("policies.field.ttl")}
            help={t("policies.help.event_age")}
          />
          <Input
            type="number"
            value={f.event_freshness_ttl_ms}
            onChange={(e) => setN("event_freshness_ttl_ms")(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
          <Checkbox
            id={enabledId}
            checked={f.enabled}
            onCheckedChange={(v) => setF({ ...f, enabled: v === true })}
          />
          <Label htmlFor={enabledId} className="font-normal">
            {t("policies.col.enabled")}
          </Label>
          <HelpTip text={t("policies.help.enabled")} />
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
