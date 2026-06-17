"use client";

import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, AlertTriangle, Upload } from "lucide-react";
import { api, IndexerConnection, ModelProfile } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
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
  const indexerConnections = useQuery({
    queryKey: ["indexer-connections"],
    queryFn: () => api.get<IndexerConnection[]>("/admin/connections"),
    retry: false,
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
        <SheetContent className="max-h-dvh w-full overflow-hidden sm:max-w-lg">
          <SheetHeader className="shrink-0">
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
              connections={indexerConnections.data ?? []}
              registryAvailable={indexerConnections.isSuccess}
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
                <TableHead>{t("profiles.col.tokenizer_source")}</TableHead>
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
                    <Badge
                      variant={
                        p.tokenizer_mode === "local" ? "warning" : "outline"
                      }
                    >
                      {p.tokenizer_mode === "local"
                        ? t("profiles.tokenizer_mode.local")
                        : t("profiles.tokenizer_mode.remote")}
                    </Badge>
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
    tokenizer_mode: "remote",
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
  connections,
  registryAvailable,
  onDone,
  onCancel,
}: {
  initial: ModelProfile;
  existing?: ModelProfile;
  cluster: string;
  connections: IndexerConnection[];
  registryAvailable: boolean;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [f, setF] = React.useState<ModelProfile>(initial);
  const [targetBackend, setTargetBackend] = React.useState(initial._backend ?? "");
  const [tokenizerZip, setTokenizerZip] = React.useState<File | null>(null);
  const [chatTemplateFile, setChatTemplateFile] = React.useState<File | null>(null);
  const [err, setErr] = React.useState("");
  const targetOptions = React.useMemo(
    () => connections.filter((c) => c.enabled || c.id === initial._backend),
    [connections, initial._backend],
  );
  React.useEffect(() => {
    if (!registryAvailable || targetBackend) return;
    const matchingCluster =
      cluster !== "all"
        ? targetOptions.find((c) => c.cluster === cluster)
        : undefined;
    setTargetBackend((matchingCluster ?? targetOptions[0])?.id ?? "");
  }, [cluster, registryAvailable, targetBackend, targetOptions]);
  const selectedTarget = targetOptions.find((c) => c.id === targetBackend);
  const virtualTarget = selectedTarget?.kind === "virtual" || (!registryAvailable && initial._virtual);
  const movingProfile =
    Boolean(existing && initial._backend && targetBackend && targetBackend !== initial._backend);
  React.useEffect(() => {
    if (virtualTarget && f.tokenizer_mode !== "local") {
      setF((prev) => ({ ...prev, tokenizer_mode: "local" }));
    }
  }, [f.tokenizer_mode, virtualTarget]);
  const save = useMutation({
    mutationFn: async () => {
      const target = targetBackend
        ? backendQ("/model-profiles", targetBackend)
        : initial._backend
          ? backendQ("/model-profiles", initial._backend)
          : clusterQ("/model-profiles", cluster);
      const saveTarget =
        movingProfile && initial._backend
          ? appendSourceBackend(target, initial._backend)
          : target;
      const payload = profileForSave(f);
      const hasLocalUpload =
        f.tokenizer_mode === "local" &&
        (tokenizerZip || chatTemplateFile || Boolean(f.chat_template));
      let saved: ModelProfile;
      if (!hasLocalUpload) {
        saved = await api.post<ModelProfile>(saveTarget, payload);
      } else {
        if (tokenizerZip && tokenizerZip.size === 0) {
          throw new Error(t("profiles.error.empty_tokenizer_zip"));
        }
        if (chatTemplateFile && chatTemplateFile.size === 0) {
          throw new Error(t("profiles.error.empty_template_file"));
        }
        const body = new FormData();
        appendProfileForm(body, payload);
        if (tokenizerZip) body.set("tokenizer_zip", tokenizerZip);
        if (chatTemplateFile) body.set("chat_template_file", chatTemplateFile);
        saved = await api.postForm<ModelProfile>(saveTarget, body);
      }
      if (movingProfile && initial._backend) {
        await api.del(
          backendQ(
            `/model-profiles/${encodeURIComponent(initial.model_id)}`,
            initial._backend,
          ),
        );
      }
      return saved;
    },
    onSuccess: onDone,
    onError: (e: Error) => setErr(e.message),
  });

  const willBump =
    existing &&
    (existing.block_size !== f.block_size ||
      existing.hash_profile !== f.hash_profile ||
      existing.tokenizer_endpoint !== f.tokenizer_endpoint ||
      existing.tokenizer_mode !== f.tokenizer_mode ||
      existing.chat_template_sha256 !== f.chat_template_sha256 ||
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
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-6 pb-4">
        {registryAvailable && (
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.target_backend")}</Label>
            <Select
              value={targetBackend}
              onValueChange={(v) => {
                setTargetBackend(v);
                const next = targetOptions.find((c) => c.id === v);
                if (next?.kind === "virtual") {
                  setF((prev) => ({ ...prev, tokenizer_mode: "local" }));
                }
              }}
              disabled={targetOptions.length === 0}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {targetOptions.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.cluster} · {c.id}
                    {c.kind === "virtual"
                      ? ` · ${t("indexers.kind.virtual")}`
                      : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.model")}</Label>
            <Input
              value={f.model_id}
              disabled={!!existing}
              placeholder="qwen3.5-4b"
              onChange={(e) => setS("model_id")(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
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
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.hash")}</Label>
            <Input
              value={f.hash_profile}
              onChange={(e) => setS("hash_profile")(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.block")}</Label>
            <Input
              type="number"
              value={f.block_size}
              onChange={(e) => setN("block_size")(e.target.value)}
            />
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.tokenizer_mode")}</Label>
            <Select
              value={f.tokenizer_mode || "remote"}
              onValueChange={setS("tokenizer_mode")}
              disabled={virtualTarget}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="remote">
                  {t("profiles.tokenizer_mode.remote")}
                </SelectItem>
                <SelectItem value="local">
                  {t("profiles.tokenizer_mode.local")}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          {f.tokenizer_mode !== "local" ? (
            <div className="flex flex-col gap-1.5">
              <Label>{t("profiles.field.tokenizer")}</Label>
              <Input
                value={f.tokenizer_endpoint ?? ""}
                placeholder={t("profiles.field.tokenizer_ph")}
                onChange={(e) => setS("tokenizer_endpoint")(e.target.value)}
              />
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label>{t("profiles.field.tokenizer_zip")}</Label>
              <Input
                type="file"
                accept=".zip,application/zip"
                onChange={(e) => setTokenizerZip(e.target.files?.[0] ?? null)}
              />
            </div>
          )}
        </div>
        {f.tokenizer_mode === "local" && (
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label>{t("profiles.field.template_file")}</Label>
              <Input
                type="file"
                accept=".jinja,.jinja2,.txt,.json"
                onChange={(e) =>
                  setChatTemplateFile(e.target.files?.[0] ?? null)
                }
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label>{t("profiles.field.seed")}</Label>
              <Input
                value={f.hash_seed}
                onChange={(e) => setS("hash_seed")(e.target.value)}
              />
            </div>
          </div>
        )}
        {f.tokenizer_mode === "local" ? (
          <div className="flex flex-col gap-1.5">
            <Label>{t("profiles.field.template")}</Label>
            <Textarea
              value={f.chat_template ?? ""}
              onChange={(e) => setS("chat_template")(e.target.value)}
              className="border-input bg-background min-h-20 w-full rounded-md border px-3 py-2 font-mono text-xs shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
            />
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label>{t("profiles.field.seed")}</Label>
              <Input
                value={f.hash_seed}
                onChange={(e) => setS("hash_seed")(e.target.value)}
              />
            </div>
            <div className="flex flex-wrap items-end gap-5 pb-2">
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
          </div>
        )}
        {f.tokenizer_mode === "local" && (
          <div className="flex flex-wrap gap-5">
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
        )}
        {willBump && (
          <Alert variant="warning">
            <AlertTriangle />
            <AlertTitle>
              {t("profiles.bump.title", {
                n: (existing?.version ?? 0) + 1,
              })}
            </AlertTitle>
            <AlertDescription>{t("profiles.bump.desc")}</AlertDescription>
          </Alert>
        )}
        {err && <div className="text-destructive text-sm">{err}</div>}
      </div>
      <SheetFooter className="shrink-0 border-t">
        <div className="flex w-full justify-end gap-2">
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={
              !f.model_id ||
              (registryAvailable && !targetBackend)
            }
          >
            {f.tokenizer_mode === "local" && (tokenizerZip || chatTemplateFile) && (
              <Upload />
            )}
            {movingProfile
              ? t("profiles.btn.move")
              : willBump
                ? t("profiles.btn.save_new")
                : t("common.save")}
          </Button>
        </div>
      </SheetFooter>
    </div>
  );
}

function appendProfileForm(body: FormData, f: ModelProfile) {
  const put = (k: keyof ModelProfile, v: unknown) => {
    if (v === undefined || v === null) return;
    body.set(k, String(v));
  };
  put("model_id", f.model_id);
  put("framework", f.framework);
  put("tokenizer_endpoint", f.tokenizer_endpoint ?? "");
  put("tokenizer_mode", f.tokenizer_mode ?? "remote");
  put("chat_template", f.chat_template ?? "");
  put("hash_profile", f.hash_profile);
  put("block_size", f.block_size);
  put("hash_seed", f.hash_seed);
  put("supports_lora", f.supports_lora);
  put("supports_multimodal", f.supports_multimodal);
  put("supports_cache_salt", f.supports_cache_salt);
}

function profileForSave(f: ModelProfile): ModelProfile {
  if (f.tokenizer_mode === "local") {
    return {
      ...f,
      tokenizer_endpoint: undefined,
      tokenizer_mode: "local",
    };
  }
  return {
    ...f,
    tokenizer_mode: "remote",
    chat_template: undefined,
    chat_template_sha256: undefined,
  };
}

function appendSourceBackend(path: string, sourceBackend: string) {
  const sep = path.includes("?") ? "&" : "?";
  return `${path}${sep}source_backend=${encodeURIComponent(sourceBackend)}`;
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
