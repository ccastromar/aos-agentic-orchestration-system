// SSO for AOS — Authorization Code + PKCE against the shared Chestertown IdP (any OIDC provider:
// Keycloak, Entra, Dex…; client aos-spa). Endpoints come from the issuer's discovery document.
// On return it stores the same `aosAuth` shape the rest of the app already uses
// ({token, userId, roles}), so api.js sends the IdP's token as a bearer and the AOS backend
// validates it via JWKS. Opt-in: enabled when VITE_OIDC_ISSUER is set.
import { getStoredAuth, saveStoredAuth } from './authStorage';

const ISSUER = (import.meta.env.VITE_OIDC_ISSUER || '').replace(/\/$/, '');
const CLIENT_ID = import.meta.env.VITE_OIDC_CLIENT_ID || 'aos-spa';
// Some IdPs only put roles or groups in the token when asked (Dex: `groups`).
const SCOPE = (import.meta.env.VITE_OIDC_SCOPE || '').trim() || 'openid profile email';

export const ssoEnabled = Boolean(ISSUER && CLIENT_ID);

// The issuer's endpoints, read once from its discovery document. A failed read is not cached,
// so the next attempt tries again.
let discovery = null;
function endpoints() {
  if (!discovery) {
    discovery = fetch(`${ISSUER}/.well-known/openid-configuration`)
      .then((res) => {
        if (!res.ok) throw new Error(`OIDC discovery failed (${res.status})`);
        return res.json();
      })
      .then((d) => {
        if (!d.authorization_endpoint || !d.token_endpoint) throw new Error('OIDC discovery is missing endpoints');
        return d;
      })
      .catch((err) => {
        discovery = null;
        throw err;
      });
  }
  return discovery;
}

// The username/role modal exists only to exercise the backend's deliberately insecure
// development authenticator. Do not expose it merely because OIDC is absent: it must be
// explicitly enabled in a local Vite build and is never available in a production build.
export const devLoginEnabled = import.meta.env.DEV && import.meta.env.VITE_ENABLE_DEV_LOGIN === 'true';

const VERIFIER_KEY = 'aos_pkce_verifier';
const STATE_KEY = 'aos_pkce_state';

const b64url = (bytes) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const rand = (n = 32) => { const a = new Uint8Array(n); crypto.getRandomValues(a); return b64url(a); };
const challenge = async (v) => b64url(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(v))));
const redirectUri = () => `${window.location.origin}/`;

function decodeJwt(t) {
  try { const p = t.split('.')[1].replace(/-/g, '+').replace(/_/g, '/'); return JSON.parse(atob(p + '==='.slice((p.length + 3) % 4))); }
  catch { return {}; }
}

const SILENT_TRIED_KEY = 'aos_silent_tried';
const FORCE_LOGIN_KEY = 'aos_force_login';
const ID_TOKEN_KEY = 'aos_id_token';
// Explicit sign-out: the next sign-in must ask for credentials instead of reusing the IdP session.
// It also ends the IdP session (RP-initiated logout) so the whole suite signs out.
// An IdP without end_session_endpoint (Dex) only gets the local sign-out.
export async function markSignedOut() {
  const idToken = sessionStorage.getItem(ID_TOKEN_KEY) || '';
  sessionStorage.removeItem(ID_TOKEN_KEY);
  sessionStorage.setItem(FORCE_LOGIN_KEY, 'true');
  if (!ssoEnabled) return;
  let endSession;
  try { endSession = (await endpoints()).end_session_endpoint; } catch { return; }
  if (!endSession) return;
  const p = new URLSearchParams({ client_id: CLIENT_ID, post_logout_redirect_uri: redirectUri() });
  if (idToken) p.set('id_token_hint', idToken);
  window.location.assign(`${endSession}?${p}`);
}

export async function ssoLogin(silent = false) {
  if (!ssoEnabled) return;
  const { authorization_endpoint: authorize } = await endpoints();
  const verifier = rand(48), state = rand();
  sessionStorage.setItem(VERIFIER_KEY, verifier);
  sessionStorage.setItem(STATE_KEY, state);
  const p = new URLSearchParams({
    response_type: 'code', client_id: CLIENT_ID, redirect_uri: redirectUri(),
    scope: SCOPE, state, code_challenge: await challenge(verifier), code_challenge_method: 'S256',
  });
  if (silent === true) p.set('prompt', 'none');
  else if (sessionStorage.getItem(FORCE_LOGIN_KEY)) {
    p.set('prompt', 'login');
    sessionStorage.removeItem(FORCE_LOGIN_KEY);
  }
  window.location.assign(`${authorize}?${p}`);
}

// Joins an existing IdP session (opened by another app of the suite) without showing a login.
// Once per tab, and never right after an explicit sign-out. Returns true when it redirects.
export async function trySilentSso() {
  if (!ssoEnabled) return false;
  const params = new URLSearchParams(window.location.search);
  if (params.has('code') || params.has('error')) return false;
  if (sessionStorage.getItem(SILENT_TRIED_KEY) || sessionStorage.getItem(FORCE_LOGIN_KEY)) return false;
  sessionStorage.setItem(SILENT_TRIED_KEY, 'true');
  try { await ssoLogin(true); return true; } catch { return false; }
}

// If the URL carries ?code, exchange it and persist aosAuth. Returns the auth object or null.
export async function completeSso() {
  const url = new URL(window.location.href);
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  // The IdP session this sign-in belongs to (OIDC `session_state`; Keycloak and Entra send it).
  const session = url.searchParams.get('session_state');
  if (!code) {
    // prompt=none came back without an IdP session: just show the sign-in card.
    if (url.searchParams.has('error')) {
      sessionStorage.removeItem(VERIFIER_KEY); sessionStorage.removeItem(STATE_KEY);
      window.history.replaceState({}, '', url.origin + url.pathname);
    }
    return null;
  }
  const verifier = sessionStorage.getItem(VERIFIER_KEY);
  const expected = sessionStorage.getItem(STATE_KEY);
  sessionStorage.removeItem(VERIFIER_KEY); sessionStorage.removeItem(STATE_KEY);
  window.history.replaceState({}, '', url.origin + url.pathname);
  if (!verifier || !state || state !== expected) return null;

  const body = new URLSearchParams({
    grant_type: 'authorization_code', code, redirect_uri: redirectUri(), client_id: CLIENT_ID, code_verifier: verifier,
  });
  const res = await fetch((await endpoints()).token_endpoint, {
    method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body,
  });
  if (!res.ok) return null;
  const tok = await res.json();
  // AOS validates the SPA client audience. Keycloak puts that audience in the ID token for a
  // public PKCE client, whereas an access token's audience is normally `account` unless an
  // extra audience mapper is installed.
  if (tok.id_token) sessionStorage.setItem(ID_TOKEN_KEY, tok.id_token);
  const bearer = tok.id_token || tok.access_token;
  if (!bearer) return null;
  const c = decodeJwt(bearer);
  const auth = { token: bearer, userId: c.preferred_username || c.email || c.sub, roles: c.roles || [], session };
  saveStoredAuth(auth);
  return auth;
}

const CHECK_PREFIX = 'idp-check:';
const RENEW_WINDOW_MS = 120000;

/** Seconds-precision JWT expiry in ms, or 0 when unreadable. */
function jwtExpiryMs(token) {
  try {
    const part = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
    return (JSON.parse(atob(part + '==='.slice((part.length + 3) % 4))).exp ?? 0) * 1000;
  } catch {
    return 0;
  }
}

/** Runs inside the hidden iframe: reports the prompt=none outcome to the parent and stops. */
export function reportIdpSessionIfFramed() {
  if (window.parent === window) return false;
  const params = new URLSearchParams(window.location.search);
  const state = params.get('state') ?? '';
  if (!state.startsWith(CHECK_PREFIX)) return false;
  window.parent.postMessage(
    {
      type: 'idp-check',
      state,
      active: !params.has('error'),
      code: params.get('code'),
      session: params.get('session_state'),
    },
    window.location.origin,
  );
  return true;
}


function idpCheck(
  authorizationEndpoint,
  clientId,
  redirectUri,
  challenge,
) {
  return new Promise((resolve) => {
    const state =
      CHECK_PREFIX +
      Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('');
    const iframe = document.createElement('iframe');
    iframe.style.display = 'none';
    const finish = (value) => {
      window.removeEventListener('message', onMessage);
      clearTimeout(timer);
      iframe.remove();
      resolve(value);
    };
    const onMessage = (event) => {
      if (event.origin !== window.location.origin || event.data?.type !== 'idp-check' || event.data.state !== state) return;
      finish({ active: Boolean(event.data.active), code: event.data.code ?? null, session: event.data.session ?? null });
    };
    const timer = setTimeout(() => finish(null), 8000);
    window.addEventListener('message', onMessage);
    const url = new URL(authorizationEndpoint);
    url.search = new URLSearchParams({
      client_id: clientId,
      response_type: 'code',
      scope: SCOPE,
      redirect_uri: redirectUri,
      prompt: 'none',
      state,
      code_challenge: challenge,
      code_challenge_method: 'S256',
    }).toString();
    iframe.src = url.toString();
    document.body.appendChild(iframe);
  });
}


/**
 * While this app holds a session, asks the IdP now and then (and when the tab regains focus)
 * whether the SSO session is alive. A sign-out done in another app of the suite ends ours; a
 * token about to expire is renewed silently from the same check, so users are not bounced.
 */
export function watchIdpSession(w) {
  let busy = false;
  const check = async () => {
    if (busy || document.hidden || !w.hasSession()) return;
    busy = true;
    try {
      const { verifier, challenge } = await w.pkce();
      const result = await idpCheck(await w.authorizationEndpoint(), w.clientId, w.redirectUri, challenge);
      if (!result || !w.hasSession()) return;
      // No IdP session, or another one (someone else signed in after a sign-out elsewhere).
      const known = w.session();
      const switched = Boolean(known && result.session && result.session !== known);
      if (!result.active || switched) w.onEnded();
      else if (result.code && w.expiresAt() - Date.now() < RENEW_WINDOW_MS) await w.exchange(result.code, verifier, result.session);
    } catch {
      /* transient: try again later */
    } finally {
      busy = false;
    }
  };
  const interval = setInterval(check, 30000);
  document.addEventListener('visibilitychange', check);
  window.addEventListener('focus', check);
  return () => {
    clearInterval(interval);
    document.removeEventListener('visibilitychange', check);
    window.removeEventListener('focus', check);
  };
}

async function renewWithCode(code, verifier, session) {
  const res = await fetch((await endpoints()).token_endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code', code, redirect_uri: redirectUri(), client_id: CLIENT_ID, code_verifier: verifier,
    }),
  });
  if (!res.ok) return;
  const tok = await res.json();
  if (tok.id_token) sessionStorage.setItem(ID_TOKEN_KEY, tok.id_token);
  const bearer = tok.id_token || tok.access_token;
  if (!bearer) return;
  const c = decodeJwt(bearer);
  saveStoredAuth({
    token: bearer,
    userId: c.preferred_username || c.email || c.sub,
    roles: c.roles || [],
    session: session || getStoredAuth()?.session || null,
  });
}

export const watchAosSession = (hasSession, onEnded) =>
  watchIdpSession({
    authorizationEndpoint: async () => (await endpoints()).authorization_endpoint,
    clientId: CLIENT_ID,
    redirectUri: redirectUri(),
    hasSession,
    session: () => getStoredAuth()?.session || null,
    expiresAt: () => jwtExpiryMs(getStoredAuth()?.token || ''),
    pkce: async () => {
      const verifier = rand(48);
      return { verifier, challenge: await challenge(verifier) };
    },
    exchange: renewWithCode,
    onEnded,
  });
