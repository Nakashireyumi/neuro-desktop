/**
 * Neurcro dashboard API client.
 *
 * The dashboard is served by the Go bridge itself (http://127.0.0.1:8300/ui/),
 * so every call is same-origin and relative. When the UI is served from the Vite
 * dev server, `vite.config.ts` proxies /api and /health to the bridge.
 *
 * Every API call needs the dashboard token, including reads and including calls
 * from this machine. The operator gets the token from `neuro-integration setup`
 * and types it into the sign-in page. It is kept in sessionStorage for this
 * browser session only, and it is sent in the X-ND-Token header. The bridge never
 * puts it in a page, and it is not read from the URL.
 */

export type ScopeName =
  | "input"
  | "game"
  | "filesystem"
  | "process"
  | "network"
  | "system"
  | "vision"
  | "shell"
  | "extensions";

export type ScopeConfig = {
  allowed: boolean;
  /** Neuro may ask the operator to switch this scope on (request_permission). */
  requestable?: boolean;
  limits?: {
    /** 0 or missing means unlimited. Enforced by the bridge. */
    max_actions_per_minute?: number;
  };
};

export type PermissionPolicyFile = {
  version?: string | number;
  default_allow: boolean;
  allowed_actions: string[];
  denied_actions: string[];
  scopes: Partial<Record<ScopeName, ScopeConfig>>;
  created_at?: string;
  metadata?: Record<string, string>;
};

export type BootstrapInfo = {
  version?: string;
  api_base?: string;
  nativeHost?: boolean;
  platform?: string;
};

export type ExecutorInfo = {
  connected: boolean;
  remote?: string;
  version?: string;
  completed_commands: number;
  failed_commands: number;
  last_error?: string;
  last_seen?: string;
  total_connections: number;
};

export type BridgeStats = {
  actions_received?: number;
  actions_completed?: number;
  actions_failed?: number;
  actions_denied?: number;
  last_action?: string;
  last_error?: string;
};

export type RelayStatus = {
  enabled: boolean;
  url?: string;
  connected: boolean;
  registered: boolean;
  peer_count: number;
  peers?: Record<string, string>;
  last_error?: string;
  last_event?: string;
  last_seen?: string;
  reserved_actions?: string[];
};

export type GameProfileSummary = {
  id: string;
  name: string;
  description?: string;
  mode: string;
  external?: string;
  keys?: Record<string, string>;
  mouse_look: boolean;
  vision: boolean;
  launchable: boolean;
  tags?: string[];
};

export type GameSession = {
  profile_id: string;
  profile_name: string;
  control_mode: string;
  external_integration?: string;
  started_at: string;
  actions_issued: number;
  last_action?: string;
  last_action_at?: string;
};

export type GameDetected = {
  profile?: GameProfileSummary;
  matched_on?: string;
  active_window?: string;
  process_candidates?: string[];
};

export type StatusPayload = {
  ok: boolean;
  version: string;
  integration: string;
  uptime: number;
  started_at: string;
  executor: ExecutorInfo;
  negotiation: {
    executor_connected: boolean;
    file_ipc_path: string;
    protocol_version: string;
  };
  permissions_path: string;
  game: {
    registry_source: string;
    profiles: number;
    session: GameSession | null;
  };
  relay: RelayStatus;
  actions: BridgeStats;
  control: ControlState;
  neuro: NeuroConnection;
  admin: {
    listen: string;
    token_required: boolean;
    token_configured?: boolean;
    token_source?: string;
    requests: number;
    last_error?: string;
  };
};

export type NeuroConnection = {
  connected: boolean;
  state: string;
  attempts: number;
  last_error?: string;
  connected_at?: string;
};

export type ControlState = {
  paused: boolean;
  kill_switch_file?: string;
  kill_switch_active?: boolean;
};

export type AuditEntry = {
  time: string;
  event: string;
  action?: string;
  decision?: string;
  reason?: string;
  paused?: boolean;
  source?: string;
};

export type AuditPayload = {
  enabled: boolean;
  entries: AuditEntry[];
  truncated?: boolean;
  message?: string;
};

export type CatalogItem = {
  id: string;
  name: string;
  description?: string;
  type?: string;
  repository?: string;
  homepage?: string;
  tags?: string[];
  launch_hints?: string[];
  publisher?: string;
  commit?: string;
  /** verified | unsigned | untrusted | invalid (see catalog_trust.go). */
  signature_state?: string;
  signature_detail?: string;
  mcp?: { command: string; args?: string[] };
  permissions?: ExtensionPermissionDeclaration[];
  settings_schema?: SettingsSchema;
};

export type InstalledExtension = {
  id: string;
  enabled: boolean;
  installed_at?: string;
  source?: string;
  path?: string;
  name?: string;
  description?: string;
  type?: string;
  repository?: string;
  trust?: string;
  signature_state?: string;
};

export type PermissionRequest = {
  id: string;
  scope: ScopeName;
  reason: string;
  minutes: number;
  status: "pending" | "approved" | "denied";
  granted_minutes?: number;
  requested_at: string;
  decided_at?: string;
  granted_until?: string;
  note?: string;
};

export type PermissionGrant = {
  scope: ScopeName;
  granted_at: string;
  /** Empty or missing means the grant lasts until it is revoked. */
  expires_at?: string;
  reason?: string;
  request_id?: string;
};

export type PermissionRequestsPayload = {
  pending: PermissionRequest[];
  recent: PermissionRequest[];
  grants: PermissionGrant[];
  requestable: ScopeName[];
};

export type RuntimeState = {
  state: string;
  [key: string]: unknown;
};

export type RuntimePayload = {
  ok: boolean;
  checked_at: string;
  neuro: NeuroConnection;
  bridge: RuntimeState & { uptime_seconds: number; admin_listen?: string; paused?: boolean };
  executor: RuntimeState & { connected: boolean };
  relay: RuntimeState & { enabled: boolean; url?: string; peer_count: number; last_error?: string };
  vision: RuntimeState & { configured: boolean; url?: string; backend?: string; latency_ms?: number; error?: string };
  mcp: RuntimeState & {
    running: number;
    servers: { id: string; state: string; tools?: string[]; last_error?: string; trust?: string }[];
  };
};

export type ExtensionsPayload = {
  ok: boolean;
  installed: InstalledExtension[];
  catalog: CatalogItem[];
  install_mode: string;
  extension_dir: string;
  state_file: string;
  built_in_services?: { vision?: VisionServiceState };
};

export type VisionServiceSettings = {
  enabled: boolean;
  actors: { neuro: boolean; evil: boolean };
};

export type VisionServiceState = VisionServiceSettings & {
  persona: "neuro" | "evil" | "shared";
  effective: boolean;
  configured: boolean;
  reason?: string;
  endpoint?: string;
};

export type VisionServicePayload = { ok: boolean; vision: VisionServiceState };

export type ManagerSession = {
  ok: boolean;
  version: string;
  token_source: string;
  role: "operator" | "neuro";
  expires_at?: string;
};

export type ExtensionPermissionDeclaration = {
  id: string;
  description: string;
  required: boolean;
};

export type SettingsSchema = {
  type?: string;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  properties?: Record<string, SettingsSchema>;
  required?: string[];
  additionalProperties?: boolean;
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  writeOnly?: boolean;
  secret?: boolean;
};

export type ExtensionPermissionPolicy = {
  manage_allowed: boolean;
  run_allowed: boolean;
  grants: Record<string, boolean>;
  settings: Record<string, unknown>;
};

export type ExtensionPolicyPayload = {
  ok: boolean;
  id: string;
  policy: ExtensionPermissionPolicy;
  permissions: ExtensionPermissionDeclaration[];
  settings_schema?: SettingsSchema;
  legacy?: boolean;
};

export type IntegrationManifest = {
  schema_version: number;
  id: string;
  name: string;
  version: string;
  kind: string;
  game_ids?: string[];
  neuro_api?: { game_name?: string };
  permissions: ExtensionPermissionDeclaration[];
  settings_schema?: SettingsSchema;
};

export type ConfiguredIntegration = {
  id: string;
  manifest: IntegrationManifest;
  connected: boolean;
  state: string;
  message?: string;
  last_seen?: string;
  viewer_visible?: boolean;
  token_configured?: boolean;
};

export type MarketplaceItem = {
  id: string;
  name: string;
  kind: string;
  description: string;
  repository: string;
  source_ref: string;
  source_commit: string;
  game_ids: string[];
  license: { spdx: string; status: string; notes: string; url: string };
  platforms: { documented: string[]; tested: string[]; notes: string };
  install: { mode: string; readiness: string; instructions_url: string; requirements: string[]; notes: string[] };
  permissions: { capabilities: { id: string; reason: string }[]; enforcement: string };
  verification: { reviewed_at: string; unverified: string[] };
  source_staged?: boolean;
  can_stage_source?: boolean;
};

export type MarketplacePayload = {
  ok: boolean;
  items: MarketplaceItem[];
  integrations: ConfiguredIntegration[];
  schema_version: number;
  path: string;
};

export type ActionInfo = {
  name: string;
  scope: string;
  description?: string;
  allowed: boolean;
  registered: boolean;
  reserved: boolean;
  kind?: string;
};

export type ActionsPayload = {
  actions: ActionInfo[];
  registered: string[];
  reserved: string[];
  registered_count: number;
  reserved_count: number;
};

export type PermissionSchema = {
  scopes: (ScopeName | { name: ScopeName; title?: string; description?: string })[];
  actions: { name: string; scope: string; kind?: string }[];
  defaults: { default_allow: boolean };
};

export type ConfigPayload = {
  ok: boolean;
  version: string;
  paths: Record<string, string>;
  features: Record<string, boolean>;
};

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

const TOKEN_KEY = "nd_admin_token";
export const AUTH_REQUIRED_EVENT = "nd:auth-required";
// Keep sign-in usable when the browser blocks sessionStorage.
let memoryToken = "";

function bootstrap(): BootstrapInfo | undefined {
  return window.__ND_BOOTSTRAP;
}

/** The token the operator signed in with, for this browser session only. */
export function adminToken(): string {
  try {
    return window.sessionStorage.getItem(TOKEN_KEY) ?? memoryToken;
  } catch {
    return memoryToken;
  }
}

export function setAdminToken(token: string) {
  memoryToken = token;
  try {
    if (token) {
      window.sessionStorage.setItem(TOKEN_KEY, token);
    } else {
      window.sessionStorage.removeItem(TOKEN_KEY);
    }
  } catch {
    // Storage can be unavailable (private mode); the token just won't persist.
  }
}

/** Same-origin by default; NEURO_UI_API / __ND_BOOTSTRAP.api_base override it. */
export const API_BASE = (bootstrap()?.api_base ?? "").replace(/\/$/, "");

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const token = adminToken();
  if (token) {
    headers.set("X-ND-Token", token);
  }

  const controller = new AbortController();
  const abort = () => controller.abort();
  init.signal?.addEventListener("abort", abort, { once: true });
  if (init.signal?.aborted) controller.abort();
  // Extension downloads can outlast a status read. Do not cut them off at the
  // polling timeout and invite a duplicate install.
  const mutation = init.method !== undefined && !["GET", "HEAD"].includes(init.method.toUpperCase());
  const timeout = window.setTimeout(abort, mutation ? 120000 : 15000);
  try {
    const response = await fetch(`${API_BASE}${path}`, { ...init, headers, signal: controller.signal });
    const text = await response.text();
    let payload: any = null;
    if (text) {
      try {
        payload = JSON.parse(text);
      } catch {
        if (response.ok) throw new ApiError(response.status, "The bridge returned an invalid JSON response.");
        payload = { error: text };
      }
    }

    if (!response.ok) {
      if (response.status === 401 && token && token === adminToken()) {
        setAdminToken("");
        window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT));
      }
      const message =
        (payload && (payload.error || payload.message)) || `Request failed (HTTP ${response.status})`;
      throw new ApiError(response.status, message);
    }

    return payload as T;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw new ApiError(0, controller.signal.aborted
      ? "The bridge did not respond in time. Refresh the dashboard to check whether the request completed."
      : `Cannot reach the Neurcro bridge (${path}). Is it running?`);
  } finally {
    window.clearTimeout(timeout);
    init.signal?.removeEventListener("abort", abort);
  }
}

function post<T>(path: string, body?: unknown, headers?: HeadersInit): Promise<T> {
  return request<T>(path, {
    method: "POST",
    body: body === undefined ? "" : JSON.stringify(body),
    headers,
  });
}

/** External metadata links never execute a non-HTTPS URL or carry credentials. */
export function safeExternalURL(value?: string): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.username || url.password) return undefined;
    return url.href;
  } catch {
    return undefined;
  }
}

function operatorHeaders(operatorToken: string): HeadersInit {
  const fresh = operatorToken.trim();
  if (!fresh) throw new ApiError(400, "Re-enter your operator token for this approval.");
  // This value must come from a new password entry. Never use adminToken here.
  return { "X-ND-Operator-Token": fresh };
}

export const api = {
  /** Checks the token without reading anything else. 401 means it is wrong; 503 means setup has not run. */
  session: () => request<ManagerSession>("/api/session"),
  status: () => request<StatusPayload>("/api/status"),
  config: () => request<ConfigPayload>("/api/config"),
  pause: () => post<{ ok: boolean; paused: boolean; message: string }>("/api/control/pause"),
  resume: () => post<{ ok: boolean; paused: boolean; message: string }>("/api/control/resume"),
  audit: () => request<AuditPayload>("/api/audit?limit=100"),

  permissions: () => request<PermissionPolicyFile>("/api/permissions"),
  permissionSchema: () => request<PermissionSchema>("/api/permissions/schema"),
  savePermissions: (policy: PermissionPolicyFile, operatorToken: string) =>
    request<{ ok: boolean; saved: string; applied_live: boolean }>("/api/permissions", {
      method: "PUT",
      headers: operatorHeaders(operatorToken),
      body: JSON.stringify(policy),
    }),

  actions: () => request<ActionsPayload>("/api/actions"),

  extensions: () => request<ExtensionsPayload>("/api/extensions"),
  visionService: () => request<VisionServicePayload>("/api/services/vision"),
  saveVisionService: (settings: VisionServiceSettings, operatorToken: string, signal?: AbortSignal) =>
    request<VisionServicePayload>("/api/services/vision", {
      method: "PUT", headers: operatorHeaders(operatorToken), body: JSON.stringify(settings), signal,
    }),
  extensionPolicy: (id: string, signal?: AbortSignal) =>
    request<ExtensionPolicyPayload>(`/api/extensions/${encodeURIComponent(id)}/permissions`, { signal }),
  saveExtensionPolicy: (id: string, policy: ExtensionPermissionPolicy, operatorToken: string) =>
    request<ExtensionPolicyPayload>(`/api/extensions/${encodeURIComponent(id)}/permissions`, {
      method: "PUT", headers: operatorHeaders(operatorToken), body: JSON.stringify(policy),
    }),
  extensionAction: (id: string, action: "install" | "enable" | "disable" | "uninstall", operatorToken?: string) =>
    post<{ ok: boolean; message?: string }>(
      `/api/extensions/${encodeURIComponent(id)}/${action}`,
      undefined,
      operatorToken?.trim() ? operatorHeaders(operatorToken) : undefined,
    ),

  marketplace: (signal?: AbortSignal) => request<MarketplacePayload>("/api/marketplace", { signal }),
  stageMarketplaceSource: (id: string) =>
    post<{ ok: boolean; message?: string }>(`/api/marketplace/${encodeURIComponent(id)}/stage`),

  games: () =>
    request<{
      ok: boolean;
      profiles: GameProfileSummary[];
      registry_source: string;
      detected: GameDetected | null;
      session: GameSession | null;
    }>("/api/games"),

  startSession: (profileId: string, launch: boolean, mode: string) =>
    post<{ ok: boolean; message: string; session: GameSession | null }>("/api/games/session", {
      profile_id: profileId,
      launch,
      mode,
    }),

  endSession: () =>
    request<{ ok: boolean; message: string }>("/api/games/session", { method: "DELETE" }),

  releaseInput: () => post<{ ok: boolean; message: string }>("/api/games/release"),

  observe: (vision = true) =>
    post<{ ok: boolean; observation: string }>("/api/games/observe", { vision }),

  relay: () => request<{ ok: boolean; relay: RelayStatus }>("/api/relay"),

  runtime: () => request<RuntimePayload>("/api/runtime"),

  permissionRequests: (signal?: AbortSignal) => request<PermissionRequestsPayload>("/api/permission-requests", { signal }),
  decidePermissionRequest: (id: string, approve: boolean, minutes: number | undefined, operatorToken: string) =>
    post<{ ok: boolean; message?: string }>(
      `/api/permission-requests/${encodeURIComponent(id)}/${approve ? "approve" : "deny"}`,
      approve && minutes !== undefined ? { minutes } : {},
      operatorHeaders(operatorToken),
    ),
  revokePermissionGrant: (scope: ScopeName, operatorToken: string) =>
    post<{ ok: boolean; message?: string }>(
      `/api/permissions/grants/${encodeURIComponent(scope)}/revoke`,
      undefined,
      operatorHeaders(operatorToken),
    ),
};
