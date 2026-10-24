const AUTH_KEY = 'aosAuth';

const parseAuth = (value) => {
  if (!value) return null;
  try {
    const auth = JSON.parse(value);
    return auth && typeof auth.token === 'string' && auth.token ? auth : null;
  } catch {
    return null;
  }
};

export const getStoredAuth = () => {
  const legacyValue = localStorage.getItem(AUTH_KEY);
  localStorage.removeItem(AUTH_KEY);

  const sessionValue = sessionStorage.getItem(AUTH_KEY);
  if (sessionValue) {
    const sessionAuth = parseAuth(sessionValue);
    if (sessionAuth) return sessionAuth;
    sessionStorage.removeItem(AUTH_KEY);
  }

  const legacyAuth = parseAuth(legacyValue);
  if (!legacyAuth) return null;
  sessionStorage.setItem(AUTH_KEY, JSON.stringify(legacyAuth));
  return legacyAuth;
};

export const saveStoredAuth = (auth) => {
  localStorage.removeItem(AUTH_KEY);
  sessionStorage.setItem(AUTH_KEY, JSON.stringify(auth));
};

export const clearStoredAuth = () => {
  sessionStorage.removeItem(AUTH_KEY);
  localStorage.removeItem(AUTH_KEY);
};
