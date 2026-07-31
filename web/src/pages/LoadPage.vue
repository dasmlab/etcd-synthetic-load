<template>
  <q-page padding>
    <div class="text-h4 q-mb-sm">Load</div>
    <p class="text-body1 text-grey-8 q-mb-lg">
      Apply a generated plan to a cluster. This is the step that actually creates objects in etcd.
    </p>

    <q-banner class="bg-red-1 text-negative q-mb-lg" rounded>
      <template #avatar><q-icon name="warning" color="negative" /></template>
      <strong>NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.</strong>
      Loading intentionally stresses etcd and cannot be easily undone.
    </q-banner>

    <div v-if="!planId" class="text-center text-grey-7 q-my-xl">
      <q-icon name="description" size="3em" class="q-mb-sm" />
      <div>No plan selected. Generate a plan first, then continue here.</div>
      <q-btn class="q-mt-md" color="primary" label="Go to Generate" :to="{ name: 'generate' }" />
    </div>

    <template v-else>
      <q-card flat bordered class="q-mb-lg">
        <q-card-section>
          <div class="text-subtitle1 q-mb-sm">Plan summary</div>
          <div class="text-caption text-grey-7 q-mb-md">
            Plan <code>{{ planId }}</code>
            <span v-if="targetId"> · Target <code>{{ targetId }}</code></span>
          </div>

          <div v-if="loadingPlan" class="row justify-center q-my-md">
            <q-spinner color="primary" size="2em" />
          </div>
          <q-banner v-else-if="planError" class="bg-orange-1 text-orange-9" rounded>
            <template #avatar><q-icon name="cloud_off" color="orange-9" /></template>
            Could not load plan details ({{ planError }}).
          </q-banner>
          <div v-else class="row q-col-gutter-md">
            <div class="col-6 col-md-3" v-for="stat in planStats" :key="stat.label">
              <div class="text-h6 text-primary">{{ stat.value }}</div>
              <div class="text-caption text-grey-7">{{ stat.label }}</div>
            </div>
          </div>
        </q-card-section>

        <q-separator />

        <q-card-section>
          <q-toggle v-model="dryRun" label="Dry run (validate only, no objects created)" color="primary" />
        </q-card-section>

        <q-separator />

        <q-card-actions align="right">
          <q-btn
            color="negative"
            icon="bolt"
            label="LOAD"
            :loading="submitting"
            :disable="loadStatus && isActive(loadStatus.state)"
            @click="confirmOpen = true"
          />
        </q-card-actions>
      </q-card>

      <q-card v-if="loadStatus" flat bordered>
        <q-card-section>
          <div class="row items-center q-mb-sm">
            <div class="text-subtitle1">Load status</div>
            <q-space />
            <q-badge :color="statusColor(loadStatus.state)" class="text-capitalize">
              {{ loadStatus.state || 'unknown' }}
            </q-badge>
          </div>
          <q-linear-progress
            v-if="isActive(loadStatus.state)"
            indeterminate
            color="primary"
            class="q-mb-md"
          />
          <q-linear-progress
            v-else-if="typeof loadStatus.progress === 'number'"
            :value="loadStatus.progress"
            color="primary"
            class="q-mb-md"
          />
          <div class="text-caption text-grey-7">{{ loadStatus.message || '' }}</div>
        </q-card-section>
      </q-card>
    </template>

    <q-dialog v-model="confirmOpen">
      <q-card style="min-width: 380px">
        <q-card-section class="row items-center">
          <q-icon name="warning" color="negative" size="md" class="q-mr-sm" />
          <span class="text-h6">Are you really sure?</span>
        </q-card-section>
        <q-card-section class="text-body1">
          This stresses etcd{{ dryRun ? ' (dry run — no objects will actually be created)' : '' }}.
          Only proceed on a disposable lab/dev cluster.
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn color="negative" label="Yes, load it" :loading="submitting" @click="doLoad" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Notify } from 'quasar'
import { getPlan, submitLoad, getLoadStatus } from 'src/services/api'

const route = useRoute()
const planId = computed(() => route.query.planId || '')
const targetId = computed(() => route.query.targetId || '')

const plan = ref(null)
const loadingPlan = ref(false)
const planError = ref('')

const dryRun = ref(false)
const confirmOpen = ref(false)
const submitting = ref(false)
const loadStatus = ref(null)
let pollTimer = null

const planStats = computed(() => {
  const s = plan.value?.summary || {}
  const t = plan.value?.target?.objectTotals || {}
  return [
    { label: 'Namespaces', value: fmt(s.totalNamespaces) },
    { label: 'Records', value: fmt(s.totalRecords) },
    { label: 'Secrets (target)', value: fmt(t.secrets) },
    { label: 'Estimated total (GiB)', value: fmt(s.totalSizeGiB) },
  ]
})

function fmt(v) {
  if (v === undefined || v === null) return '—'
  if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2)
  return v
}

function isActive(state) {
  return ['pending', 'running', 'loading'].includes(state)
}

function statusColor(state) {
  return {
    pending: 'grey-6',
    running: 'orange-7',
    loading: 'orange-7',
    complete: 'positive',
    completed: 'positive',
    failed: 'negative',
    error: 'negative',
  }[state] || 'grey-6'
}

async function loadPlan() {
  if (!planId.value) return
  loadingPlan.value = true
  planError.value = ''
  try {
    plan.value = await getPlan(planId.value)
  } catch (e) {
    planError.value = e.response?.data?.message || e.message
  } finally {
    loadingPlan.value = false
  }
}

async function doLoad() {
  submitting.value = true
  try {
    const res = await submitLoad({ planId: planId.value, confirm: true, dryRun: dryRun.value })
    loadStatus.value = res
    confirmOpen.value = false
    Notify.create({ type: 'positive', message: 'Load started.' })
    startPolling(res.id || res.loadId)
  } catch (e) {
    Notify.create({ type: 'negative', message: `Load request failed: ${e.response?.data?.message || e.message}` })
  } finally {
    submitting.value = false
  }
}

function startPolling(loadId) {
  if (!loadId) return
  stopPolling()
  pollTimer = setInterval(async () => {
    try {
      loadStatus.value = await getLoadStatus(loadId)
      if (!isActive(loadStatus.value.state)) stopPolling()
    } catch {
      stopPolling()
    }
  }, 2000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

onMounted(loadPlan)
onBeforeUnmount(stopPolling)
</script>
