// Shared type declarations for Neuro Desktop frontend

export type NativeBridge = {
  send: (event: string, payload?: unknown) => void;
};

export type ExtensionState = {
  installed: boolean;
  enabled: boolean;
};

export type NDBootstrap = {
  extensionState?: Record<string, ExtensionState>;
  nativeHost?: boolean;
  platform?: string;
};

declare global {
  interface Window {
    ndHost?: NativeBridge;
    __ND_BOOTSTRAP?: NDBootstrap;
  }
}
