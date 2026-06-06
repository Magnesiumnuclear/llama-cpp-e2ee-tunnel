/**
 * E2E fork: 聊天請求/回應的端到端加密（Web Crypto API）。
 *
 * 請求：產生一次性 AES-256 金鑰 K → AES-GCM 加密 OpenAI 請求 body → RSA-OAEP 加密 K，
 *       送出信封 { encrypted_key, iv, ciphertext } 到 proxy 的 /api/e2e/chat。
 * 回應：proxy 用同一把 K 逐塊 AES-GCM 加密成 SSE 幀「data: b64(iv).b64(ct)\n\n」；
 *       此處解密還原成 llama.cpp 原始位元流，交回原本的 SSE 解析器（渲染/推理邏輯不動）。
 *
 * 認證靠 HttpOnly cookie（authMiddleware 先驗），故無 HMAC、不需 device_secret。
 * 僅加密「聊天內容」；其餘 metadata 端點（/props、/v1/models …）維持明文。
 */

const PUBLIC_KEY_URL = './api/public-key';

let cachedPubKey: CryptoKey | null = null;

function b64encode(buf: ArrayBuffer): string {
	const bytes = new Uint8Array(buf);
	let s = '';
	for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
	return btoa(s);
}

function b64decode(s: string): Uint8Array {
	const bin = atob(s);
	const out = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
	return out;
}

/** 取得並快取伺服器 RSA-OAEP 公鑰（SPKI PEM → CryptoKey）。 */
async function getServerPublicKey(): Promise<CryptoKey> {
	if (cachedPubKey) return cachedPubKey;
	const resp = await fetch(PUBLIC_KEY_URL);
	if (!resp.ok) throw new Error('無法取得伺服器 E2E 公鑰');
	const json = await resp.json();
	const pem: string = json?.data?.public_key ?? '';
	const b64 = pem
		.replace(/-----BEGIN PUBLIC KEY-----/, '')
		.replace(/-----END PUBLIC KEY-----/, '')
		.replace(/\s+/g, '');
	const der = b64decode(b64);
	cachedPubKey = await crypto.subtle.importKey(
		'spki',
		der,
		{ name: 'RSA-OAEP', hash: 'SHA-256' },
		false,
		['encrypt']
	);
	return cachedPubKey;
}

/** 加密 OpenAI 請求 body，回傳信封與本次的 AES 金鑰 K（用於解密回應）。 */
async function encryptRequest(bodyObj: unknown): Promise<{ envelope: object; key: CryptoKey }> {
	const pub = await getServerPublicKey();
	const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, true, [
		'encrypt',
		'decrypt'
	]);
	const rawKey = await crypto.subtle.exportKey('raw', key);
	const iv = crypto.getRandomValues(new Uint8Array(12));
	const plaintext = new TextEncoder().encode(JSON.stringify(bodyObj));
	const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, plaintext);
	const encryptedKey = await crypto.subtle.encrypt({ name: 'RSA-OAEP' }, pub, rawKey);
	return {
		envelope: {
			encrypted_key: b64encode(encryptedKey),
			iv: b64encode(iv.buffer),
			ciphertext: b64encode(ciphertext)
		},
		key
	};
}

/**
 * 把「加密 SSE 幀串流」轉成「解密後的原始位元流」。
 * 每幀格式：data: base64(iv).base64(ciphertext+tag)\n\n
 */
function decryptFrameStream(src: ReadableStream<Uint8Array>, key: CryptoKey): ReadableStream<Uint8Array> {
	const reader = src.getReader();
	const decoder = new TextDecoder();
	let buffer = '';
	return new ReadableStream<Uint8Array>({
		async pull(controller) {
			while (true) {
				const { done, value } = await reader.read();
				if (done) {
					controller.close();
					return;
				}
				buffer += decoder.decode(value, { stream: true });
				let produced = false;
				let idx: number;
				while ((idx = buffer.indexOf('\n\n')) !== -1) {
					let line = buffer.slice(0, idx);
					buffer = buffer.slice(idx + 2);
					if (line.startsWith('data: ')) line = line.slice(6);
					else if (line.startsWith('data:')) line = line.slice(5);
					line = line.trim();
					if (!line) continue;
					const dot = line.indexOf('.');
					if (dot === -1) continue;
					const iv = b64decode(line.slice(0, dot));
					const ct = b64decode(line.slice(dot + 1));
					let plain: ArrayBuffer;
					try {
						plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ct);
					} catch (e) {
						controller.error(e);
						return;
					}
					controller.enqueue(new Uint8Array(plain));
					produced = true;
				}
				if (produced) return;
			}
		},
		cancel(reason) {
			reader.cancel(reason);
		}
	});
}

/**
 * 加密版 fetch：加密 body 送到 E2E 端點；成功時回傳「回應已解密」的 Response，
 * 呼叫端可照常讀 response.body（SSE）或 response.json()。
 * 失敗（非 2xx）時原樣回傳明文 Response，交給呼叫端既有錯誤處理。
 */
export async function e2eFetch(url: string, bodyObj: unknown, signal?: AbortSignal): Promise<Response> {
	const { envelope, key } = await encryptRequest(bodyObj);
	const resp = await fetch(url, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(envelope),
		signal
	});
	if (!resp.ok || !resp.body) return resp;
	const decrypted = decryptFrameStream(resp.body, key);
	const headers = new Headers();
	headers.set('Content-Type', resp.headers.get('Content-Type') || 'text/event-stream');
	return new Response(decrypted, { status: resp.status, statusText: resp.statusText, headers });
}
