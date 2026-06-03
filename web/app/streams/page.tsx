"use client";

import { useQuery } from "@tanstack/react-query";
import { api, IndexStat, Policy, StreamHealth, streamStatus } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader, QueryState } from "@/components/page";
import { StreamStatusBadge, useRelativeTime } from "@/components/stream-status";
import { useT } from "@/lib/i18n";
import { useCluster, clusterQ } from "@/lib/cluster";

// A connected listener that has gone quiet for longer than the configured freshness
// TTL is effectively "stale" for admission. We give a generous floor so a normal lull
// between events does not flap the badge.
function staleWindow(policies: Policy[] | undefined): number {
  const ttls = (policies ?? [])
    .map((p) => p.event_freshness_ttl_ms)
    .filter((v): v is number => typeof v === "number" && v > 0);
  const ttl = ttls.length ? Math.max(...ttls) : 5000;
  return Math.max(ttl * 6, 30_000);
}

export default function StreamsPage() {
  const t = useT();
  const rel = useRelativeTime();
  const { cluster, multiCluster } = useCluster();
  const streams = useQuery({
    queryKey: ["streams", cluster],
    queryFn: () => api.get<StreamHealth[]>(clusterQ("/event-streams", cluster)),
  });
  const stats = useQuery({
    queryKey: ["index", cluster],
    queryFn: () => api.get<IndexStat[]>(clusterQ("/index/stats", cluster)),
  });
  const policies = useQuery({
    queryKey: ["policies", cluster],
    queryFn: () => api.get<Policy[]>(clusterQ("/policies", cluster)),
  });

  const now = Date.now();
  const staleAfterMs = staleWindow(policies.data);
  const list = streams.data ?? [];
  const unhealthy = list.filter(
    (s) => streamStatus(s, now, staleAfterMs) !== "healthy",
  ).length;

  return (
    <div className="space-y-6">
      <PageHeader title={t("streams.title")} subtitle={t("streams.subtitle")} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            {t("streams.listeners.title")}
            {list.length > 0 && (
              <Badge variant={unhealthy > 0 ? "warning" : "success"}>
                {list.length - unhealthy}/{list.length}
              </Badge>
            )}
          </CardTitle>
          <CardDescription>{t("streams.listeners.desc")}</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          <QueryState
            isLoading={streams.isLoading}
            isError={streams.isError}
            error={streams.error}
            onRetry={() => streams.refetch()}
            rows={2}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">
                    {t("streams.col.engine")}
                  </TableHead>
                  {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                  <TableHead>{t("streams.col.status")}</TableHead>
                  <TableHead>{t("streams.col.endpoint")}</TableHead>
                  <TableHead>{t("streams.col.topic")}</TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.last_seq")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.events")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.last_event")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.gaps")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.skipped")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.decode")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.queue")}
                  </TableHead>
                  <TableHead className="pr-6">
                    {t("streams.col.last_err")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((s) => (
                  <TableRow key={`${s._backend ?? ""}/${s.engine_id}`}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {s.engine_id}
                    </TableCell>
                    {multiCluster && (
                      <TableCell className="text-xs">
                        <Badge variant="outline">{s._cluster ?? "—"}</Badge>
                      </TableCell>
                    )}
                    <TableCell>
                      <StreamStatusBadge
                        stream={s}
                        nowMs={now}
                        staleAfterMs={staleAfterMs}
                      />
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {s.endpoint}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {s.topic}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.last_seq}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.events_total}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {rel(s.last_event_unix, now)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.gaps_total > 0 ? (
                        <Badge variant="warning">{s.gaps_total}</Badge>
                      ) : (
                        0
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {(s.skipped_total ?? 0) > 0 ? (
                        <Badge variant="warning">{s.skipped_total}</Badge>
                      ) : (
                        0
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.decode_errors > 0 ? (
                        <Badge variant="destructive">{s.decode_errors}</Badge>
                      ) : (
                        0
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.queue_cap ? (
                        // Warn when the recv→apply buffer is >50% full: the
                        // apply goroutine is falling behind this engine's rate.
                        (s.queue_depth ?? 0) * 2 > s.queue_cap ? (
                          <Badge variant="warning">
                            {s.queue_depth}/{s.queue_cap}
                          </Badge>
                        ) : (
                          `${s.queue_depth ?? 0}/${s.queue_cap}`
                        )
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="pr-6 text-muted-foreground text-xs">
                      {s.last_error || "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {list.length === 0 && (
              <EmptyState>{t("streams.empty.listeners")}</EmptyState>
            )}
          </QueryState>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("streams.index.title")}</CardTitle>
          <CardDescription>{t("streams.index.desc")}</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          <QueryState
            isLoading={stats.isLoading}
            isError={stats.isError}
            error={stats.error}
            onRetry={() => stats.refetch()}
            rows={2}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">
                    {t("streams.col.namespace")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.req_keys")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.bridges")}
                  </TableHead>
                  <TableHead className="pr-6 text-right">
                    {t("streams.col.engines")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(stats.data ?? []).map((s) => (
                  <TableRow key={`${s._backend ?? ""}/${s.namespace}`}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {s.namespace}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.request_keys}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {s.bridges}
                    </TableCell>
                    <TableCell className="pr-6 text-right font-mono text-xs">
                      {s.engines}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {(stats.data ?? []).length === 0 && (
              <EmptyState>{t("streams.empty.index")}</EmptyState>
            )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}
