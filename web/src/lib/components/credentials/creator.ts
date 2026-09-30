// Creating credentials right at a credential-ref field (plugin forms, setup wizard). A
// <CredentialCreator> around a form provides the dialog; CredentialField then shows
// "Neu anlegen" and selects the new credential. Without a provider nothing changes.
import { getContext, setContext } from 'svelte';
import type { Credential } from '$lib/api';

/** Opens the dialog for the given credential types; done receives the new credential. */
export type CredentialCreator = (types: string[], done: (c: Credential) => void) => void;

const KEY = Symbol('credential-creator');

export function provideCredentialCreator(fn: CredentialCreator) {
	setContext(KEY, fn);
}

export function credentialCreator(): CredentialCreator | undefined {
	return getContext<CredentialCreator | undefined>(KEY);
}
