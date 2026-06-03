"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, AlertTriangle } from "lucide-react";
import { api, ModelProfile } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
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
import { useCluster, clusterQ, backendQ } from "@/lib/cluster";

export default function ProfilesPage() {
  const t = useT();
  const qc = useQueryClient();
  const { cluster, multiCluster } = useCluster();
  const profiles = useQuery({
    queryKey: ["profiles", cluster],
    queryFn: () => api.get<ModelProfile[]>(clusterQ("/model-profiles", cluster)),
  });
  const [editing, setEditing] = React.useState<ModelProfile | null>(null);
  const [creating, setCreating] = React.useState(false);

  const open = editing !== null;
  const close = () => {
    setEditing(null);
    setCreating(false);
  };
  const existing = creating
    ? undefined
    : profiles.data?.find((p) => p.model_id === editing?.model_id);

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("profiles.title")}
        subtitle={t("profiles.subtitle")}
        actions={
          <Button
            onClick={() => {
              setCreating(true);
              setEditing(blank());
            }}
          >
            <Plus />
            {t("profiles.btn.new")}
          </Button>
        }
      />

      <Sheet open={open} onOpenChange={(v) => !v && close()}>
        <SheetContent className="w-full sm:max-w-lg">
          <SheetHeader>
            <SheetTitle>
              {existing
                ? t("profiles.sheet.edit", { id: existing.model_id })
                : t("profiles.sheet.new")}
            </SheetTitle>
            <SheetDescription>{t("profiles.sheet.desc")}</SheetDescription>
          </SheetHeader>
          {editing && (
            <ProfileForm
              initial={editing}
              existing={existing}
              cluster={cluster}
              onDone={() => {
                close();
                qc.invalidateQueries({ queryKey: ["profiles"] });
              }}
              onCancel={close}
            />
          )}
        </SheetContent>
      </Sheet>

      <Card>
        <CardContent className="px-0">
          <QueryState
            isLoading={profiles.isLoading}
            isError={profiles.isError}
            error={profiles.error}
            onRetry={() => profiles.refetch()}
          >
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">
                  {t("profiles.col.model")}
                </TableHead>
                {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                <TableHead>{t("profiles.col.framework")}</TableHead>
                <TableHead>{t("profiles.col.version")}</TableHead>
                <TableHead>{t("profiles.col.hash")}</TableHead>
                <TableHead>{t("profiles.col.block")}</TableHead>
                <TableHead>{t("profiles.col.namespace")}</TableHead>
                <TableHead>{t("profiles.col.features")}</TableHead>
                <TableHead className="pr-6 text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(profiles.data ?? []).map((p) => (
                <TableRow key={`${p._backend ?? ""}/${p.model_id}`}>
                  <TableCell className="pl-6 font-mono text-xs">
                    {p.model_id}
                  </TableCell>
                  {multiCluster && (
                    <TableCell className="text-xs">
                      <Badge variant="outline">{p._cluster ?? "—"}</Badge>
                    </TableCell>
                  )}
                  <TableCell>
                    <Badge variant="secondary">{p.framework}</Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant="success">v{p.version}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {p.hash_profile}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {p.block_size}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {p.model_id}/v{p.version}/{p.hash_profile}/{p.block_size}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {p.supports_lora && (
                        <Badge variant="outline">
                          {t("profiles.feature.lora")}
                        </Badge>
                      )}
                      {p.supports_multimodal && (
                        <Badge variant="outline">
                          {t("profiles.feature.mm")}
                        </Badge>
                      )}
                      {p.supports_cache_salt && (
                        <Badge variant="outline">
                          {t("profiles.feature.salt")}
                        </Badge>
                      )}
                      {!p.supports_lora &&
                        !p.supports_multimodal &&
                        !p.supports_cache_salt && (
                          <span className="text-muted-foreground text-xs">
                            {t("profiles.text_only")}
                          </span>
                        )}
                    </div>
                  </TableCell>
                  <TableCell className="pr-6 text-right">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setCreating(false);
                        setEditing(p);
                      }}
                    >
                      {t("common.edit")}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {(profiles.data ?? []).length === 0 && (
            <EmptyState>{t("profiles.empty")}</EmptyState>
          )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}

function blank(): ModelProfile {
  return {
    model_id: "",
    framework: "vllm",
    version: 0,
    hash_profile: "vllm-v1-text",
    block_size: 16,
    hash_seed: "0",
    supports_lora: false,
    supports_multimodal: false,
    supports_cache_salt: false,
  };
}

function ProfileForm({
  initial,
  existing,
  cluster,
  onDone,
  onCancel,
}: {
  initial: ModelProfile;
  existing?: ModelProfile;
  cluster: string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [f, setF] = React.useState<ModelProfile>(initial);
  const [err, setErr] = React.useState("");
  const save = useMutation({
    // Editing routes to the row's own backend; creating uses the selected cluster.
    mutationFn: () =>
      api.post<ModelProfile>(
        initial._backend
          ? backendQ("/model-profiles", initial._backend)
          : clusterQ("/model-profiles", cluster),
        f,
      ),
    onSuccess: onDone,
    onError: (e: Error) => setErr(e.message),
  });

  const willBump =
    existing &&
    (existing.block_size !== f.block_size ||
      existing.hash_profile !== f.hash_profile ||
      existing.tokenizer_endpoint !== f.tokenizer_endpoint ||
      existing.hash_seed !== f.hash_seed ||
      existing.framework !== f.framework ||
      existing.supports_lora !== f.supports_lora ||
      existing.supports_multimodal !== f.supports_multimodal ||
      existing.supports_cache_salt !== f.supports_cache_salt);

  const setS = (k: keyof ModelProfile) => (v: string) =>
    setF({ ...f, [k]: v });
  const setN = (k: keyof ModelProfile) => (v: string) =>
    setF({ ...f, [k]: Number(v) });
  const setB = (k: keyof ModelProfile) => (v: boolean) =>
    setF({ ...f, [k]: v });

  return (
    <div className="flex flex-1 flex-col">
      <div className="grid gap-4 overflow-y-auto px-6 pb-6 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("profiles.field.model")}</Label>
          <Input
            value={f.model_id}
            disabled={!!existing}
            placeholder="qwen3.5-4b"
            onChange={(e) => setS("model_id")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("profiles.field.framework")}</Label>
          <Select value={f.framework} onValueChange={setS("framework")}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="vllm">vllm</SelectItem>
              <SelectItem value="sglang">sglang</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("profiles.field.hash")}</Label>
          <Input
            value={f.hash_profile}
            onChange={(e) => setS("hash_profile")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("profiles.field.block")}</Label>
          <Input
            type="number"
            value={f.block_size}
            onChange={(e) => setN("block_size")(e.target.value)}
          />
          <p className="text-muted-foreground text-xs">
            {t("profiles.field.block_hint")}
          </p>
        </div>
        <div className="space-y-2">
          <Label>{t("profiles.field.tokenizer")}</Label>
          <Input
            value={f.tokenizer_endpoint ?? ""}
            placeholder={t("profiles.field.tokenizer_ph")}
            onChange={(e) => setS("tokenizer_endpoint")(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("profiles.field.seed")}</Label>
          <Input
            value={f.hash_seed}
            onChange={(e) => setS("hash_seed")(e.target.value)}
          />
        </div>
        <div className="sm:col-span-2 flex flex-wrap gap-6 pt-2">
          <Check
            label={t("profiles.feature.lora")}
            v={f.supports_lora}
            onChange={setB("supports_lora")}
          />
          <Check
            label={t("profiles.feature.mm")}
            v={f.supports_multimodal}
            onChange={setB("supports_multimodal")}
          />
          <Check
            label={t("profiles.feature.salt")}
            v={f.supports_cache_salt}
            onChange={setB("supports_cache_salt")}
          />
        </div>
        {willBump && (
          <Alert variant="warning" className="sm:col-span-2">
            <AlertTriangle />
            <AlertTitle>
              {t("profiles.bump.title", {
                n: (existing?.version ?? 0) + 1,
              })}
            </AlertTitle>
            <AlertDescription>{t("profiles.bump.desc")}</AlertDescription>
          </Alert>
        )}
        {err && (
          <div className="text-destructive sm:col-span-2 text-sm">{err}</div>
        )}
      </div>
      <SheetFooter className="border-t">
        <div className="flex w-full justify-end gap-2">
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => save.mutate()} disabled={!f.model_id}>
            {willBump ? t("profiles.btn.save_new") : t("common.save")}
          </Button>
        </div>
      </SheetFooter>
    </div>
  );
}

function Check({
  label,
  v,
  onChange,
}: {
  label: string;
  v: boolean;
  onChange: (v: boolean) => void;
}) {
  const id = React.useId();
  return (
    <div className="flex items-center gap-2">
      <Checkbox id={id} checked={v} onCheckedChange={(c) => onChange(!!c)} />
      <Label htmlFor={id} className="cursor-pointer">
        {label}
      </Label>
    </div>
  );
}
