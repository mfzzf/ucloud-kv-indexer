"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Eye, RefreshCw } from "lucide-react";
import {
  API_BASE,
  api,
  IndexStat,
  KVEventRecord,
  Policy,
  StreamHealth,
  streamStatus,
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
  Sheet,
  SheetContent,
  SheetDescription,
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

function eventKey(e: KVEventRecord): string {
  return `${e._backend ?? ""}/${e.engine_id}/${e.seq}/${e.kind}/${e.observed_at}`;
}

function appendEvent(prev: KVEventRecord[], ev: KVEventRecord): KVEventRecord[] {
  if (prev.some((p) => eventKey(p) === eventKey(ev))) return prev;
  return [...prev, ev].slice(-100);
}

function observedUnix(e: KVEventRecord): number {
  const ts = Date.parse(e.observed_at);
  return Number.isFinite(ts) ? Math.floor(ts / 1000) : 0;
}

const eventsPerPage = 10;

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
  const kvEvents = useQuery({
    queryKey: ["kv-events", cluster],
    queryFn: () => api.get<KVEventRecord[]>(clusterQ("/kv-events/recent", cluster)),
  });
  const [liveEvents, setLiveEvents] = React.useState<KVEventRecord[]>([]);
  const [eventPage, setEventPage] = React.useState(1);
  const [selectedEvent, setSelectedEvent] = React.useState<KVEventRecord | null>(
    null,
  );
  const [liveConnected, setLiveConnected] = React.useState(false);
  const canStream = !(multiCluster && cluster === "all");

  React.useEffect(() => {
    setLiveEvents((kvEvents.data ?? []).slice(-100));
  }, [kvEvents.data]);

  React.useEffect(() => {
    setEventPage(1);
    setSelectedEvent(null);
  }, [cluster]);

  React.useEffect(() => {
    if (!canStream) {
      setLiveConnected(false);
      return;
    }
    const es = new EventSource(`${API_BASE}${clusterQ("/kv-events/stream", cluster)}`);
    es.onopen = () => setLiveConnected(true);
    es.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data) as KVEventRecord;
        if (!ev._cluster && cluster !== "all") ev._cluster = cluster;
        setLiveEvents((prev) => appendEvent(prev, ev));
      } catch {
        // Ignore malformed SSE frames; the connection will keep carrying later events.
      }
    };
    es.onerror = () => setLiveConnected(false);
    return () => {
      es.close();
      setLiveConnected(false);
    };
  }, [canStream, cluster]);

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
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2">
                {t("streams.events.title")}
                <Badge
                  variant={
                    liveConnected ? "success" : canStream ? "warning" : "outline"
                  }
                >
                  {liveConnected
                    ? t("streams.events.live")
                    : canStream
                      ? t("streams.events.connecting")
                      : t("streams.events.select_cluster")}
                </Badge>
              </CardTitle>
              <CardDescription>{t("streams.events.desc")}</CardDescription>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setEventPage(1);
                kvEvents.refetch();
              }}
              disabled={kvEvents.isFetching}
            >
              <RefreshCw />
              {t("streams.events.query")}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="px-0">
          <QueryState
            isLoading={kvEvents.isLoading}
            isError={kvEvents.isError}
            error={kvEvents.error}
            onRetry={() => kvEvents.refetch()}
            rows={2}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-6">
                    {t("streams.events.col.time")}
                  </TableHead>
                  <TableHead>{t("streams.col.engine")}</TableHead>
                  {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                  <TableHead>{t("streams.events.col.kind")}</TableHead>
                  <TableHead>{t("streams.events.col.model")}</TableHead>
                  <TableHead className="text-right">
                    {t("streams.col.last_seq")}
                  </TableHead>
                  <TableHead>{t("streams.events.col.tier")}</TableHead>
                  <TableHead>{t("streams.events.col.indexed")}</TableHead>
                  <TableHead className="text-right">
                    {t("streams.events.col.tokens")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("streams.events.col.keys")}
                  </TableHead>
                  <TableHead className="pr-6">
                    {t("streams.events.col.skip")}
                  </TableHead>
                  <TableHead className="pr-6 text-right">
                    {t("streams.events.col.detail")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pagedEvents(liveEvents, eventPage).map((e) => (
                  <TableRow key={eventKey(e)}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {rel(observedUnix(e), now)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {e.engine_id}
                    </TableCell>
                    {multiCluster && (
                      <TableCell className="text-xs">
                        <Badge variant="outline">{e._cluster ?? "—"}</Badge>
                      </TableCell>
                    )}
                    <TableCell className="text-xs">
                      <Badge variant="outline">{e.kind}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{e.model}</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {e.seq}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {e.tier || e.medium || "—"}
                    </TableCell>
                    <TableCell>
                      <Badge variant={e.indexed ? "success" : "warning"}>
                        {e.indexed ? t("common.yes") : t("common.no")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {e.token_ids?.length ?? 0}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {e.request_keys?.length ?? 0}
                    </TableCell>
                    <TableCell className="pr-6 text-muted-foreground text-xs">
                      {e.skip_reason || "—"}
                    </TableCell>
                    <TableCell className="pr-6 text-right">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t("streams.events.detail")}
                        onClick={() => setSelectedEvent(e)}
                      >
                        <Eye />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {liveEvents.length > 0 && (
              <EventPager
                page={eventPage}
                total={liveEvents.length}
                onPage={setEventPage}
              />
            )}
            {liveEvents.length === 0 && (
              <EmptyState>{t("streams.events.empty")}</EmptyState>
            )}
          </QueryState>
        </CardContent>
      </Card>

      <KVEventDetail
        event={selectedEvent}
        onOpenChange={(open) => {
          if (!open) setSelectedEvent(null);
        }}
      />

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

function pagedEvents(events: KVEventRecord[], page: number): KVEventRecord[] {
  const newestFirst = [...events].reverse();
  const start = (page - 1) * eventsPerPage;
  return newestFirst.slice(start, start + eventsPerPage);
}

function EventPager({
  page,
  total,
  onPage,
}: {
  page: number;
  total: number;
  onPage: (page: number) => void;
}) {
  const t = useT();
  const pages = Math.max(1, Math.ceil(total / eventsPerPage));
  const safePage = Math.min(page, pages);

  React.useEffect(() => {
    if (page !== safePage) onPage(safePage);
  }, [page, safePage, onPage]);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t px-6 py-3 text-sm">
      <div className="text-muted-foreground">
        {t("streams.events.page_info")
          .replace("{page}", String(safePage))
          .replace("{pages}", String(pages))
          .replace("{total}", String(total))}
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={safePage <= 1}
          onClick={() => onPage(Math.max(1, safePage - 1))}
        >
          <ChevronLeft />
          {t("common.prev")}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={safePage >= pages}
          onClick={() => onPage(Math.min(pages, safePage + 1))}
        >
          {t("common.next")}
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}

function KVEventDetail({
  event,
  onOpenChange,
}: {
  event: KVEventRecord | null;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useT();
  return (
    <Sheet open={event !== null} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{t("streams.events.detail")}</SheetTitle>
          <SheetDescription>
            {event
              ? `${event.engine_id} · ${event.kind} · seq ${event.seq}`
              : ""}
          </SheetDescription>
        </SheetHeader>
        {event && (
          <div className="space-y-5 px-4 pb-6">
            <div className="grid gap-2 text-sm sm:grid-cols-2">
              <DetailRow k={t("streams.events.col.time")} v={event.observed_at} />
              <DetailRow k={t("streams.col.engine")} v={event.engine_id} />
              <DetailRow k={t("cluster.col")} v={event._cluster || "—"} />
              <DetailRow k={t("streams.events.col.model")} v={event.model} />
              <DetailRow k={t("streams.col.namespace")} v={event.namespace || "—"} />
              <DetailRow k={t("streams.col.last_seq")} v={event.seq} />
              <DetailRow k={t("streams.events.col.kind")} v={event.kind} />
              <DetailRow k={t("streams.events.col.tier")} v={event.tier || event.medium || "—"} />
              <DetailRow k={t("streams.events.col.indexed")} v={event.indexed ? t("common.yes") : t("common.no")} />
              <DetailRow k={t("streams.events.col.skip")} v={event.skip_reason || "—"} />
              <DetailRow k="dp_rank" v={event.dp_rank} />
              <DetailRow k="block_size" v={event.block_size || "—"} />
              <DetailRow k="group_idx" v={event.group_idx ?? "—"} />
              <DetailRow k="spec_kind" v={event.spec_kind || "—"} />
              <DetailRow k="sliding_window" v={event.sliding_window ?? "—"} />
              <DetailRow k="lora_id" v={event.lora_id ?? "—"} />
              <DetailRow k="lora_name" v={event.lora_name || "—"} />
              <DetailRow k="extra_key_count" v={event.extra_key_count ?? "—"} />
              <DetailRow k="nested_token_ids" v={event.nested_token_ids ? t("common.yes") : t("common.no")} />
              <DetailRow k="parent_hash" v={event.parent_hash || "—"} />
              <DetailRow k="batch_ts" v={event.batch_ts ?? "—"} />
            </div>

            <ArrayBlock title="token_ids" values={event.token_ids ?? []} />
            <ArrayBlock title="request_keys" values={event.request_keys ?? []} />
            <ArrayBlock title="block_hashes" values={event.block_hashes ?? []} />
            <ArrayBlock title="extra_keys" values={event.extra_keys ?? []} />

            <div className="space-y-2">
              <div className="text-sm font-medium">
                {t("streams.events.raw_json")}
              </div>
              <pre className="bg-muted/50 max-h-80 overflow-auto rounded-md p-3 font-mono text-xs leading-relaxed">
                {JSON.stringify(event, null, 2)}
              </pre>
            </div>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function DetailRow({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="min-w-0 rounded-md border px-3 py-2">
      <div className="text-muted-foreground text-xs">{k}</div>
      <div className="mt-1 break-words font-mono text-xs">{v}</div>
    </div>
  );
}

function ArrayBlock({
  title,
  values,
}: {
  title: string;
  values: Array<string | number>;
}) {
  return (
    <div className="space-y-2">
      <div className="text-sm font-medium">
        {title} <span className="text-muted-foreground">({values.length})</span>
      </div>
      <pre className="bg-muted/50 max-h-40 overflow-auto rounded-md p-3 font-mono text-xs leading-relaxed">
        {values.length ? JSON.stringify(values, null, 2) : "[]"}
      </pre>
    </div>
  );
}
