/// <reference types="vite-plugin-svgr/client" />
/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_READY_GATE_HOLD?: string;
  readonly VITE_ROUTER_DEVTOOLS?: string;
}
