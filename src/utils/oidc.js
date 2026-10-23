// SSO for AOS — Authorization Code + PKCE against the shared Chestertown IdP (Keycloak, client
// aos-spa). On return it stores the same `aosAuth` shape the rest of the app already uses
// ({token, userId, roles}), so api.js sends the Keycloak access token as a bearer and the
// AOS backend validates it via JWKS. Opt-in: enabled when VITE_OIDC_ISSUER is set.
import { saveStoredAuth } from './authStorage';

const ISSUER = (import.meta.env.VITE_OIDC_ISSUER || '').replace(/\/$/, '');
const CLIENT_ID = import.meta.env.VITE_OIDC_CLIENT_ID || 'aos-spa';

export const ssoEnabled = Boolean(ISSUER && CLIENT_ID);

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

const FORCE_LOGIN_KEY = 'aos_force_login';
const ID_TOKEN_KEY = 'aos_id_token';
// Explicit sign-out: the next sign-in must ask for credentials instead of reusing the IdP session.
// It also ends the IdP session (RP-initiated logout) so the whole suite signs out.
export function markSignedOut() {
  const idToken = sessionStorage.getItem(ID_TOKEN_KEY) || '';
  sessionStorage.removeItem(ID_TOKEN_KEY);
  sessionStorage.setItem(FORCE_LOGIN_KEY, 'true');
  if (!ssoEnabled) return;
  const p = new URLSearchParams({ client_id: CLIENT_ID, post_logout_redirect_uri: redirectUri() });
  if (idToken) p.set('id_token_hint', idToken);
  window.location.assign(`${ISSUER}/protocol/openid-connect/logout?${p}`);
}

export async function ssoLogin() {
  if (!ssoEnabled) return;
  const verifier = rand(48), state = rand();
  sessionStorage.setItem(VERIFIER_KEY, verifier);
  sessionStorage.setItem(STATE_KEY, state);
  const p = new URLSearchParams({
    response_type: 'code', client_id: CLIENT_ID, redirect_uri: redirectUri(),
    scope: 'openid profile email', state, code_challenge: await challenge(verifier), code_challenge_method: 'S256',
  });
  if (sessionStorage.getItem(FORCE_LOGIN_KEY)) {
    p.set('prompt', 'login');
    sessionStorage.removeItem(FORCE_LOGIN_KEY);
  }
  window.location.assign(`${ISSUER}/protocol/openid-connect/auth?${p}`);
}

// If the URL carries ?code, exchange it and persist aosAuth. Returns the auth object or null.
export async function completeSso() {
  const url = new URL(window.location.href);
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  if (!code) return null;
  const verifier = sessionStorage.getItem(VERIFIER_KEY);
  const expected = sessionStorage.getItem(STATE_KEY);
  sessionStorage.removeItem(VERIFIER_KEY); sessionStorage.removeItem(STATE_KEY);
  window.history.replaceState({}, '', url.origin + url.pathname);
  if (!verifier || !state || state !== expected) return null;

  const body = new URLSearchParams({
    grant_type: 'authorization_code', code, redirect_uri: redirectUri(), client_id: CLIENT_ID, code_verifier: verifier,
  });
  const res = await fetch(`${ISSUER}/protocol/openid-connect/token`, {
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
  const auth = { token: bearer, userId: c.preferred_username || c.email || c.sub, roles: c.roles || [] };
  saveStoredAuth(auth);
  return auth;
}

const SILENT_STATE = 'idp-session-check';
// Any well-formed S256 challenge: the code is never exchanged, only the prompt=none outcome matters.
const DUMMY_CHALLENGE = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";

/** Runs inside the hidden iframe: reports the prompt=none outcome to the parent and stops. */
export function reportIdpSessionIfFramed() {
  if (window.parent === window) return false;
  const params = new URLSearchParams(window.location.search);
  if (params.get("state") !== SILENT_STATE) return false;
  window.parent.postMessage(
    { type: SILENT_STATE, active: !params.has("error") },
    window.location.origin,
  );
  return true;
}

function idpSessionActive(authorizationEndpoint, clientId, redirectUri) {
  return new Promise((resolve) => {
    const iframe = document.createElement("iframe");
    iframe.style.display = "none";
    const finish = (value) => {
      window.removeEventListener("message", onMessage);
      clearTimeout(timer);
      iframe.remove();
      resolve(value);
    };
    const onMessage = (event) => {
      if (event.origin !== window.location.origin || event.data?.type !== SILENT_STATE) return;
      finish(Boolean(event.data.active));
    };
    const timer = setTimeout(() => finish(null), 8000);
    window.addEventListener("message", onMessage);
    const url = new URL(authorizationEndpoint);
    url.search = new URLSearchParams({
      client_id: clientId,
      response_type: "code",
      scope: "openid",
      redirect_uri: redirectUri,
      prompt: "none",
      state: SILENT_STATE,
      code_challenge: DUMMY_CHALLENGE,
      code_challenge_method: "S256",
    }).toString();
    iframe.src = url.toString();
    document.body.appendChild(iframe);
  });
}

/**
 * Detects a sign-out done in another app of the suite: while this app holds a session, it asks
 * the IdP now and then (and when the tab regains focus) whether its own session is still alive.
 */
export function watchIdpSession(
  authorizationEndpoint,
  clientId,
  redirectUri,
  hasSession,
  onEnded,
) {
  let busy = false;
  const check = async () => {
    if (busy || document.hidden || !hasSession()) return;
    busy = true;
    try {
      const active = await idpSessionActive(await authorizationEndpoint(), clientId, redirectUri);
      if (active === false && hasSession()) onEnded();
    } catch {
      /* transient: try again later */
    } finally {
      busy = false;
    }
  };
  const interval = setInterval(check, 30000);
  document.addEventListener("visibilitychange", check);
  window.addEventListener("focus", check);
  return () => {
    clearInterval(interval);
    document.removeEventListener("visibilitychange", check);
    window.removeEventListener("focus", check);
  };
}

export const watchAosSession = (hasSession, onEnded) =>
  watchIdpSession(
    async () => `${ISSUER}/protocol/openid-connect/auth`,
    CLIENT_ID,
    redirectUri(),
    hasSession,
    onEnded,
  );
