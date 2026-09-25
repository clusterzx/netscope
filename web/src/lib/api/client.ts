// Typed JSON client for the NetScope API (/api/v1).
//
//   import { api, ApiError } from '$lib/api';
//   const dev  = await api.get('/api/v1/devices/{id}', { path: { id } });
//   const list = await api.get('/api/v1/devices', { query: { q: 'tag:iot', limit: 50 } });
//   await api.post('/api/v1/devices/bulk', { body: { action: 'add_tags', ids, tags: ['x'] } });
//
// Paths, query parameters, bodies and responses are checked against ApiPaths (generated from
// /api/openapi.json). Endpoints that are not (yet) in the spec go through api.raw.*.
// Mutating requests carry the CSRF header; a 401 redirects to /login?next=<current path>, a
// 403 of a session that has to finish its setup (password, second factor) to /setup
// (unless `auth: false`). Failures throw ApiError (status, code, message, fields).
import { goto } from '$app/navigation';
import type { ApiPaths, ApiUploadResponse } from './generated';

export type FieldError = { field: string; message: string };

/** Error thrown for every failed request (HTTP error, network error, invalid JSON). */
export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly fields: FieldError[];
	constructor(status: number, code: string, message: string, fields: FieldError[] = []) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
		this.fields = fields;
	}
	/** Field errors as a map field → message (first message per field). */
	get fieldMap(): Record<string, string> {
		const out: Record<string, string> = {};
		for (const f of this.fields) if (!(f.field in out)) out[f.field] = f.message;
		return out;
	}
	get isNotFound() {
		return this.status === 404;
	}
}

/** Human readable message for any thrown value. */
export function errorMessage(e: unknown): string {
	if (e instanceof ApiError) return e.message;
	if (e instanceof Error) return e.message;
	return String(e);
}

/** Field errors of an ApiError as map (empty for other errors). */
export function fieldErrors(e: unknown): Record<string, string> {
	return e instanceof ApiError ? e.fieldMap : {};
}

export type QueryValue = string | number | boolean | null | undefined | Array<string | number>;
export type Query = Record<string, QueryValue>;

/** Builds "?a=1&b=x" (skips null/undefined/"" and joins arrays with commas). */
export function toQueryString(query?: Query): string {
	if (!query) return '';
	const sp = new URLSearchParams();
	for (const [k, v] of Object.entries(query)) {
		if (v === undefined || v === null || v === '') continue;
		if (Array.isArray(v)) {
			if (v.length) sp.set(k, v.join(','));
		} else if (typeof v === 'boolean') {
			sp.set(k, v ? '1' : '0');
		} else sp.set(k, String(v));
	}
	const s = sp.toString();
	return s ? '?' + s : '';
}

type Method = 'get' | 'post' | 'put' | 'patch' | 'delete';

type ParamNames<S extends string> = S extends `${string}{${infer N}}${infer R}` ? N | ParamNames<R> : never;

type PathsWith<M extends Method> = {
	[P in keyof ApiPaths]: ApiPaths[P] extends Record<M, unknown> ? P : never;
}[keyof ApiPaths];

type OpOf<P extends keyof ApiPaths, M extends Method> =
	ApiPaths[P] extends Record<M, infer O extends { query: unknown; body: unknown; response: unknown }>
		? O
		: never;

export type RequestOptions = {
	signal?: AbortSignal;
	/** false: do not redirect to /login on 401 (e.g. the login page itself). */
	auth?: boolean;
	/** fetch implementation (pass the `fetch` of a SvelteKit load function) */
	fetch?: typeof fetch;
};

type Opts<P extends string, O extends { query: unknown; body: unknown }> = RequestOptions &
	([ParamNames<P>] extends [never]
		? { path?: undefined }
		: { path: Record<ParamNames<P>, string | number> }) &
	([O['query']] extends [never] ? { query?: undefined } : { query?: O['query'] }) &
	([O['body']] extends [never] ? { body?: undefined } : { body: O['body'] });

type Args<P extends string, O extends { query: unknown; body: unknown }> = [ParamNames<P>] extends [never]
	? [O['body']] extends [never]
		? [opts?: Opts<P, O>]
		: [opts: Opts<P, O>]
	: [opts: Opts<P, O>];

/** Response type of an endpoint, e.g. Response<'/api/v1/devices', 'get'>. */
export type Response<P extends keyof ApiPaths, M extends Method = 'get'> = OpOf<P, M>['response'];
/** Request body type of an endpoint. */
export type Body<P extends keyof ApiPaths, M extends Method> = OpOf<P, M>['body'];

function fillPath(path: string, params?: Record<string, string | number>): string {
	return path.replace(/\{([a-zA-Z_]+)\}/g, (_, name: string) => {
		const v = params?.[name];
		if (v === undefined || v === '') throw new Error(`Pfadparameter ${name} fehlt für ${path}`);
		return encodeURIComponent(String(v));
	});
}

let redirecting = false;

function redirectToLogin() {
	if (redirecting || typeof window === 'undefined') return;
	const here = window.location.pathname + window.location.search;
	if (window.location.pathname === '/login') return;
	redirecting = true;
	goto('/login?next=' + encodeURIComponent(here), { replaceState: true }).finally(
		() => (redirecting = false)
	);
}

function redirectToSetup() {
	if (redirecting || typeof window === 'undefined' || window.location.pathname === '/setup') return;
	redirecting = true;
	goto('/setup', { replaceState: true }).finally(() => (redirecting = false));
}

async function parseError(res: globalThis.Response): Promise<ApiError> {
	let code = 'http_' + res.status;
	let message = res.statusText || `HTTP ${res.status}`;
	let fields: FieldError[] = [];
	try {
		const j = await res.json();
		if (j && typeof j === 'object' && j.error) {
			code = j.error.code ?? code;
			message = j.error.message ?? message;
			fields = Array.isArray(j.error.fields) ? j.error.fields : [];
		}
	} catch {
		// non-JSON error body
	}
	if (res.status === 502 || res.status === 503 || res.status === 504) {
		if (!fields.length && code.startsWith('http_')) message = 'Server nicht erreichbar (' + res.status + ')';
	}
	return new ApiError(res.status, code, message, fields);
}

/**
 * Low level request. `url` is a full path ("/api/v1/..."), body is JSON-encoded unless it
 * is FormData. Returns parsed JSON, a Blob for non-JSON responses or undefined for 204.
 */
export async function request<T>(
	method: string,
	url: string,
	opts: RequestOptions & { query?: Query; body?: unknown } = {}
): Promise<T> {
	const m = method.toUpperCase();
	const headers: Record<string, string> = { Accept: 'application/json' };
	let body: BodyInit | undefined;
	if (m !== 'GET' && m !== 'HEAD') headers['X-NetScope-CSRF'] = '1';
	if (opts.body instanceof FormData) body = opts.body;
	else if (opts.body !== undefined) {
		headers['Content-Type'] = 'application/json';
		body = JSON.stringify(opts.body);
	}
	let res: globalThis.Response;
	try {
		res = await (opts.fetch ?? fetch)(url + toQueryString(opts.query), {
			method: m,
			headers,
			body,
			credentials: 'same-origin',
			signal: opts.signal
		});
	} catch (e) {
		if (e instanceof DOMException && e.name === 'AbortError') throw e;
		throw new ApiError(0, 'network', 'Server nicht erreichbar – Netzwerkfehler');
	}
	if (!res.ok) {
		const err = await parseError(res);
		if (res.status === 401 && opts.auth !== false) redirectToLogin();
		// the session first has to change its start password or set up a second factor
		if (res.status === 403 && (err.code === 'password_change_required' || err.code === 'mfa_setup_required'))
			redirectToSetup();
		throw err;
	}
	if (res.status === 204) return undefined as T;
	const ct = res.headers.get('Content-Type') ?? '';
	if (ct.includes('application/json')) {
		try {
			return (await res.json()) as T;
		} catch {
			throw new ApiError(res.status, 'invalid_json', 'Ungültige Antwort vom Server');
		}
	}
	return (await res.blob()) as T;
}

function typed<M extends Method>(method: M) {
	return <P extends PathsWith<M>>(path: P, ...args: Args<P, OpOf<P, M>>): Promise<OpOf<P, M>['response']> => {
		const o = (args[0] ?? {}) as RequestOptions & {
			path?: Record<string, string | number>;
			query?: Query;
			body?: unknown;
		};
		return request(method, fillPath(path, o.path), o);
	};
}

/** Untyped escape hatch for endpoints that are not in the OpenAPI spec (yet). */
const raw = {
	get: <T>(url: string, query?: Query, opts?: RequestOptions) => request<T>('GET', url, { ...opts, query }),
	post: <T>(url: string, body?: unknown, opts?: RequestOptions) => request<T>('POST', url, { ...opts, body }),
	put: <T>(url: string, body?: unknown, opts?: RequestOptions) => request<T>('PUT', url, { ...opts, body }),
	patch: <T>(url: string, body?: unknown, opts?: RequestOptions) =>
		request<T>('PATCH', url, { ...opts, body }),
	delete: <T>(url: string, opts?: RequestOptions) => request<T>('DELETE', url, opts)
};

/** Uploads a file (multipart field "file") to /api/v1/uploads and returns its server path. */
async function upload(file: File, opts?: RequestOptions): Promise<ApiUploadResponse> {
	const fd = new FormData();
	fd.append('file', file, file.name);
	return request<ApiUploadResponse>('POST', '/api/v1/uploads', { ...opts, body: fd });
}

/** Downloads an endpoint response as file (uses the filename from Content-Disposition). */
async function download(url: string, query?: Query, fallbackName = 'download'): Promise<void> {
	const res = await fetch(url + toQueryString(query), { credentials: 'same-origin' });
	if (!res.ok) {
		const err = await parseError(res);
		if (res.status === 401) redirectToLogin();
		throw err;
	}
	const cd = res.headers.get('Content-Disposition') ?? '';
	const name = /filename="?([^";]+)"?/.exec(cd)?.[1] ?? fallbackName;
	const blob = await res.blob();
	const href = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = href;
	a.download = name;
	document.body.appendChild(a);
	a.click();
	a.remove();
	setTimeout(() => URL.revokeObjectURL(href), 1000);
}

export const api = {
	get: typed('get'),
	post: typed('post'),
	put: typed('put'),
	patch: typed('patch'),
	delete: typed('delete'),
	raw,
	upload,
	download,
	/** URL with query string, e.g. for <a href> downloads. */
	url: (path: string, query?: Query) => path + toQueryString(query)
};
