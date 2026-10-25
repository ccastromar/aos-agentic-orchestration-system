import { devLoginEnabled } from './oidc';
import { clearStoredAuth, getStoredAuth, saveStoredAuth } from './authStorage';

export const getApiUrl = () => {
  // Empty string = same origin: requests stay relative and the Vite dev
  // proxy forwards them to the backend (AOS_BACKEND_URL in vite.config.js).
  // Set aosApiUrl in Settings only to bypass the proxy with an absolute URL.
  return localStorage.getItem('aosApiUrl') || '';
};

// --- Auth (dev login against POST /auth/login) ---

export const getAuth = () => getStoredAuth();

export const clearAuth = () => clearStoredAuth();

export const login = async (username, password, roles = []) => {
  const res = await fetch(`${getApiUrl()}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, ...(roles.length ? { roles } : {}) })
  });
  if (!res.ok) {
    throw new Error(`Login failed (${res.status})`);
  }
  const data = await res.json();
  const auth = { token: data.token, userId: data.user_id, roles: data.roles };
  saveStoredAuth(auth);
  return auth;
};

export const apiFetch = async (endpoint, options = {}) => {
  const url = endpoint.startsWith('http') ? endpoint : `${getApiUrl()}${endpoint.startsWith('/') ? endpoint : '/' + endpoint}`;
  const auth = getAuth();

  const headers = { ...options.headers };
  if (auth?.token) {
    headers['Authorization'] = `Bearer ${auth.token}`;
  } else if (devLoginEnabled) {
    // Anonymous dev mode only: the backend maps this to the demo-user identity
    headers['X-Mock-Role'] = localStorage.getItem('aosMockRole') || 'admin';
  }

  const res = await fetch(url, { ...options, headers });
  if (res.status === 401) {
    // Missing or expired session: drop the stale token and let App send the user to sign in.
    clearAuth();
    window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT));
  }
  if (res.status === 403 && auth?.token) {
    // Signed in but without a role AOS maps: App explains it instead of a raw API error.
    const text = await res.clone().text().catch(() => '');
    // The backend refuses a token that grants no governance function in AOS (older builds: no role).
    if (text.includes('no governance functions assigned') || text.includes('no roles assigned')) {
      window.dispatchEvent(new CustomEvent(NO_ROLES_EVENT));
    }
  }
  return res;
};

export const UNAUTHORIZED_EVENT = 'aos:unauthorized';
export const NO_ROLES_EVENT = 'aos:no-roles';

// A prompt typed while signed out survives the sign-in round trip and is restored into the input.
const PENDING_PROMPT_KEY = 'aosPendingPrompt';
export const savePendingPrompt = (text) => sessionStorage.setItem(PENDING_PROMPT_KEY, text);
export const takePendingPrompt = () => {
  const text = sessionStorage.getItem(PENDING_PROMPT_KEY) || '';
  sessionStorage.removeItem(PENDING_PROMPT_KEY);
  return text;
};
