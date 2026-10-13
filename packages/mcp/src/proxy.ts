export function createProxy(
  env: Record<string, string | undefined>,
  fetcher: typeof fetch = fetch,
) {
  const url = new URL(env.CMS_MCP_URL ?? ""),
    project = env.CMS_PROJECT,
    token = env.CMS_AGENT_TOKEN;
  if (
    url.pathname !== "/mcp" ||
    url.search ||
    url.hash ||
    url.username ||
    url.password ||
    !(
      url.protocol === "https:" ||
      (url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname))
    ) ||
    !/^[a-z0-9-]{2,64}$/.test(project ?? "") ||
    !/^cms_agt_[A-Za-z0-9_-]+$/.test(token ?? "")
  )
    throw new Error("MCP_CONFIG_INVALID");
  return async function forward(line: string): Promise<string | undefined> {
    let id: unknown = null;
    try {
      if (Buffer.byteLength(line) > 1024 * 1024) throw new Error();
      const request = JSON.parse(line) as { id?: unknown; jsonrpc?: unknown; method?: unknown };
      if (!request || request.jsonrpc !== "2.0" || typeof request.method !== "string")
        throw new Error();
      id = request.id ?? null;
      const response = await fetcher(url, {
        method: "POST",
        redirect: "error",
        signal: AbortSignal.timeout(30000),
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json, text/event-stream",
          Authorization: "Bearer " + token,
          "X-CMS-Project": project!,
          "MCP-Protocol-Version": "2025-06-18",
        },
        body: line,
      });
      if (response.status === 202 && request.id === undefined) return undefined;
      if (
        !response.ok ||
        !response.headers.get("content-type")?.startsWith("application/json") ||
        !response.body
      )
        throw new Error();
      const reader = response.body.getReader();
      let size = 0,
        text = "";
      const decoder = new TextDecoder();
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          size += value.length;
          if (size > 1024 * 1024) throw new Error();
          text += decoder.decode(value, { stream: true });
        }
        text += decoder.decode();
      } finally {
        await reader.cancel().catch(() => {});
        reader.releaseLock();
      }
      const result = JSON.parse(text) as { jsonrpc?: unknown; id?: unknown };
      if (result.jsonrpc !== "2.0" || result.id !== id) throw new Error();
      return JSON.stringify(result);
    } catch {
      return JSON.stringify({
        jsonrpc: "2.0",
        id,
        error: { code: -32000, message: "CMS MCP request failed" },
      });
    }
  };
}
