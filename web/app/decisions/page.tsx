"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { api, RouteRecord } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
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
import { EmptyState, PageHeader, QueryState } from "@/components/page";
import { useT } from "@/lib/i18n";
import { useCluster, clusterQ } from "@/lib/cluster";

type Filter = "all" | "accept" | "reject" | "fallback";

export default function DecisionsPage() {
  const t = useT();
  const { cluster, multiCluster } = useCluster();
  const [filter, setFilter] = React.useState<Filter>("all");
  const decisions = useQuery({
    queryKey: ["decisions", cluster],
    queryFn: () => api.get<RouteRecord[]>(clusterQ("/decisions", cluster)),
  });
  const all = (decisions.data ?? []).slice().reverse();
  const recs = all.filter((r) => {
    if (filter === "all") return true;
    if (filter === "fallback") return r.fallback;
    if (filter === "reject") return r.decision === "reject";
    return r.decision !== "reject" && !r.fallback;
  });

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("decisions.title")}
        subtitle={t("decisions.subtitle")}
        actions={
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground text-xs tabular-nums">
              {t("decisions.count", { shown: recs.length, total: all.length })}
            </span>
            <Select value={filter} onValueChange={(v) => setFilter(v as Filter)}>
              <SelectTrigger className="h-8 w-[11rem]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("decisions.filter.all")}</SelectItem>
                <SelectItem value="accept">
                  {t("decisions.filter.accept")}
                </SelectItem>
                <SelectItem value="reject">
                  {t("decisions.filter.reject")}
                </SelectItem>
                <SelectItem value="fallback">
                  {t("decisions.filter.fallback")}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        }
      />

      <Card>
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
                  <TableHead className="pl-6">
                    {t("decisions.col.time")}
                  </TableHead>
                  {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                  <TableHead>{t("decisions.col.protocol")}</TableHead>
                  <TableHead>{t("decisions.col.model")}</TableHead>
                  <TableHead>{t("decisions.col.tenant")}</TableHead>
                  <TableHead>{t("decisions.col.decision")}</TableHead>
                  <TableHead>{t("decisions.col.reason")}</TableHead>
                  <TableHead className="text-right">
                    {t("decisions.col.input")}
                  </TableHead>
                  <TableHead className="text-right">
                    {t("decisions.col.hit")}
                  </TableHead>
                  <TableHead>{t("decisions.col.target")}</TableHead>
                  <TableHead className="pr-6">
                    {t("decisions.col.cfg")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recs.map((r, i) => (
                  <TableRow key={i}>
                    <TableCell className="pl-6 font-mono text-xs">
                      {new Date(r.timestamp).toLocaleTimeString()}
                    </TableCell>
                    {multiCluster && (
                      <TableCell className="text-xs">
                        <Badge variant="outline">{r._cluster ?? "—"}</Badge>
                      </TableCell>
                    )}
                    <TableCell className="text-xs">
                      {r.protocol.replace("openai.", "oai.")}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {r.model}
                    </TableCell>
                    <TableCell className="text-xs">
                      {r.tenant_id || t("common.default")}
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
                    <TableCell className="text-right font-mono text-xs">
                      {(r.hit_ratio * 100).toFixed(1)}%
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {r.target_engine || "—"}
                    </TableCell>
                    <TableCell className="pr-6 font-mono text-xs">
                      #{r.config_version}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {recs.length === 0 && (
              <EmptyState>
                {all.length === 0
                  ? t("decisions.empty")
                  : t("decisions.filter.none")}
              </EmptyState>
            )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}
