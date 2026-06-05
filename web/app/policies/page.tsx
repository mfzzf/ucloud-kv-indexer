"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleHelp, Pencil, PlayCircle, Plus, Trash2 } from "lucide-react";
import {
  api,
  Cluster as ConfigCluster,
  ModelProfile,
  Policy,
  PolicyPreview,
  RuleCondition,
} from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectGroup,
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
import { Switch } from "@/components/ui/switch";
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
import { backendQ, clusterQ, useCluster } from "@/lib/cluster";
import { useT } from "@/lib/i18n";

const conditionFields = [
  "cluster_id",
  "model_id",
  "tenant_id",
  "input_tokens",
  "hit_ratio",
  "best_hit_tokens",
  "effective_cached_tokens",
  "kv_event_state",
  "tokenized",
  "hash_supported",
  "has_candidates",
];

const conditionOps = ["eq", "neq", "in", "not_in", "gt", "gte", "lt", "lte", "contains"];
const actionTypes = ["accept", "reject", "require_cache_hit"];
const outcomes = ["reject", "fallback_accept", "accept"];
const anyClusterValue = "__any_cluster__";

type ConditionDraft = {
  field: string;
  op: string;
  value: string;
};

type PolicyFormState = {
  rule_id: string;
  name: string;
  priority: number;
  scope_cluster: string;
  conditions: ConditionDraft[];
  action_type: string;
  min_hit_ratio: number;
  on_low_hit: string;
  on_uncertain: string;
  reject_status: number;
  enabled: boolean;
};

export default function PoliciesPage() {
  const t = useT();
  const qc = useQueryClient();
  const { cluster, clusters, multiCluster } = useCluster();
  const clusterCatalog = useQuery({
    queryKey: ["policy-cluster-options"],
    queryFn: () => api.get<ConfigCluster[]>("/clusters"),
    retry: false,
    staleTime: 10_000,
  });
  const clusterOptions = React.useMemo(
    () =>
      [
        ...new Set([
          ...clusters.map((c) => c.cluster),
          ...(clusterCatalog.data ?? []).map((c) => c.cluster_id),
        ]),
      ]
        .filter(Boolean)
        .sort(),
    [clusters, clusterCatalog.data],
  );
  const policies = useQuery({
    queryKey: ["policies", cluster],
    queryFn: () => api.get<Policy[]>(clusterQ("/policies", cluster)),
  });
  const [open, setOpen] = React.useState(false);
  const [testOpen, setTestOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<Policy | null>(null);
  const [deleteErr, setDeleteErr] = React.useState("");

  const deletePolicy = useMutation({
    mutationFn: (p: Policy) => {
      const path = `/policies/${encodeURIComponent(p.rule_id)}`;
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

  const togglePolicy = useMutation({
    mutationFn: ({ policy, enabled }: { policy: Policy; enabled: boolean }) => {
      const path = `/policies/${encodeURIComponent(policy.rule_id)}`;
      const target = policy._backend
        ? backendQ(path, policy._backend)
        : clusterQ(path, cluster);
      return api.patch(target, policyPayload({ ...policy, enabled }));
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policies"] }),
    onError: (e: Error) => setDeleteErr(e.message),
  });

  const confirmDelete = (p: Policy) => {
    if (
      !window.confirm(
        t("policies.confirm.delete").replace("{id}", p.rule_id),
      )
    ) {
      return;
    }
    deletePolicy.mutate(p);
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t("policies.title")}
        subtitle={t("policies.subtitle")}
        actions={
          <div className="flex gap-2">
            <Sheet open={testOpen} onOpenChange={setTestOpen}>
              <SheetTrigger asChild>
                <Button variant="outline">
                  <PlayCircle data-icon="inline-start" />
                  {t("policies.btn.test")}
                </Button>
              </SheetTrigger>
              <SheetContent className="w-full sm:max-w-md">
                <SheetHeader>
                  <SheetTitle>{t("policies.test.title")}</SheetTitle>
                  <SheetDescription>{t("policies.test.desc")}</SheetDescription>
                </SheetHeader>
                <RulePreview cluster={cluster} clusterOptions={clusterOptions} />
              </SheetContent>
            </Sheet>
            <Sheet open={open} onOpenChange={setOpen}>
              <SheetTrigger asChild>
                <Button>
                  <Plus data-icon="inline-start" />
                  {t("policies.btn.new")}
                </Button>
              </SheetTrigger>
              <SheetContent className="w-full sm:max-w-2xl">
                <SheetHeader>
                  <SheetTitle>{t("policies.sheet.title")}</SheetTitle>
                  <SheetDescription>{t("policies.sheet.desc")}</SheetDescription>
                </SheetHeader>
                <PolicyForm
                  cluster={cluster}
                  multiCluster={multiCluster}
                  clusterOptions={clusterOptions}
                  onDone={() => {
                    setOpen(false);
                    qc.invalidateQueries({ queryKey: ["policies"] });
                  }}
                  onCancel={() => setOpen(false)}
                />
              </SheetContent>
            </Sheet>
          </div>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>{t("policies.list.title")}</CardTitle>
          <CardDescription>{t("policies.list.desc")}</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          {deleteErr && (
            <div className="px-6 pb-3 text-sm text-destructive">
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
                  <TableHead className="w-20 pl-6">
                    <HeadWithHelp
                      label={t("policies.col.priority")}
                      help={t("policies.help.priority")}
                    />
                  </TableHead>
                  <TableHead>
                    <HeadWithHelp
                      label={t("policies.col.name")}
                      help={t("policies.help.rule_id")}
                    />
                  </TableHead>
                  {multiCluster && (
                    <TableHead>
                      <HeadWithHelp
                        label={t("policies.col.scope_cluster")}
                        help={t("policies.help.cluster")}
                      />
                    </TableHead>
                  )}
                  <TableHead className="min-w-72">
                    <HeadWithHelp
                      label={t("policies.col.conditions")}
                      help={t("policies.help.conditions")}
                    />
                  </TableHead>
                  <TableHead>
                    <HeadWithHelp
                      label={t("policies.col.action")}
                      help={t("policies.help.action")}
                    />
                  </TableHead>
                  <TableHead className="text-right">
                    <HeadWithHelp
                      align="right"
                      label={t("policies.col.minhit")}
                      help={t("policies.help.required_hit")}
                    />
                  </TableHead>
                  <TableHead>
                    <HeadWithHelp
                      label={t("policies.col.uncertain")}
                      help={t("policies.help.uncertain")}
                    />
                  </TableHead>
                  <TableHead>
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
                  <TableRow key={`${p._backend ?? ""}/${p.rule_id}`}>
                    <TableCell className="pl-6 font-mono text-sm">
                      {p.priority ?? 0}
                    </TableCell>
                    <TableCell>
                      <div className="flex min-w-36 flex-col gap-1">
                        <span className="font-medium">
                          {p.name || p.rule_id}
                        </span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {p.rule_id}
                        </span>
                      </div>
                    </TableCell>
                    {multiCluster && (
                      <TableCell className="text-xs">
                        <PolicyClusterScope policy={p} />
                      </TableCell>
                    )}
                    <TableCell>
                      <ConditionChips
                        conditions={(p.conditions ?? []).filter(
                          (condition) => condition.field !== "cluster_id",
                        )}
                      />
                    </TableCell>
                    <TableCell>
                      <ActionBadge action={p.action?.type} />
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {p.action?.type === "require_cache_hit" &&
                      p.action.min_hit_ratio != null
                        ? formatRatio(p.action.min_hit_ratio)
                        : "—"}
                    </TableCell>
                    <TableCell>
                      {p.action?.type === "require_cache_hit" ? (
                        <OutcomeBadge outcome={p.action.on_uncertain} />
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Switch
                        size="sm"
                        checked={p.enabled !== false}
                        disabled={togglePolicy.isPending}
                        aria-label={t("policies.col.enabled")}
                        onCheckedChange={(enabled) =>
                          togglePolicy.mutate({ policy: p, enabled })
                        }
                      />
                    </TableCell>
                    <TableCell className="pr-6">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("common.edit")}
                          onClick={() => setEditing(p)}
                        >
                          <Pencil data-icon="icon" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-destructive hover:text-destructive"
                          aria-label={t("common.delete")}
                          disabled={deletePolicy.isPending}
                          onClick={() => confirmDelete(p)}
                        >
                          <Trash2 data-icon="icon" />
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

      <Sheet open={editing !== null} onOpenChange={(v) => !v && setEditing(null)}>
        <SheetContent className="w-full sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>
              {editing
                ? t("policies.sheet.edit").replace("{id}", editing.rule_id)
                : t("common.edit")}
            </SheetTitle>
            <SheetDescription>{t("policies.sheet.desc")}</SheetDescription>
          </SheetHeader>
          {editing && (
            <PolicyForm
              cluster={cluster}
              multiCluster={multiCluster}
              clusterOptions={clusterOptions}
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
          className="inline-flex size-4 shrink-0 cursor-help items-center justify-center rounded-full text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <CircleHelp aria-hidden="true" />
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

function FieldLabelWithHelp({
  children,
  help,
  htmlFor,
}: {
  children: React.ReactNode;
  help: string;
  htmlFor?: string;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <FieldLabel htmlFor={htmlFor}>{children}</FieldLabel>
      <HelpTip text={help} />
    </div>
  );
}

function ConditionChips({ conditions }: { conditions: RuleCondition[] }) {
  const t = useT();
  if (conditions.length === 0) {
    return <Badge variant="secondary">{t("policies.conditions.all")}</Badge>;
  }
  return (
    <div className="flex max-w-xl flex-wrap gap-1.5">
      {conditions.map((condition, index) => (
        <Badge key={index} variant="outline" className="font-mono">
          {conditionText(condition, t)}
        </Badge>
      ))}
    </div>
  );
}

function PolicyClusterScope({ policy }: { policy: Policy }) {
  const t = useT();
  return (
    <div className="flex min-w-28 flex-col gap-1">
      <Badge variant="outline">
        {policyClusterScope(policy.conditions ?? [], t)}
      </Badge>
      {policy._cluster && (
        <span className="text-xs text-muted-foreground">
          {t("policies.storage_cluster").replace("{cluster}", policy._cluster)}
        </span>
      )}
    </div>
  );
}

function ActionBadge({ action }: { action?: string }) {
  const t = useT();
  if (action === "reject") {
    return <Badge variant="destructive">{t("policies.action.reject")}</Badge>;
  }
  if (action === "require_cache_hit") {
    return <Badge variant="default">{t("policies.action.require_hit")}</Badge>;
  }
  return <Badge variant="success">{t("policies.action.accept")}</Badge>;
}

function OutcomeBadge({ outcome }: { outcome?: string }) {
  const t = useT();
  if (outcome === "accept") {
    return <Badge variant="success">{t("policies.outcome.accept")}</Badge>;
  }
  if (outcome === "reject") {
    return <Badge variant="destructive">{t("policies.outcome.reject")}</Badge>;
  }
  return <Badge variant="warning">{t("policies.outcome.fallback_accept")}</Badge>;
}

function RulePreview({
  cluster,
  clusterOptions,
}: {
  cluster: string;
  clusterOptions: string[];
}) {
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
    cluster_id: cluster === "all" ? "" : cluster,
    model_id: "qwen3.5-4b",
    tenant_id: "",
    input_tokens: 256,
    hit_ratio: 0.5,
  });
  const [preview, setPreview] = React.useState<PolicyPreview | null>(null);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    setScope((s) => ({ ...s, cluster_id: cluster === "all" ? "" : cluster }));
  }, [cluster]);

  React.useEffect(() => {
    if (profileIDs.length === 0) return;
    if (!profileIDs.includes(scope.model_id)) {
      setScope((s) => ({ ...s, model_id: profileIDs[0] }));
      setPreview(null);
      setErr("");
    }
  }, [profileIDs, scope.model_id]);

  const run = useMutation({
    mutationFn: () =>
      api.post<PolicyPreview>(
        clusterQ("/config/effective-policy/preview", scope.cluster_id || cluster),
        scope,
      ),
    onSuccess: (d) => {
      setPreview(d);
      setErr("");
    },
    onError: (e: Error) => setErr(e.message),
  });

  return (
    <FieldGroup className="overflow-y-auto px-6 pb-6">
      <Field>
        <FieldLabelWithHelp help={t("policies.help.model")}>
          {t("policies.field.model")}
        </FieldLabelWithHelp>
        {profileIDs.length > 0 ? (
          <Select
            value={scope.model_id}
            onValueChange={(v) => setScope({ ...scope, model_id: v })}
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {profileIDs.map((id) => (
                  <SelectItem key={id} value={id}>
                    {id}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={scope.model_id}
            onChange={(e) =>
              setScope({ ...scope, model_id: e.target.value })
            }
          />
        )}
      </Field>
      <Field>
        <FieldLabelWithHelp help={t("policies.help.tenant")}>
          {t("policies.field.tenant")}
        </FieldLabelWithHelp>
        <Input
          value={scope.tenant_id}
          placeholder={t("common.default")}
          onChange={(e) => setScope({ ...scope, tenant_id: e.target.value })}
        />
      </Field>
      <Field>
        <FieldLabelWithHelp help={t("policies.help.cluster")}>
          {t("policies.field.cluster")}
        </FieldLabelWithHelp>
        {clusterOptions.length > 0 ? (
          <Select
            value={scope.cluster_id || anyClusterValue}
            onValueChange={(value) =>
              setScope({
                ...scope,
                cluster_id: value === anyClusterValue ? "" : value,
              })
            }
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value={anyClusterValue}>{t("common.any")}</SelectItem>
                {clusterOptions.map((id) => (
                  <SelectItem key={id} value={id}>
                    {id}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={scope.cluster_id}
            placeholder={t("common.any")}
            onChange={(e) => setScope({ ...scope, cluster_id: e.target.value })}
          />
        )}
      </Field>
      <Field>
        <FieldLabelWithHelp help={t("policies.help.input_tokens")}>
          {t("policies.field.input_tokens")}
        </FieldLabelWithHelp>
        <Input
          type="number"
          min={0}
          value={scope.input_tokens}
          onChange={(e) =>
            setScope({ ...scope, input_tokens: Number(e.target.value) })
          }
        />
      </Field>
      <Field>
        <FieldLabelWithHelp help={t("policies.help.preview_hit_ratio")}>
          {t("policies.field.preview_hit_ratio")}
        </FieldLabelWithHelp>
        <Input
          type="number"
          min={0}
          max={1}
          step="0.05"
          value={scope.hit_ratio}
          onChange={(e) =>
            setScope({ ...scope, hit_ratio: Number(e.target.value) })
          }
        />
      </Field>
      <Button onClick={() => run.mutate()} disabled={run.isPending}>
        <PlayCircle data-icon="inline-start" />
        {t("policies.preview.btn")}
      </Button>
      {err && <div className="text-sm text-destructive">{err}</div>}
      {preview && (
        <div className="flex flex-col gap-2 pt-1 text-xs">
          <Separator />
          <Row
            k={t("policies.preview.matched")}
            help={t("policies.help.matched_rule")}
            v={
              preview.result.matched_rule_id
                ? `${preview.result.matched_rule_name || preview.result.matched_rule_id}`
                : t("policies.preview.no_match")
            }
          />
          <Row
            k={t("overview.col.decision")}
            help={t("policies.help.action")}
            v={preview.result.decision}
          />
          <Row
            k={t("overview.col.reason")}
            help={t("policies.help.result_reason")}
            v={preview.result.reason}
          />
          <Row
            k={t("policies.preview.evaluated")}
            help={t("policies.help.evaluated_rules")}
            v={preview.result.evaluated_rule_ids?.join(" → ") || "—"}
          />
        </div>
      )}
    </FieldGroup>
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
      <span className="flex items-center gap-1.5 text-muted-foreground">
        <span>{k}</span>
        {help && <HelpTip text={help} />}
      </span>
      <span className="text-right font-mono font-medium">{v}</span>
    </div>
  );
}

function PolicyForm({
  cluster,
  multiCluster,
  clusterOptions,
  initial,
  onDone,
  onCancel,
}: {
  cluster: string;
  multiCluster: boolean;
  clusterOptions: string[];
  initial?: Policy;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [f, setF] = React.useState<PolicyFormState>(() =>
    formStateFromPolicy(initial, cluster),
  );
  const [err, setErr] = React.useState("");
  const enabledId = React.useId();

  const requiresHit = f.action_type === "require_cache_hit";
  const canReject = f.action_type === "reject" || requiresHit;
  const save = useMutation({
    mutationFn: () => {
      const body = payloadFromForm(f);
      if (!initial) {
        const targetCluster = createTargetCluster(f, cluster, multiCluster);
        if (!targetCluster && multiCluster) {
          throw new Error(t("policies.error.target_cluster_required"));
        }
        return api.post(clusterQ("/policies", targetCluster || cluster), body);
      }
      const path = `/policies/${encodeURIComponent(initial.rule_id)}`;
      const target = initial._backend
        ? backendQ(path, initial._backend)
        : clusterQ(path, cluster);
      return api.patch(target, body);
    },
    onSuccess: onDone,
    onError: (e: Error) => setErr(e.message),
  });

  const set = <K extends keyof PolicyFormState>(key: K, value: PolicyFormState[K]) =>
    setF((prev) => ({ ...prev, [key]: value }));

  const setCondition = (index: number, patch: Partial<ConditionDraft>) => {
    setF((prev) => ({
      ...prev,
      conditions: prev.conditions.map((condition, i) =>
        i === index ? { ...condition, ...patch } : condition,
      ),
    }));
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <FieldGroup className="overflow-y-auto px-6 pb-6">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabelWithHelp help={t("policies.help.rule_id")}>
              {t("policies.field.id")}
            </FieldLabelWithHelp>
            <Input
              value={f.rule_id}
              placeholder={t("policies.field.id_ph")}
              disabled={Boolean(initial)}
              onChange={(e) => set("rule_id", e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabelWithHelp help={t("policies.help.priority")}>
              {t("policies.field.priority")}
            </FieldLabelWithHelp>
            <Input
              type="number"
              value={f.priority}
              onChange={(e) => set("priority", Number(e.target.value))}
            />
          </Field>
          <Field className="sm:col-span-2">
            <FieldLabelWithHelp help={t("policies.help.name")}>
              {t("policies.field.name")}
            </FieldLabelWithHelp>
            <Input
              value={f.name}
              placeholder={t("policies.field.name_ph")}
              onChange={(e) => set("name", e.target.value)}
            />
          </Field>
          <Field className="sm:col-span-2">
            <FieldLabelWithHelp help={t("policies.help.cluster")}>
              {t("policies.field.scope_cluster")}
            </FieldLabelWithHelp>
            <ClusterScopeControl
              value={f.scope_cluster}
              clusterOptions={clusterOptions}
              onChange={(value) => set("scope_cluster", value)}
            />
            <FieldDescription>
              {t("policies.scope_cluster.desc")}
            </FieldDescription>
          </Field>
        </div>

        <FieldSet>
          <div className="flex items-center justify-between gap-3">
            <div>
              <FieldLegend>{t("policies.form.conditions")}</FieldLegend>
              <FieldDescription>{t("policies.form.conditions_desc")}</FieldDescription>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() =>
                setF((prev) => ({
                  ...prev,
                  conditions: [...prev.conditions, defaultCondition()],
                }))
              }
            >
              <Plus data-icon="inline-start" />
              {t("policies.btn.add_condition")}
            </Button>
          </div>
          <div className="flex flex-col gap-3">
            {f.conditions.length === 0 && (
              <div className="rounded-md border p-3 text-sm text-muted-foreground">
                {t("policies.conditions.all_desc")}
              </div>
            )}
            {f.conditions.map((condition, index) => (
              <div
                key={index}
                className="grid gap-2 rounded-md border p-3 md:grid-cols-[minmax(0,1.1fr)_minmax(0,.85fr)_minmax(0,1.3fr)_auto]"
              >
                <Field>
                  <FieldLabel>{t("policies.condition.field")}</FieldLabel>
                  <Select
                    value={condition.field}
                    onValueChange={(value) =>
                      setCondition(index, {
                        field: value,
                        op: defaultOpForField(value),
                        value: defaultValueForField(value),
                      })
                    }
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {conditionFields.map((field) => (
                          <SelectItem key={field} value={field}>
                            {fieldLabel(field, t)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel>{t("policies.condition.op")}</FieldLabel>
                  <Select
                    value={condition.op}
                    onValueChange={(value) => setCondition(index, { op: value })}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {conditionOps.map((op) => (
                          <SelectItem key={op} value={op}>
                            {opLabel(op, t)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel>{t("policies.condition.value")}</FieldLabel>
                  <ConditionValueControl
                    condition={condition}
                    clusterOptions={clusterOptions}
                    onChange={(value) => setCondition(index, { value })}
                  />
                </Field>
                <div className="flex items-end justify-end">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t("common.delete")}
                    onClick={() =>
                      setF((prev) => ({
                        ...prev,
                        conditions: prev.conditions.filter((_, i) => i !== index),
                      }))
                    }
                  >
                    <Trash2 data-icon="icon" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t("policies.form.action")}</FieldLegend>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabelWithHelp help={t("policies.help.action")}>
                {t("policies.field.action")}
              </FieldLabelWithHelp>
              <Select
                value={f.action_type}
                onValueChange={(value) => set("action_type", value)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {actionTypes.map((action) => (
                      <SelectItem key={action} value={action}>
                        {actionLabel(action, t)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            {requiresHit && (
              <Field>
                <FieldLabelWithHelp help={t("policies.help.required_hit")}>
                  {t("policies.field.minhit")}
                </FieldLabelWithHelp>
                <Input
                  type="number"
                  min={0}
                  max={1}
                  step="0.05"
                  value={f.min_hit_ratio}
                  onChange={(e) =>
                    set("min_hit_ratio", Number(e.target.value))
                  }
                />
              </Field>
            )}
            {requiresHit && (
              <Field>
                <FieldLabelWithHelp help={t("policies.help.low_hit")}>
                  {t("policies.field.low_hit")}
                </FieldLabelWithHelp>
                <OutcomeSelect
                  value={f.on_low_hit}
                  onValueChange={(value) => set("on_low_hit", value)}
                />
              </Field>
            )}
            {requiresHit && (
              <Field>
                <FieldLabelWithHelp help={t("policies.help.uncertain")}>
                  {t("policies.field.uncertain")}
                </FieldLabelWithHelp>
                <OutcomeSelect
                  value={f.on_uncertain}
                  onValueChange={(value) => set("on_uncertain", value)}
                />
              </Field>
            )}
            {canReject && (
              <Field>
                <FieldLabelWithHelp help={t("policies.help.reject_status")}>
                  {t("policies.field.reject_status")}
                </FieldLabelWithHelp>
                <Input
                  type="number"
                  value={f.reject_status}
                  onChange={(e) => set("reject_status", Number(e.target.value))}
                />
              </Field>
            )}
          </div>
        </FieldSet>

        <Field orientation="horizontal" className="rounded-md border p-3">
          <Switch
            id={enabledId}
            checked={f.enabled}
            onCheckedChange={(checked) => set("enabled", checked)}
          />
          <FieldContent>
            <FieldLabel htmlFor={enabledId}>{t("policies.col.enabled")}</FieldLabel>
            <FieldDescription>{t("policies.help.enabled")}</FieldDescription>
          </FieldContent>
        </Field>

        {err && <div className="text-sm text-destructive">{err}</div>}
      </FieldGroup>
      <SheetFooter className="border-t">
        <div className="flex w-full justify-end gap-2">
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={!f.rule_id || save.isPending}
          >
            {t("policies.btn.save")}
          </Button>
        </div>
      </SheetFooter>
    </div>
  );
}

function OutcomeSelect({
  value,
  onValueChange,
}: {
  value: string;
  onValueChange: (value: string) => void;
}) {
  const t = useT();
  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {outcomes.map((outcome) => (
            <SelectItem key={outcome} value={outcome}>
              {outcomeLabel(outcome, t)}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function ClusterScopeControl({
  value,
  clusterOptions,
  onChange,
}: {
  value: string;
  clusterOptions: string[];
  onChange: (value: string) => void;
}) {
  const t = useT();
  if (clusterOptions.length === 0) {
    return (
      <Input
        value={value}
        placeholder={t("common.any")}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <Select
      value={value || anyClusterValue}
      onValueChange={(next) =>
        onChange(next === anyClusterValue ? "" : next)
      }
    >
      <SelectTrigger className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectItem value={anyClusterValue}>{t("common.any")}</SelectItem>
          {clusterOptions.map((id) => (
            <SelectItem key={id} value={id}>
              {id}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function ConditionValueControl({
  condition,
  clusterOptions,
  onChange,
}: {
  condition: ConditionDraft;
  clusterOptions: string[];
  onChange: (value: string) => void;
}) {
  const t = useT();
  const singleClusterEq =
    condition.field === "cluster_id" &&
    condition.op !== "in" &&
    condition.op !== "not_in";

  if (singleClusterEq && clusterOptions.length > 0) {
    return (
      <Select value={condition.value} onValueChange={onChange}>
        <SelectTrigger className="w-full">
          <SelectValue placeholder={t("policies.field.cluster")} />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {clusterOptions.map((id) => (
              <SelectItem key={id} value={id}>
                {id}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    );
  }

  return (
    <Input
      value={condition.value}
      placeholder={valuePlaceholder(condition.field, condition.op, t)}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function formStateFromPolicy(initial: Policy | undefined, cluster: string): PolicyFormState {
  const sourceConditions =
    initial?.conditions?.map((condition) => ({
      field: condition.field,
      op: condition.op || "eq",
      value: valueToInput(condition.value),
    })) ?? defaultConditions(cluster);
  const { scopeCluster, conditions } = extractScopeCluster(sourceConditions);

  return {
    rule_id: initial?.rule_id ?? "",
    name: initial?.name ?? "",
    priority: initial?.priority ?? 100,
    scope_cluster: scopeCluster,
    conditions,
    action_type: initial?.action?.type ?? "require_cache_hit",
    min_hit_ratio: initial?.action?.min_hit_ratio ?? 0.5,
    on_low_hit: initial?.action?.on_low_hit ?? "reject",
    on_uncertain: initial?.action?.on_uncertain ?? "fallback_accept",
    reject_status: initial?.action?.reject_status ?? 429,
    enabled: initial?.enabled !== false,
  };
}

function defaultConditions(cluster: string): ConditionDraft[] {
  const out: ConditionDraft[] = [];
  if (cluster !== "all") {
    out.push({ field: "cluster_id", op: "eq", value: cluster });
  }
  out.push(defaultCondition());
  return out;
}

function defaultCondition(): ConditionDraft {
  return { field: "input_tokens", op: "gte", value: "256" };
}

function payloadFromForm(f: PolicyFormState): Policy {
  const action: Policy["action"] = {
    type: f.action_type,
  };
  if (f.action_type === "require_cache_hit") {
    action.min_hit_ratio = f.min_hit_ratio;
    action.on_low_hit = f.on_low_hit;
    action.on_uncertain = f.on_uncertain;
    action.reject_status = f.reject_status;
  }
  if (f.action_type === "reject") {
    action.reject_status = f.reject_status;
  }
  const conditions = f.conditions
    .filter((condition) => condition.field && condition.op)
    .map((condition) => ({
      field: condition.field,
      op: condition.op,
      value: parseConditionValue(condition.field, condition.op, condition.value),
    }));
  if (f.scope_cluster.trim()) {
    conditions.unshift({
      field: "cluster_id",
      op: "eq",
      value: f.scope_cluster.trim(),
    });
  }
  return {
    rule_id: f.rule_id,
    name: f.name || undefined,
    priority: f.priority,
    conditions,
    action,
    enabled: f.enabled,
  };
}

function extractScopeCluster(conditions: ConditionDraft[]) {
  const index = conditions.findIndex(
    (condition) =>
      condition.field === "cluster_id" &&
      (condition.op === "" || condition.op === "eq") &&
      condition.value.trim() !== "",
  );
  if (index < 0) {
    return { scopeCluster: "", conditions };
  }
  return {
    scopeCluster: conditions[index].value.trim(),
    conditions: conditions.filter((_, i) => i !== index),
  };
}

function createTargetCluster(
  f: PolicyFormState,
  selectedCluster: string,
  multiCluster: boolean,
) {
  if (f.scope_cluster.trim()) return f.scope_cluster.trim();
  if (selectedCluster && selectedCluster !== "all") return selectedCluster;
  return multiCluster ? "" : selectedCluster;
}

function policyPayload(p: Policy): Policy {
  return {
    rule_id: p.rule_id,
    name: p.name,
    priority: p.priority,
    conditions: p.conditions ?? [],
    action: p.action,
    enabled: p.enabled,
  };
}

function parseConditionValue(field: string, op: string, raw: string) {
  if (op === "in" || op === "not_in") {
    return raw
      .split(",")
      .map((part) => parseSingleValue(field, part.trim()))
      .filter((part) => part !== "");
  }
  return parseSingleValue(field, raw);
}

function parseSingleValue(field: string, raw: string) {
  if (numericField(field)) {
    return Number(raw);
  }
  if (booleanField(field)) {
    return raw === "true";
  }
  return raw;
}

function valueToInput(value: RuleCondition["value"]): string {
  if (Array.isArray(value)) return value.join(", ");
  if (value == null) return "";
  return String(value);
}

function defaultOpForField(field: string) {
  if (numericField(field)) return "gte";
  return "eq";
}

function defaultValueForField(field: string) {
  if (field === "input_tokens") return "256";
  if (field === "hit_ratio") return "0.5";
  if (field === "kv_event_state") return "available";
  if (booleanField(field)) return "true";
  return "";
}

function numericField(field: string) {
  return [
    "input_tokens",
    "hit_ratio",
    "best_hit_tokens",
    "effective_cached_tokens",
  ].includes(field);
}

function booleanField(field: string) {
  return ["tokenized", "hash_supported", "has_candidates"].includes(field);
}

function valuePlaceholder(field: string, op: string, t: (key: string) => string) {
  if (op === "in" || op === "not_in") return t("policies.placeholder.list");
  if (field === "cluster_id") return "local-vllm";
  if (field === "model_id") return "qwen3.5-4b";
  if (field === "tenant_id") return t("common.default");
  if (field === "input_tokens") return "256";
  if (field === "hit_ratio") return "0.5";
  if (field === "kv_event_state") return "available";
  if (booleanField(field)) return "true";
  return "";
}

function policyClusterScope(
  conditions: RuleCondition[],
  t: (key: string) => string,
) {
  const clusterConditions = conditions.filter(
    (condition) => condition.field === "cluster_id",
  );
  if (clusterConditions.length === 0) return t("common.any");
  if (clusterConditions.length === 1) {
    const condition = clusterConditions[0];
    if (!condition.op || condition.op === "eq") {
      return formatConditionValue(condition.value);
    }
    if (condition.op === "in") {
      return formatConditionValue(condition.value);
    }
    return conditionText(condition, t);
  }
  return clusterConditions
    .map((condition) => conditionText(condition, t))
    .join(" AND ");
}

function conditionText(condition: RuleCondition, t: (key: string) => string) {
  return `${fieldLabel(condition.field, t)} ${opLabel(condition.op, t)} ${formatConditionValue(condition.value)}`;
}

function formatConditionValue(value: RuleCondition["value"]) {
  if (Array.isArray(value)) return value.join(", ");
  if (typeof value === "boolean") return value ? "true" : "false";
  return value ?? "—";
}

function formatRatio(value: number) {
  return `${Math.round(value * 100)}%`;
}

function fieldLabel(field: string, t: (key: string) => string) {
  return t(`policies.condition.field.${field}`);
}

function opLabel(op: string, t: (key: string) => string) {
  return t(`policies.condition.op.${op}`);
}

function actionLabel(action: string, t: (key: string) => string) {
  return t(`policies.action.${action}`);
}

function outcomeLabel(outcome: string, t: (key: string) => string) {
  return t(`policies.outcome.${outcome}`);
}
