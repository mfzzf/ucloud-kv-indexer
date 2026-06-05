const HOP_BY_HOP_HEADERS = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

const RESPONSE_DROP_HEADERS = new Set([
  ...HOP_BY_HOP_HEADERS,
  "content-length",
  "content-encoding",
]);

type RouteContext = {
  params: Promise<{ path?: string[] }> | { path?: string[] };
};

function gatewayBase(): string {
  return (process.env.KVI_API_BASE || "http://127.0.0.1:8095").replace(/\/$/, "");
}

async function targetURL(req: Request, ctx: RouteContext): Promise<string> {
  const params = await ctx.params;
  const path = (params.path || []).map(encodeURIComponent).join("/");
  const incoming = new URL(req.url);
  return `${gatewayBase()}/${path}${incoming.search}`;
}

function forwardHeaders(req: Request): Headers {
  const headers = new Headers(req.headers);
  for (const key of headers.keys()) {
    if (HOP_BY_HOP_HEADERS.has(key.toLowerCase()) || key.toLowerCase() === "host") {
      headers.delete(key);
    }
  }
  return headers;
}

function responseHeaders(upstream: Response): Headers {
  const headers = new Headers(upstream.headers);
  for (const key of headers.keys()) {
    if (RESPONSE_DROP_HEADERS.has(key.toLowerCase())) {
      headers.delete(key);
    }
  }
  headers.set("cache-control", "no-store");
  headers.set("x-accel-buffering", "no");
  return headers;
}

async function proxy(req: Request, ctx: RouteContext): Promise<Response> {
  const method = req.method.toUpperCase();
  const init: RequestInit = {
    method,
    headers: forwardHeaders(req),
    cache: "no-store",
    redirect: "manual",
  };
  if (method !== "GET" && method !== "HEAD") {
    init.body = await req.arrayBuffer();
  }
  const upstream = await fetch(await targetURL(req, ctx), init);
  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: responseHeaders(upstream),
  });
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

export function GET(req: Request, ctx: RouteContext) {
  return proxy(req, ctx);
}

export function POST(req: Request, ctx: RouteContext) {
  return proxy(req, ctx);
}

export function PUT(req: Request, ctx: RouteContext) {
  return proxy(req, ctx);
}

export function PATCH(req: Request, ctx: RouteContext) {
  return proxy(req, ctx);
}

export function DELETE(req: Request, ctx: RouteContext) {
  return proxy(req, ctx);
}
