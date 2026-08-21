// This route handler is itself a BFF call: the browser only ever talks to
// this Next.js route (same-origin, no CORS involved), and this route talks
// server-to-server to the Go service's aggregation endpoints. It measures
// both the concurrent (`/bff/aggregate`) and serial (`/bff/aggregate-serial`)
// paths so the frontend can show the real, live latency gap between them.

const GO_API_BASE_URL = process.env.GO_API_BASE_URL || "http://127.0.0.1:8090";

async function timedFetch(path: string) {
  const start = Date.now();
  const res = await fetch(`${GO_API_BASE_URL}${path}`, { cache: "no-store" });
  const data = await res.json();
  return { ok: res.ok, data, ms: Date.now() - start };
}

export async function GET() {
  const [parallel, serial] = await Promise.all([
    timedFetch("/bff/aggregate"),
    timedFetch("/bff/aggregate-serial"),
  ]);

  return Response.json({ parallel, serial });
}
