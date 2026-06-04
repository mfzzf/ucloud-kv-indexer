"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Area,
  AreaChart,
  CartesianGrid,
  XAxis,
  YAxis,
} from "recharts";
import {
  api,
  Cluster,
  Engine,
  IndexStat,
  ModelProfile,
  RouteRecord,
  StreamHealth,
  streamStatus,
} from "@/lib/api";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { EmptyState, PageHeader, QueryState, StatCard } from "@/components/page";
import { useT } from "@/lib/i18n";
import { useCluster, clusterQ } from "@/lib/cluster";

export default function Overview() {
  const t = useT();
  const { cluster, multiCluster, clusters: clusterList } = useCluster();
  const clusters = useQuery({
    queryKey: ["clusters", cluster],
    queryFn: () => api.get<Cluster[]>(clusterQ("/clusters", cluster)),
  });
  const engines = useQuery({
    queryKey: ["engines", cluster],
    queryFn: () => api.get<Engine[]>(clusterQ("/engines", cluster)),
  });
  const profiles = useQuery({
    queryKey: ["profiles", cluster],
    queryFn: () => api.get<ModelProfile[]>(clusterQ("/model-profiles", cluster)),
  });
  const streams = useQuery({
    queryKey: ["streams", cluster],
    queryFn: () => api.get<StreamHealth[]>(clusterQ("/event-streams", cluster)),
  });
  const decisions = useQuery({
    queryKey: ["decisions", cluster],
    queryFn: () => api.get<RouteRecord[]>(clusterQ("/decisions", cluster)),
  });
  const stats = useQuery({
    queryKey: ["index", cluster],
    queryFn: () => api.get<IndexStat[]>(clusterQ("/index/stats", cluster)),
  });
  const chartConfig = {
    ratio: { label: t("overview.col.hit") + " (%)", color: "var(--chart-1)" },
  } satisfies ChartConfig;

  const recs = decisions.data ?? [];
  const total = recs.length;
  const rejects = recs.filter((r) => r.decision === "reject").length;
  const fallbacks = recs.filter((r) => r.fallback).length;
  const rejectRate = total ? ((rejects / total) * 100).toFixed(1) : "0.0";
  const fallbackRate = total ? ((fallbacks / total) * 100).toFixed(1) : "0.0";

  const streamList = streams.data ?? [];
  // UI-only freshness display. Backend admission trusts listener connection/gaps,
  // while the dashboard still marks very quiet streams as stale for visibility.
  const staleAfterMs = 60_000;
  const now = Date.now();
  const healthyStreams = streamList.filter(
    (s) => streamStatus(s, now, staleAfterMs) === "healthy",
  ).length;
  const badStreams = streamList.filter((s) => {
    const st = streamStatus(s, now, staleAfterMs);
    return st === "stale" || st === "down" || st === "degraded";
  }).length;
  const totalReqKeys = (stats.data ?? []).reduce(
    (a, s) => a + s.request_keys,
    0,
  );

  const chartData = recs.slice(-40).map((r, i) => ({
    i,
    ratio: +(r.hit_ratio * 100).toFixed(1),
  }));

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("overview.title")}
        subtitle={t("overview.subtitle")}
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {multiCluster && (
          <StatCard
            label={t("overview.stat.clusters_count")}
            value={clusterList.length}
          />
        )}
        <StatCard
          label={t("overview.stat.clusters")}
          value={clusters.data?.length ?? "—"}
        />
        <StatCard
          label={t("overview.stat.engines")}
          value={engines.data?.length ?? "—"}
        />
        <StatCard
          label={t("overview.stat.profiles")}
          value={profiles.data?.length ?? "—"}
        />
        <StatCard
          label={t("overview.stat.healthy_streams")}
          value={`${healthyStreams}/${streamList.length}`}
          tone={
            streamList.length > 0 && healthyStreams === streamList.length
              ? "success"
              : badStreams > 0
                ? "destructive"
                : "warning"
          }
          hint={
            badStreams > 0
              ? t("overview.stat.stale_streams") + `: ${badStreams}`
              : undefined
          }
        />
        <StatCard
          label={t("overview.stat.indexed_blocks")}
          value={totalReqKeys}
        />
        <StatCard
          label={t("overview.stat.reject_rate")}
          value={`${rejectRate}%`}
          tone={rejects > 0 ? "destructive" : "muted"}
        />
        <StatCard
          label={t("overview.stat.fallback_rate")}
          value={`${fallbackRate}%`}
          tone={fallbacks > 0 ? "warning" : "muted"}
        />
        <StatCard label={t("overview.stat.decisions")} value={total} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>{t("overview.recent.title")}</CardTitle>
            <CardDescription>
              {t("overview.recent.desc", { n: chartData.length })}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {chartData.length === 0 ? (
              <EmptyState>{t("overview.recent.empty")}</EmptyState>
            ) : (
              <ChartContainer
                config={chartConfig}
                className="aspect-auto h-[220px] w-full"
              >
                <AreaChart data={chartData} margin={{ left: 0, right: 12 }}>
                  <defs>
                    <linearGradient id="ratio-fill" x1="0" y1="0" x2="0" y2="1">
                      <stop
                        offset="5%"
                        stopColor="var(--color-ratio)"
                        stopOpacity={0.45}
                      />
                      <stop
                        offset="95%"
                        stopColor="var(--color-ratio)"
                        stopOpacity={0}
                      />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis dataKey="i" hide />
                  <YAxis
                    domain={[0, 100]}
                    width={32}
                    tickLine={false}
                    axisLine={false}
                    tick={{ fontSize: 11 }}
                  />
                  <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
                  <Area
                    type="monotone"
                    dataKey="ratio"
                    stroke="var(--color-ratio)"
                    fill="url(#ratio-fill)"
                    strokeWidth={2}
                  />
                </AreaChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("overview.clusters.title")}</CardTitle>
            <CardDescription>{t("overview.clusters.desc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {(clusters.data ?? []).map((c) => (
              <div
                key={`${c._backend ?? ""}/${c.cluster_id}`}
                className="flex items-center justify-between border-b py-2 last:border-b-0"
              >
                <div>
                  <div className="text-sm font-medium">
                    {c.display_name}
                    {multiCluster && c._cluster && (
                      <Badge variant="outline" className="ml-2 text-[10px]">
                        {c._cluster}
                      </Badge>
                    )}
                  </div>
                  <div className="text-muted-foreground text-xs">
                    {c.region ?? "—"} · {c.environment ?? "—"}
                  </div>
                </div>
                <Badge
                  variant={
                    c.maintenance_mode
                      ? "warning"
                      : c.enabled
                        ? "success"
                        : "outline"
                  }
                >
                  {c.maintenance_mode
                    ? t("overview.cluster.maintenance")
                    : c.enabled
                      ? t("common.enabled")
                      : t("common.disabled")}
                </Badge>
              </div>
            ))}
            {(clusters.data ?? []).length === 0 && (
              <EmptyState>{t("overview.clusters.empty")}</EmptyState>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t("overview.latest.title")}</CardTitle>
          <CardDescription>{t("overview.latest.desc")}</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          <QueryState
            isLoading={decisions.isLoading}
            isError={decisions.isError}
            error={decisions.error}
            onRetry={() => decisions.refetch()}
          >
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">{t("overview.col.time")}</TableHead>
                <TableHead>{t("overview.col.protocol")}</TableHead>
                <TableHead>{t("overview.col.model")}</TableHead>
                <TableHead>{t("overview.col.decision")}</TableHead>
                <TableHead>{t("overview.col.reason")}</TableHead>
                <TableHead className="text-right">
                  {t("overview.col.tokens")}
                </TableHead>
                <TableHead className="text-right pr-6">
                  {t("overview.col.hit")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {recs
                .slice(-8)
                .reverse()
                .map((r, i) => (
                  <TableRow key={i}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {new Date(r.timestamp).toLocaleTimeString()}
                    </TableCell>
                    <TableCell>
                      {r.protocol
                        .replace("openai.", "")
                        .replace("anthropic.", "")}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {r.model}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          r.decision === "reject"
                            ? "destructive"
                            : r.fallback
                              ? "warning"
                              : "success"
                        }
                      >
                        {r.decision}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {r.reason}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {r.input_tokens}
                    </TableCell>
                    <TableCell className="pr-6 text-right font-mono text-xs">
                      {(r.hit_ratio * 100).toFixed(0)}%
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
          {recs.length === 0 && (
            <EmptyState>{t("overview.latest.empty")}</EmptyState>
          )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}
