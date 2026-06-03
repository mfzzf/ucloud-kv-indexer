"use client";

import { useQuery } from "@tanstack/react-query";
import { api, AuditEntry } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
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

export default function AuditPage() {
  const t = useT();
  const { cluster, multiCluster } = useCluster();
  const audit = useQuery({
    queryKey: ["audit", cluster],
    queryFn: () => api.get<AuditEntry[]>(clusterQ("/config/audit-log", cluster)),
  });
  const entries = (audit.data ?? []).slice().reverse();

  return (
    <div className="space-y-6">
      <PageHeader title={t("audit.title")} subtitle={t("audit.subtitle")} />

      <Card>
        <CardContent className="px-0">
          <QueryState
            isLoading={audit.isLoading}
            isError={audit.isError}
            error={audit.error}
            onRetry={() => audit.refetch()}
          >
            <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">
                  {t("audit.col.version")}
                </TableHead>
                {multiCluster && <TableHead>{t("cluster.col")}</TableHead>}
                <TableHead>{t("audit.col.time")}</TableHead>
                <TableHead>{t("audit.col.action")}</TableHead>
                <TableHead>{t("audit.col.entity")}</TableHead>
                <TableHead>{t("audit.col.id")}</TableHead>
                <TableHead>{t("audit.col.detail")}</TableHead>
                <TableHead className="pr-6">{t("audit.col.flag")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map((a, i) => (
                <TableRow key={i}>
                  <TableCell className="pl-6 font-mono text-xs">
                    #{a.version}
                  </TableCell>
                  {multiCluster && (
                    <TableCell className="text-xs">
                      <Badge variant="outline">{a._cluster ?? "—"}</Badge>
                    </TableCell>
                  )}
                  <TableCell className="font-mono text-xs">
                    {new Date(a.timestamp).toLocaleString()}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        a.action === "remove"
                          ? "destructive"
                          : a.action === "patch"
                            ? "warning"
                            : "secondary"
                      }
                    >
                      {a.action}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs">{a.entity}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {a.entity_id}
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    {a.detail || "—"}
                  </TableCell>
                  <TableCell className="pr-6">
                    {a.version_bump && (
                      <Badge variant="warning">{t("audit.bump_badge")}</Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {entries.length === 0 && <EmptyState>{t("audit.empty")}</EmptyState>}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  );
}
