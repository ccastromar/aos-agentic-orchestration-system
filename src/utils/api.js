export const getApiUrl = () => {
  return localStorage.getItem('aosApiUrl') || 'http://localhost:8080';
};

// --- Auth (dev login against POST /auth/login) ---

export const getAuth = () => {
  try {
    return JSON.parse(localStorage.getItem('aosAuth')) || null;
  } catch {
    return null;
  }
};

export const clearAuth = () => localStorage.removeItem('aosAuth');

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
  localStorage.setItem('aosAuth', JSON.stringify(auth));
  return auth;
};

export const apiFetch = async (endpoint, options = {}) => {
  const url = endpoint.startsWith('http') ? endpoint : `${getApiUrl()}${endpoint.startsWith('/') ? endpoint : '/' + endpoint}`;
  const auth = getAuth();

  const headers = { ...options.headers };
  if (auth?.token) {
    headers['Authorization'] = `Bearer ${auth.token}`;
  } else {
    // Anonymous dev mode: the backend maps this to the demo-user identity
    headers['X-Mock-Role'] = localStorage.getItem('aosMockRole') || 'admin';
  }

  return fetch(url, { ...options, headers });
};
