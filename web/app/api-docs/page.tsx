"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, RefreshCw } from "lucide-react";
import { API_BASE, api } from "@/lib/api";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader, QueryState } from "@/components/page";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";

type OpenAPIRef = { $ref: string };

type OpenAPIParameter = {
  name?: string;
  in?: string;
  required?: boolean;
  description?: string;
  schema?: { type?: string };
};

type OpenAPISchema = {
  type?: string;
  format?: string;
  description?: string;
  required?: string[];
  properties?: Record<string, OpenAPISchema>;
  items?: OpenAPISchema;
  additionalProperties?: boolean | OpenAPISchema;
};

type OpenAPIRequestBody = {
  description?: string;
  content?: Record<string, { schema?: OpenAPISchema }>;
};

type OpenAPIOperation = {
  tags?: string[];
  summary?: string;
  description?: string;
  parameters?: Array<OpenAPIParameter | OpenAPIRef>;
  requestBody?: OpenAPIRequestBody;
};

type OpenAPISpec = {
  openapi: string;
  info: { title: string; version: string; description?: string };
  paths: Record<string, Record<string, OpenAPIOperation>>;
  components?: {
    parameters?: Record<string, OpenAPIParameter>;
  };
};

type Endpoint = {
  tag: string;
  method: string;
  path: string;
  op: OpenAPIOperation;
};

const methodOrder = ["get", "post", "patch", "delete"];

function flatten(spec: OpenAPISpec | undefined): Endpoint[] {
  if (!spec?.paths) return [];
  const out: Endpoint[] = [];
  for (const [path, methods] of Object.entries(spec.paths)) {
    for (const method of methodOrder) {
      const op = methods[method];
      if (!op) continue;
      out.push({
        tag: op.tags?.[0] ?? "Other",
        method: method.toUpperCase(),
        path,
        op,
      });
    }
  }
  return out;
}

function grouped(endpoints: Endpoint[]) {
  const map = new Map<string, Endpoint[]>();
  for (const endpoint of endpoints) {
    const list = map.get(endpoint.tag) ?? [];
    list.push(endpoint);
    map.set(endpoint.tag, list);
  }
  return [...map.entries()];
}

function methodTone(method: string): React.ComponentProps<typeof Badge>["variant"] {
  switch (method) {
    case "GET":
      return "success";
    case "POST":
      return "default";
    case "PATCH":
      return "warning";
    case "DELETE":
      return "destructive";
    default:
      return "outline";
  }
}

function resolveParam(
  spec: OpenAPISpec | undefined,
  param: OpenAPIParameter | OpenAPIRef,
): OpenAPIParameter {
  if ("$ref" in param) {
    const name = param.$ref.split("/").pop() ?? "";
    return spec?.components?.parameters?.[name] ?? { name };
  }
  return param;
}

function schemaType(schema: OpenAPISchema | undefined): string {
  if (!schema) return "any";
  if (schema.type === "array") {
    return `${schema.items?.type ?? "any"}[]`;
  }
  return schema.type ?? "object";
}

function paramList(spec: OpenAPISpec | undefined, op: OpenAPIOperation) {
  const params = (op.parameters ?? []).map((p) => resolveParam(spec, p));
  return params.map((p) => ({
    name: p.name ?? "param",
    location: p.in ?? "query",
    type: schemaType(p.schema),
    required: Boolean(p.required),
    description: p.description,
  }));
}

function requestSchema(op: OpenAPIOperation): OpenAPISchema | undefined {
  return op.requestBody?.content?.["application/json"]?.schema;
}

function bodyFields(op: OpenAPIOperation) {
  const schema = requestSchema(op);
  const props = schema?.properties ?? {};
  const required = new Set(schema?.required ?? []);
  return Object.entries(props).map(([name, prop]) => ({
    name,
    type: schemaType(prop),
    required: required.has(name),
    description: prop.description,
  }));
}

function FieldChips({
  fields,
  empty,
}: {
  fields: Array<{
    name: string;
    type: string;
    required: boolean;
    location?: string;
    description?: string;
  }>;
  empty: string;
}) {
  if (fields.length === 0) {
    return <span className="text-muted-foreground">{empty}</span>;
  }
  return (
    <div className="flex max-w-md flex-wrap gap-1.5">
      {fields.map((f) => (
        <span
          key={`${f.location ?? "body"}:${f.name}`}
          title={f.description}
          className="bg-muted inline-flex items-center gap-1 rounded-md px-2 py-1 font-mono text-[11px]"
        >
          {f.location && (
            <span className="text-muted-foreground">{f.location}</span>
          )}
          <span>{f.name}</span>
          <span className="text-muted-foreground">:{f.type}</span>
          {f.required && <span className="text-destructive">*</span>}
        </span>
      ))}
    </div>
  );
}

export default function ApiDocsPage() {
  const t = useT();
  const docs = useQuery({
    queryKey: ["openapi"],
    queryFn: () => api.get<OpenAPISpec>("/openapi.json"),
  });
  const endpoints = React.useMemo(() => flatten(docs.data), [docs.data]);
  const groups = React.useMemo(() => grouped(endpoints), [endpoints]);

  return (
    <div className="space-y-6">
      <PageHeader
        title={t("docs.title")}
        subtitle={t("docs.subtitle")}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => docs.refetch()}
              disabled={docs.isFetching}
            >
              <RefreshCw />
              {t("common.refresh")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => window.open(`${API_BASE}/openapi.json`, "_blank")}
            >
              <ExternalLink />
              {t("docs.raw")}
            </Button>
          </>
        }
      />

      <QueryState
        isLoading={docs.isLoading}
        isError={docs.isError}
        error={docs.error}
        onRetry={() => docs.refetch()}
      >
        {docs.data && (
          <Card>
            <CardHeader>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="space-y-1">
                  <CardTitle>{docs.data.info.title}</CardTitle>
                  <CardDescription>
                    {docs.data.info.description}
                  </CardDescription>
                </div>
                <div className="flex gap-2">
                  <Badge variant="outline">OpenAPI {docs.data.openapi}</Badge>
                  <Badge variant="secondary">v{docs.data.info.version}</Badge>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="text-muted-foreground text-sm">
                {t("docs.count", { n: endpoints.length })}
              </div>
            </CardContent>
          </Card>
        )}

        {groups.map(([tag, items]) => (
          <Card key={tag}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                {tag}
                <Badge variant="outline">{items.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent className="px-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-6">{t("docs.col.method")}</TableHead>
                    <TableHead>{t("docs.col.path")}</TableHead>
                    <TableHead>{t("docs.col.summary")}</TableHead>
                    <TableHead>{t("docs.col.params")}</TableHead>
                    <TableHead className="pr-6">
                      {t("docs.col.body")}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={`${item.method} ${item.path}`}>
                      <TableCell className="pl-6">
                        <Badge
                          variant={methodTone(item.method)}
                          className="font-mono"
                        >
                          {item.method}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {item.path}
                      </TableCell>
                      <TableCell>
                        <div className="max-w-xl space-y-1">
                          <div className="text-sm font-medium">
                            {item.op.summary}
                          </div>
                          <div className="text-muted-foreground text-xs leading-relaxed">
                            {item.op.description}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">
                        <FieldChips
                          fields={paramList(docs.data, item.op)}
                          empty="—"
                        />
                      </TableCell>
                      <TableCell className="pr-6 text-xs">
                        <FieldChips
                          fields={bodyFields(item.op)}
                          empty={t("common.no")}
                        />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        ))}

        {groups.length === 0 && <EmptyState>{t("docs.empty")}</EmptyState>}
      </QueryState>
    </div>
  );
}
