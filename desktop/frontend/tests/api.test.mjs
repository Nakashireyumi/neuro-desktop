import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

const storage = new Map();
const events = new EventTarget();
globalThis.window = Object.assign(events, {
  sessionStorage: {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  },
  setTimeout,
  clearTimeout,
});
const originalStorage = window.sessionStorage;
const originalFetch = globalThis.fetch;
const { request, api, safeExternalURL, adminToken, setAdminToken, ApiError, AUTH_REQUIRED_EVENT } = await import("../src/api.ts");
afterEach(() => {
  window.sessionStorage = originalStorage;
  setAdminToken("");
  globalThis.fetch = originalFetch;
  window.setTimeout = setTimeout;
});

test("sign-in works when session storage is blocked, and sign-out clears the fallback", async () => {
  window.sessionStorage = {
    getItem() { throw new Error("blocked"); },
    setItem() { throw new Error("blocked"); },
    removeItem() { throw new Error("blocked"); },
  };
  setAdminToken("private-session-token");
  globalThis.fetch = async (_url, init) => {
    assert.equal(init.headers.get("X-ND-Token"), "private-session-token");
    return Response.json({ ok: true });
  };
  assert.deepEqual(await request("/api/session"), { ok: true });
  setAdminToken("");
  assert.equal(adminToken(), "");
});

test("expired tokens are cleared and notify the app once", async () => {
  setAdminToken("expired");
  let notifications = 0;
  const notify = () => notifications++;
  events.addEventListener(AUTH_REQUIRED_EVENT, notify);
  try {
    globalThis.fetch = async () => Response.json({ error: "invalid token" }, { status: 401 });
    await assert.rejects(request("/api/status"), (error) => error instanceof ApiError && error.status === 401);
    await assert.rejects(request("/api/status"));
    assert.equal(adminToken(), "");
    assert.equal(notifications, 1);
  } finally { events.removeEventListener(AUTH_REQUIRED_EVENT, notify); }
});

test("an old rejected request cannot clear a newer sign-in", async () => {
  setAdminToken("old-token");
  globalThis.fetch = async () => {
    setAdminToken("new-token");
    return Response.json({ error: "invalid token" }, { status: 401 });
  };
  await assert.rejects(request("/api/status"));
  assert.equal(adminToken(), "new-token");
});

test("network and malformed response failures are actionable API errors", async () => {
  globalThis.fetch = async () => { throw new TypeError("offline"); };
  await assert.rejects(request("/api/status"), /Cannot reach the Neurcro bridge/);
  globalThis.fetch = async () => new Response("<html>proxy error</html>");
  await assert.rejects(request("/api/status"), /invalid JSON response/);
});

test("caller cancellation aborts a pending request and releases the timeout", async () => {
  globalThis.fetch = async (_url, init) => new Promise((_resolve, reject) => {
    init.signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
  });
  const controller = new AbortController();
  const pending = request("/api/status", { signal: controller.signal });
  controller.abort();
  await assert.rejects(pending, (error) => error instanceof ApiError && error.status === 0);
});

test("stalled reads time out, while mutations receive a longer deadline", async () => {
  const deadlines = [];
  window.setTimeout = (callback, delay) => {
    deadlines.push(delay);
    return setTimeout(callback, 1);
  };
  globalThis.fetch = async (_url, init) => new Promise((_resolve, reject) => {
    init.signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
  });
  await assert.rejects(request("/api/status"), /check whether the request completed/);
  await assert.rejects(request("/api/extensions/memory/install", { method: "POST" }));
  assert.deepEqual(deadlines, [15000, 120000]);
});

test("extension approvals use a freshly entered token separately from session authentication", async () => {
  setAdminToken("signed-in-session");
  const policy = { manage_allowed: true, run_allowed: false, grants: { network: true }, settings: { endpoint: "local" } };
  let calls = 0;
  globalThis.fetch = async (url, init) => {
    calls++;
    assert.equal(url, "/api/extensions/example/permissions");
    assert.equal(init.method, "PUT");
    assert.equal(init.headers.get("X-ND-Token"), "signed-in-session");
    assert.equal(init.headers.get("X-ND-Operator-Token"), "fresh-approval-entry");
    assert.deepEqual(JSON.parse(init.body), policy);
    return Response.json({ ok: true, policy });
  };
  assert.throws(() => api.saveExtensionPolicy("example", policy, "   "), /Re-enter your operator token/);
  assert.equal(calls, 0);
  await api.saveExtensionPolicy("example", policy, " fresh-approval-entry ");
  assert.equal(calls, 1);
  assert.equal(adminToken(), "signed-in-session");
});

test("global permission writes and decisions all require an explicit fresh approval header", async () => {
  setAdminToken("session-only");
  const seen = [];
  globalThis.fetch = async (url, init) => {
    seen.push(url);
    assert.equal(init.headers.get("X-ND-Token"), "session-only");
    assert.equal(init.headers.get("X-ND-Operator-Token"), "newly-entered");
    return Response.json({ ok: true });
  };
  await api.savePermissions({ default_allow: false, allowed_actions: [], denied_actions: [], scopes: {} }, "newly-entered");
  await api.decidePermissionRequest("request/with spaces", true, 5, "newly-entered");
  await api.revokePermissionGrant("extensions", "newly-entered");
  assert.deepEqual(seen, ["/api/permissions", "/api/permission-requests/request%2Fwith%20spaces/approve", "/api/permissions/grants/extensions/revoke"]);
});

test("built-in vision settings require fresh approval and send persona switches without changing session authority", async () => {
  setAdminToken("operator-session");
  const settings = { enabled: false, actors: { neuro: true, evil: false } };
  const snapshot = { ...settings, persona: "shared", configured: true, effective: false, reason: "Vision Service is disabled." };
  const seen = [];
  globalThis.fetch = async (url, init) => {
    seen.push([url, init.method]);
    assert.equal(url, "/api/services/vision");
    assert.equal(init.headers.get("X-ND-Token"), "operator-session");
    if (init.method === "PUT") {
      assert.equal(init.headers.get("X-ND-Operator-Token"), "fresh-vision-approval");
      assert.deepEqual(JSON.parse(init.body), settings);
    } else {
      assert.equal(init.headers.get("X-ND-Operator-Token"), null);
    }
    return Response.json({ ok: true, vision: snapshot });
  };
  assert.throws(() => api.saveVisionService(settings, " "), /Re-enter your operator token/);
  assert.deepEqual(seen, []);
  assert.deepEqual(await api.visionService(), { ok: true, vision: snapshot });
  assert.deepEqual(await api.saveVisionService(settings, " fresh-vision-approval "), { ok: true, vision: snapshot });
  assert.deepEqual(seen, [["/api/services/vision", undefined], ["/api/services/vision", "PUT"]]);
  assert.equal(adminToken(), "operator-session");
});

test("source downloads and delegated extension actions do not invent operator authority", async () => {
  setAdminToken("delegated-session");
  const seen = [];
  globalThis.fetch = async (url, init) => {
    seen.push(url);
    assert.equal(init.headers.get("X-ND-Token"), "delegated-session");
    assert.equal(init.headers.get("X-ND-Operator-Token"), null);
    return Response.json({ ok: true });
  };
  await api.stageMarketplaceSource("some/source");
  await api.extensionAction("memory", "enable");
  assert.deepEqual(seen, ["/api/marketplace/some%2Fsource/stage", "/api/extensions/memory/enable"]);
});

test("external marketplace links accept HTTPS and reject executable or credential-bearing URLs", () => {
  assert.equal(safeExternalURL("https://github.com/example/repo"), "https://github.com/example/repo");
  for (const value of ["javascript:alert(1)", "file:///tmp/token", "https://token@github.com/example/repo", "not a URL"]) {
    assert.equal(safeExternalURL(value), undefined);
  }
});
