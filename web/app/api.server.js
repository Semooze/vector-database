const baseURL = process.env.LAB_API_URL || "http://127.0.0.1:8080";

export async function api(path, init = {}) {
  const response = await fetch(`${baseURL}${path}`, init);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(payload.error || `API returned ${response.status}`);
  }
  return payload;
}
