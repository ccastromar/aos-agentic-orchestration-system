// Deep links into the other Chestertown apps. URLs are deploy-configured (same as the app
// switcher); these are dev defaults.
const strip = (url) => url.replace(/\/+$/, '');
const ERNEST_URL = strip(import.meta.env.VITE_ERNEST_URL || 'http://localhost:5174');
const PRUDENCE_URL = strip(import.meta.env.VITE_PRUDENCE_URL || 'http://localhost:5200');

/** The AI system's evidence in Ernest: Prudence's decisions and AOS's executions, together. */
export const ernestEvidenceUrl = (aiSystemId) =>
  `${ERNEST_URL}/models?model=${encodeURIComponent(aiSystemId)}`;

/** The AI system in Prudence's registry, where it is reviewed and approved. */
export const prudenceSystemUrl = (aiSystemId) =>
  `${PRUDENCE_URL}/?tab=registry&system=${encodeURIComponent(aiSystemId)}`;
