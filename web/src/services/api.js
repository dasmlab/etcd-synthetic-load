import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1',
  timeout: 30_000,
})

export default api

export async function getHealth() {
  const { data } = await api.get('/health')
  return data
}

export async function listTargets() {
  const { data } = await api.get('/targets')
  return data
}

export async function createTarget(payload) {
  const { data } = await api.post('/targets', payload)
  return data
}

export async function getTarget(id) {
  const { data } = await api.get(`/targets/${id}`)
  return data
}

export async function configureTarget(id, seed) {
  const { data } = await api.post(`/targets/${id}/configure`, seed)
  return data
}

export async function generateTargetMap(id) {
  const { data } = await api.post(`/targets/${id}/generate`)
  return data
}

export async function deleteTarget(id) {
  const { data } = await api.delete(`/targets/${id}`)
  return data
}

// payload: { utilizationGiB?, utilizationPercent?, objects: { secrets, configmaps, services,
// routes, egressfirewalls, rolebindings, serviceaccounts }, targetId? }
export async function generatePlan(payload) {
  const { data } = await api.post('/generate', payload)
  return data
}

export async function getSeedDefaults(utilizationGiB) {
  const { data } = await api.get('/seed/defaults', {
    params: utilizationGiB != null ? { utilizationGiB } : {},
  })
  return data
}

export async function listSeedKinds() {
  const { data } = await api.get('/seed/kinds')
  return data
}

export async function previewSeed(seed) {
  const { data } = await api.post('/seed/preview', seed)
  return data
}

export async function getPlan(id) {
  const { data } = await api.get(`/plans/${id}`)
  return data
}

// payload: { planId, confirm: true, dryRun? }
export async function submitLoad(payload) {
  const { data } = await api.post('/load', payload)
  return data
}

export async function getLoadStatus(id) {
  const { data } = await api.get(`/load/${id}/status`)
  return data
}

// Test execution is not implemented server-side yet; the API stub returns
// HTTP 501, which we treat as a valid (non-error) response so callers can
// show a friendly "not implemented yet" message instead of an error toast.
export async function submitTest(payload) {
  const { data, status } = await api.post('/test', payload, {
    validateStatus: (s) => (s >= 200 && s < 300) || s === 501,
  })
  return { data, notImplemented: status === 501 }
}

export async function getResults(id) {
  const { data } = await api.get(`/results/${id}`)
  return data
}
