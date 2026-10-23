// Shared type declarations for the Neuro Desktop dashboard.
//
// The dashboard is a plain web app served by the Go bridge; the only native
// affordance left is the optional bootstrap object the bridge injects into the
// shell (see api.ts). `window.ndHost` was the old keystore bridge and is only
// kept so a cached bundle cannot throw while it is being replaced.

export type NativeBridge = {
  send: (event: string, payload?: unknown) => void;
};

export type ExtensionState = {
  installed: boolean;
  enabled: boolean;
};

export type NDBootstrap = {
  version?: string;
  api_base?: string;
  nativeHost?: boolean;
  platform?: string;
  extensionState?: Record<string, ExtensionState>;
};

declare global {
  interface Window {
    ndHost?: NativeBridge;
    __ND_BOOTSTRAP?: NDBootstrap;
  }
}

export {};
