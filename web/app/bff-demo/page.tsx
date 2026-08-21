"use client";

import { useState } from "react";

type TimedResult = {
  ok: boolean;
  data: unknown;
  ms: number;
};

type ComparisonResponse = {
  parallel: TimedResult;
  serial: TimedResult;
};

export default function BffDemoPage() {
  const [result, setResult] = useState<ComparisonResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const runComparison = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/bff-aggregate", { cache: "no-store" });
      if (!res.ok) throw new Error(`request failed: ${res.status}`);
      setResult(await res.json());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const maxMs = result ? Math.max(result.parallel.ms, result.serial.ms) : 0;

  return (
    <div className="flex flex-col flex-1 items-center bg-zinc-50 font-sans dark:bg-black">
      <main className="flex w-full max-w-2xl flex-col gap-8 py-24 px-6">
        <div className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold text-black dark:text-zinc-50">
            BFF 并发聚合 demo
          </h1>
          <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-400">
            这个页面调用 Next.js 的{" "}
            <code className="rounded bg-black/[.06] px-1 py-0.5 font-mono text-[0.85em] dark:bg-white/[.08]">
              /api/bff-aggregate
            </code>
            ，它在服务端分别请求 Go 后端的并发版{" "}
            <code className="rounded bg-black/[.06] px-1 py-0.5 font-mono text-[0.85em] dark:bg-white/[.08]">
              /bff/aggregate
            </code>{" "}
            和串行版{" "}
            <code className="rounded bg-black/[.06] px-1 py-0.5 font-mono text-[0.85em] dark:bg-white/[.08]">
              /bff/aggregate-serial
            </code>
            （各聚合 3 个各带 150ms 延迟的模拟上游），并把两边的真实耗时一起返回。
          </p>
        </div>

        <button
          onClick={runComparison}
          disabled={loading}
          className="w-fit rounded-full bg-foreground px-5 py-3 text-sm font-medium text-background transition-colors hover:bg-[#383838] disabled:opacity-50 dark:hover:bg-[#ccc]"
        >
          {loading ? "请求中..." : "运行对比"}
        </button>

        {error && (
          <p className="text-sm text-red-600 dark:text-red-400">
            出错了：{error}
          </p>
        )}

        {result && (
          <div className="flex flex-col gap-4">
            <Bar label="并发版 /bff/aggregate" ms={result.parallel.ms} maxMs={maxMs} accent />
            <Bar label="串行版 /bff/aggregate-serial" ms={result.serial.ms} maxMs={maxMs} />
            <p className="text-sm text-zinc-600 dark:text-zinc-400">
              并发版比串行版快了约{" "}
              <span className="font-semibold text-black dark:text-zinc-50">
                {Math.round(
                  ((result.serial.ms - result.parallel.ms) / result.serial.ms) * 100,
                )}
                %
              </span>
              。
            </p>
          </div>
        )}
      </main>
    </div>
  );
}

function Bar({
  label,
  ms,
  maxMs,
  accent,
}: {
  label: string;
  ms: number;
  maxMs: number;
  accent?: boolean;
}) {
  const widthPct = maxMs > 0 ? Math.max((ms / maxMs) * 100, 4) : 0;
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between text-sm">
        <span className="text-zinc-700 dark:text-zinc-300">{label}</span>
        <span className="font-mono text-zinc-500 dark:text-zinc-500">{ms}ms</span>
      </div>
      <div className="h-3 w-full rounded-full bg-black/[.06] dark:bg-white/[.08]">
        <div
          className={`h-3 rounded-full transition-all ${
            accent ? "bg-emerald-500" : "bg-zinc-400 dark:bg-zinc-600"
          }`}
          style={{ width: `${widthPct}%` }}
        />
      </div>
    </div>
  );
}
