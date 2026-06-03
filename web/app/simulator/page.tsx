"use client";

import * as React from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Play, Wand2 } from "lucide-react";
import {
  api,
  ModelProfile,
  QueryPrefixResponse,
  RouteResponse,
  TokenizePreview,
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { PageHeader } from "@/components/page";
import { useT } from "@/lib/i18n";
import { useCluster, clusterQ } from "@/lib/cluster";

const PROTOCOLS = [
  { id: "openai.chat", labelKey: "protocol.openai.chat", path: "/v1/chat/completions" },
  { id: "openai.responses", labelKey: "protocol.openai.responses", path: "/v1/responses" },
  { id: "anthropic.messages", labelKey: "protocol.anthropic.messages", path: "/v1/messages" },
];

export default function SimulatorPage() {
  const t = useT();
  const { cluster, multiCluster } = useCluster();
  // The simulator runs single-backend ops, so it needs a concrete cluster.
  // In federation mode "all" is ambiguous — require picking a cluster first.
  const needsCluster = multiCluster && cluster === "all";
  const [model, setModel] = React.useState("qwen3.5-4b");
  const profiles = useQuery({
    queryKey: ["profiles", cluster],
    queryFn: () => api.get<ModelProfile[]>(clusterQ("/model-profiles", cluster)),
    enabled: !needsCluster,
  });
  const profileIDs = React.useMemo(
    () => [...new Set((profiles.data ?? []).map((p) => p.model_id))].sort(),
    [profiles.data],
  );
  const [proto, setProto] = React.useState("openai.chat");
  const [text, setText] = React.useState(
    "In a distant kingdom, the council debated trade routes. ".repeat(20),
  );
  const [tok, setTok] = React.useState<TokenizePreview | null>(null);
  const [hits, setHits] = React.useState<QueryPrefixResponse | null>(null);
  const [route, setRoute] = React.useState<RouteResponse | null>(null);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    if (profileIDs.length === 0) return;
    if (!profileIDs.includes(model)) {
      setModel(profileIDs[0]);
      setTok(null);
      setHits(null);
      setRoute(null);
      setErr("");
    }
  }, [profileIDs, model]);

  function buildRaw() {
    if (proto === "anthropic.messages")
      return {
        model,
        max_tokens: 16,
        messages: [{ role: "user", content: text }],
      };
    if (proto === "openai.responses") return { model, input: text };
    return { model, messages: [{ role: "user", content: text }] };
  }

  const tokenize = useMutation({
    mutationFn: () =>
      api.post<TokenizePreview>(clusterQ("/tokenize/preview", cluster), {
        model,
        protocol: proto,
        raw: buildRaw(),
      }),
    onSuccess: (d) => {
      setTok(d);
      setErr("");
    },
    onError: (e: Error) => {
      setErr(e.message);
      setTok(null);
    },
  });
  const query = useMutation({
    mutationFn: (tokens: number[]) =>
      api.post<QueryPrefixResponse>(clusterQ("/query-prefix", cluster), {
        model,
        token_ids: tokens,
      }),
    onSuccess: (d) => setHits(d),
    onError: (e: Error) => setErr(e.message),
  });
  const PROTO = PROTOCOLS.find((p) => p.id === proto)!;

  const judge = useMutation({
    mutationFn: () => {
      const path = PROTO.path;
      return api.raw<RouteResponse>(clusterQ(path, cluster), {
        method: "POST",
        body: JSON.stringify(buildRaw()),
      });
    },
    onSuccess: (d) => {
      setRoute(d);
      setErr("");
    },
    onError: (e: Error) => setErr(e.message),
  });

  async function runAll() {
    setErr("");
    const tk = await tokenize.mutateAsync().catch(() => null);
    if (tk) await query.mutateAsync(tk.tokens).catch(() => null);
    await judge.mutateAsync().catch(() => null);
  }

  const running = tokenize.isPending || query.isPending || judge.isPending;

  return (
    <div className="space-y-6">
      <PageHeader title={t("sim.title")} subtitle={t("sim.subtitle")} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>{t("sim.req.title")}</CardTitle>
            <CardDescription>{t("sim.req.desc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>{t("sim.field.model")}</Label>
                {profileIDs.length > 0 ? (
                  <Select value={model} onValueChange={setModel}>
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
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                  />
                )}
              </div>
              <div className="space-y-2">
                <Label>{t("sim.field.protocol")}</Label>
                <Select value={proto} onValueChange={setProto}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PROTOCOLS.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {t(p.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-2">
              <Label>{t("sim.field.text")}</Label>
              <Textarea
                value={text}
                onChange={(e) => setText(e.target.value)}
                className="min-h-40 font-mono text-xs"
              />
            </div>
            <div className="flex flex-wrap gap-2">
              <Button onClick={runAll} disabled={needsCluster || running}>
                <Play />
                {running ? t("sim.btn.running") : t("sim.btn.run")}
              </Button>
              <Button
                variant="outline"
                onClick={() => tokenize.mutate()}
                disabled={needsCluster || running}
              >
                <Wand2 />
                {t("sim.btn.tokenize")}
              </Button>
            </div>
            {needsCluster && (
              <div className="text-warning text-sm">{t("sim.needs_cluster")}</div>
            )}
            {err && <div className="text-destructive text-sm">{err}</div>}
            <Separator />
            <div className="space-y-1">
              <div className="text-muted-foreground text-xs">
                {t("sim.raw.desc", { path: PROTO.path })}
              </div>
              <pre className="bg-muted/50 max-h-48 overflow-auto rounded-md p-3 font-mono text-[11px] leading-relaxed">
                {JSON.stringify(buildRaw(), null, 2)}
              </pre>
            </div>
          </CardContent>
        </Card>

        <div className="space-y-4">
          {route && <DecisionCard r={route} />}
          {tok && (
            <Card>
              <CardHeader>
                <CardTitle>{t("sim.tok.title")}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <div className="flex flex-wrap gap-4">
                  <span>{t("sim.tok.tokens", { n: tok.count })}</span>
                  <span>
                    {t("sim.tok.blocks", { n: tok.request_keys.length })}
                  </span>
                  <span className="text-muted-foreground">
                    {t("sim.tok.block_size", { n: tok.block_size })}
                  </span>
                </div>
                <Separator />
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    {t("sim.tok.namespace")}
                  </div>
                  <div className="font-mono text-xs">{tok.namespace}</div>
                </div>
                <div>
                  <div className="text-muted-foreground mb-1 text-xs">
                    {t("sim.tok.req_keys")}
                  </div>
                  <div className="text-primary font-mono text-xs break-all">
                    {tok.request_keys.slice(0, 8).map(String).join(", ")}
                    {tok.request_keys.length > 8 ? " …" : ""}
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
          {hits && (
            <Card>
              <CardHeader className="flex-row items-center justify-between">
                <CardTitle className="flex items-center gap-2">
                  {t("sim.hits.title")}
                  <Badge variant={hits.fresh ? "success" : "warning"}>
                    {hits.fresh ? t("common.fresh") : t("common.stale")}
                  </Badge>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="text-muted-foreground flex flex-wrap gap-3 font-mono text-[11px]">
                  <span>{hits.namespace}</span>
                  <span>{t("sim.tok.block_size", { n: hits.block_size })}</span>
                </div>
                <Separator />
                {Object.keys(hits.instances).length === 0 ? (
                  <div className="text-muted-foreground text-sm">
                    {t("sim.hits.empty")}
                  </div>
                ) : (
                  Object.entries(hits.instances).map(([id, h]) => (
                    <div
                      key={id}
                      className="border-b py-2 last:border-b-0"
                    >
                      <div className="mb-1 text-sm font-medium">{id}</div>
                      <div className="flex flex-wrap gap-4 font-mono text-xs">
                        <span>
                          {t("sim.hits.matched", { n: h.longest_matched })}
                        </span>
                        <span className="text-success">gpu {h.gpu}</span>
                        <span className="text-warning">cpu {h.cpu}</span>
                        <span className="text-muted-foreground">
                          disk {h.disk}
                        </span>
                      </div>
                    </div>
                  ))
                )}
              </CardContent>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}

function DecisionCard({ r }: { r: RouteResponse }) {
  const t = useT();
  const reject = r.decision === "reject";
  return (
    <Card
      className={cn(
        reject && "border-destructive/40",
        !reject && r.fallback && "border-warning/40",
        !reject && !r.fallback && "border-success/40",
      )}
    >
      <CardHeader className="flex-row items-start justify-between gap-2">
        <div className="space-y-1">
          <CardTitle className="text-xl">
            {reject ? t("sim.dec.reject") : t("sim.dec.accept")}
          </CardTitle>
          <CardDescription>{r.reason}</CardDescription>
        </div>
        <Badge
          variant={reject ? "destructive" : r.fallback ? "warning" : "success"}
        >
          {r.decision}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <span>
            {t("sim.dec.input")} <b>{r.cache.input_tokens}</b> {t("sim.dec.tok")}
          </span>
          <span>
            {t("sim.dec.best")} <b>{r.cache.best_hit_tokens}</b>{" "}
            {t("sim.dec.tok")}
          </span>
          <span>
            {t("sim.dec.ratio")}{" "}
            <b className={reject ? "text-destructive" : "text-success"}>
              {(r.cache.hit_ratio * 100).toFixed(1)}%
            </b>
          </span>
          {r.fallback && <Badge variant="warning">{t("sim.dec.fallback")}</Badge>}
        </div>
        {r.target && (
          <div className="text-muted-foreground text-xs">
            {t("sim.dec.target")}{" "}
            <span className="text-foreground font-mono">
              {r.target.engine_id}
            </span>
          </div>
        )}
        {r.error && (
          <div className="bg-muted/50 rounded-md p-3 font-mono text-xs">
            {t("sim.dec.min", {
              min: r.error.min_required_hit_ratio,
              got: (r.error.hit_ratio * 100).toFixed(1),
            })}
          </div>
        )}
        <div className="text-muted-foreground text-xs">
          {t("sim.dec.profile", {
            p: r.config.model_profile_version,
            c: r.config.config_version,
            ids: r.config.effective_policy_ids?.join(" → ") ?? "",
          })}
        </div>
      </CardContent>
    </Card>
  );
}
