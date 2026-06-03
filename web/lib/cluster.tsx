"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { api, ClusterInfo } from "@/lib/api";

// "all" means fan out across every cluster; otherwise a specific cluster id.
export type ClusterSelection = string; // cluster id or "all"

type Ctx = {
  cluster: ClusterSelection;
  setCluster: (c: ClusterSelection) => void;
  clusters: ClusterInfo[];
  isLoading: boolean;
  // True when the gateway is reachable. When false the app is talking to a
  // single backend directly (no /clusters-health endpoint) and cluster filtering is a no-op.
  multiCluster: boolean;
};

const ClusterCtx = React.createContext<Ctx | null>(null);
const STORAGE_KEY = "kvi.cluster";

export function ClusterProvider({ children }: { children: React.ReactNode }) {
  const [cluster, setClusterState] = React.useState<ClusterSelection>("all");

  React.useEffect(() => {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved) setClusterState(saved);
  }, []);

  const setCluster = React.useCallback((c: ClusterSelection) => {
    setClusterState(c);
    window.localStorage.setItem(STORAGE_KEY, c);
  }, []);

  // The gateway exposes GET /clusters-health. A plain single kvindexer backend does not,
  // so a failure here simply means "no federation" and we hide the switcher.
  const q = useQuery({
    queryKey: ["clusters"],
    queryFn: () => api.get<ClusterInfo[]>("/clusters-health"),
    retry: false,
    staleTime: 10_000,
    refetchInterval: 8000,
  });

  const clusters = q.data ?? [];
  const multiCluster = !q.isError && clusters.length > 0;

  // If the saved/selected cluster disappears from the registry, fall back to "all".
  React.useEffect(() => {
    if (!multiCluster) return;
    if (cluster === "all") return;
    if (!clusters.some((c) => c.cluster === cluster)) {
      setClusterState("all");
    }
  }, [multiCluster, clusters, cluster]);

  const value = React.useMemo(
    () => ({
      cluster: multiCluster ? cluster : "all",
      setCluster,
      clusters,
      isLoading: q.isLoading,
      multiCluster,
    }),
    [cluster, setCluster, clusters, q.isLoading, multiCluster],
  );

  return <ClusterCtx.Provider value={value}>{children}</ClusterCtx.Provider>;
}

export function useCluster() {
  const ctx = React.useContext(ClusterCtx);
  if (!ctx) throw new Error("useCluster must be used inside ClusterProvider");
  return ctx;
}

// clusterQ appends ?cluster= to a path for cluster-scoped reads. "all" omits the
// param (the gateway then fans out across every cluster).
export function clusterQ(path: string, cluster: ClusterSelection): string {
  if (!cluster || cluster === "all") return path;
  const sep = path.includes("?") ? "&" : "?";
  return `${path}${sep}cluster=${encodeURIComponent(cluster)}`;
}

// backendQ appends ?backend= to target one exact backend. Used for writes/patches
// to a row we read from a specific backend (a cluster may hold several backends,
// so ?cluster= alone can be ambiguous). Empty backend (single-backend mode, no
// gateway) omits the param — the plain kvindexer ignores it anyway.
export function backendQ(path: string, backend?: string): string {
  if (!backend) return path;
  const sep = path.includes("?") ? "&" : "?";
  return `${path}${sep}backend=${encodeURIComponent(backend)}`;
}
