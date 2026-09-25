// Passkeys (WebAuthn) in the browser. The server sends the options with base64url strings
// (go-webauthn); navigator.credentials wants ArrayBuffers, and the answer goes back as JSON
// with base64url strings again.
//
//   if (passkeysSupported()) {
//     const credential = await createPasskey(await api.post('/api/v1/auth/passkeys/options'));
//     await api.post('/api/v1/auth/passkeys', { body: { name, credential } });
//   }

/** Browsers offer passkeys only in a secure context (HTTPS or localhost). */
export function passkeysSupported(): boolean {
	return (
		typeof window !== 'undefined' &&
		window.isSecureContext &&
		typeof window.PublicKeyCredential === 'function'
	);
}

function fromB64url(s: string): ArrayBuffer {
	const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4);
	const bin = atob(b64);
	const out = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
	return out.buffer;
}

function toB64url(buf: ArrayBuffer | null | undefined): string | undefined {
	if (!buf) return undefined;
	const bytes = new Uint8Array(buf);
	let bin = '';
	for (const b of bytes) bin += String.fromCharCode(b);
	return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

type Json = Record<string, unknown>;
type Descriptor = { id: string; type: string; transports?: string[] };

function descriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
	if (!Array.isArray(list)) return undefined;
	return (list as Descriptor[]).map((d) => ({
		...d,
		id: fromB64url(d.id),
		type: 'public-key',
		transports: d.transports as AuthenticatorTransport[] | undefined
	}));
}

function publicKey(options: unknown): Json {
	const pk = (options as { publicKey?: Json } | null)?.publicKey;
	if (!pk) throw new Error('Ungültige Passkey-Optionen vom Server');
	return pk;
}

function cancelled(e: unknown): Error {
	if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError'))
		return new Error('Vorgang abgebrochen oder Zeit abgelaufen');
	if (e instanceof DOMException && e.name === 'InvalidStateError')
		return new Error('Dieser Passkey ist bereits registriert');
	return e instanceof Error ? e : new Error(String(e));
}

/** Registers a passkey with the options of POST /api/v1/auth/passkeys/options. */
export async function createPasskey(options: unknown): Promise<Json> {
	const pk = publicKey(options);
	const user = pk.user as Json;
	let cred: PublicKeyCredential;
	try {
		cred = (await navigator.credentials.create({
			publicKey: {
				...(pk as unknown as PublicKeyCredentialCreationOptions),
				challenge: fromB64url(pk.challenge as string),
				user: { ...(user as unknown as PublicKeyCredentialUserEntity), id: fromB64url(user.id as string) },
				excludeCredentials: descriptors(pk.excludeCredentials)
			}
		})) as PublicKeyCredential;
	} catch (e) {
		throw cancelled(e);
	}
	const res = cred.response as AuthenticatorAttestationResponse;
	return {
		id: cred.id,
		rawId: toB64url(cred.rawId),
		type: cred.type,
		authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
		clientExtensionResults: cred.getClientExtensionResults(),
		response: {
			clientDataJSON: toB64url(res.clientDataJSON),
			attestationObject: toB64url(res.attestationObject),
			transports: typeof res.getTransports === 'function' ? res.getTransports() : undefined
		}
	};
}

/** Signs in with a passkey using the options of POST /api/v1/auth/login/passkey/options. */
export async function getPasskey(options: unknown): Promise<Json> {
	const pk = publicKey(options);
	let cred: PublicKeyCredential;
	try {
		cred = (await navigator.credentials.get({
			publicKey: {
				...(pk as unknown as PublicKeyCredentialRequestOptions),
				challenge: fromB64url(pk.challenge as string),
				allowCredentials: descriptors(pk.allowCredentials)
			}
		})) as PublicKeyCredential;
	} catch (e) {
		throw cancelled(e);
	}
	const res = cred.response as AuthenticatorAssertionResponse;
	return {
		id: cred.id,
		rawId: toB64url(cred.rawId),
		type: cred.type,
		authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
		clientExtensionResults: cred.getClientExtensionResults(),
		response: {
			clientDataJSON: toB64url(res.clientDataJSON),
			authenticatorData: toB64url(res.authenticatorData),
			signature: toB64url(res.signature),
			userHandle: toB64url(res.userHandle)
		}
	};
}
