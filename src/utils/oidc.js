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
// Explicit sign-out: the next sign-in must ask for credentials instead of reusing the IdP session.
export const markSignedOut = () => sessionStorage.setItem(FORCE_LOGIN_KEY, 'true');

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
  const bearer = tok.id_token || tok.access_token;
  if (!bearer) return null;
  const c = decodeJwt(bearer);
  const auth = { token: bearer, userId: c.preferred_username || c.email || c.sub, roles: c.roles || [] };
  saveStoredAuth(auth);
  return auth;
}
