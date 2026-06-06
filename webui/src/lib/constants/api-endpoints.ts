export const API_MODELS = {
	LIST: '/v1/models',
	LOAD: '/models/load',
	UNLOAD: '/models/unload'
};

// chat completion routes, the control route drives realtime inference (e.g. end reasoning)
export const API_CHAT = {
	// E2E fork：聊天完成改打 proxy 的 E2E 端點（原生為 './v1/chat/completions'）。
	// P2 先明文串流；P3 由 proxy 解密入/加密出、ChatService 加密送/解密收。
	COMPLETIONS: './api/e2e/chat',
	CONTROL: './v1/chat/completions/control'
};

// slot introspection, requires the --slots flag on the server
export const API_SLOTS = {
	LIST: './slots'
};

export const API_TOOLS = {
	LIST: '/tools',
	EXECUTE: '/tools'
};

/** CORS proxy endpoint path */
export const CORS_PROXY_ENDPOINT = '/cors-proxy';
