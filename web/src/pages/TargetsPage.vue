<template>
  <q-page padding>
    <div class="row items-center q-mb-md">
      <div class="text-h4">Targets</div>
      <q-space />
      <q-btn color="primary" icon="add" label="Create Target" class="q-mr-sm" @click="createOpen = true" />
      <q-btn flat dense icon="refresh" label="Refresh" @click="load" />
    </div>
    <p class="text-body1 text-grey-8 q-mb-lg">
      Workflow: Create → Configure → Generate map → Load → Test → Report.
      Clean is available any time; Delete drops the Target record.
      Credentials never appear here — set <code>OC_PASSWORD</code> / kubeconfig in the environment.
    </p>

    <div v-if="loading" class="row justify-center q-my-xl">
      <q-spinner color="primary" size="3em" />
    </div>

    <q-banner v-else-if="error" class="bg-orange-1 text-orange-9 q-mb-lg" rounded>
      <template #avatar><q-icon name="cloud_off" color="orange-9" /></template>
      Could not reach the API server ({{ error }}). Is the backend running on :8080?
    </q-banner>

    <div v-else-if="targets.length === 0" class="text-center text-grey-7 q-my-xl">
      <q-icon name="dns" size="3em" class="q-mb-sm" />
      <div>No targets yet — create one to begin.</div>
    </div>

    <div v-else class="row q-col-gutter-md">
      <div v-for="target in targets" :key="target.id" class="col-12 col-sm-6 col-md-4">
        <q-card flat bordered>
          <q-card-section>
            <div class="row items-center no-wrap">
              <div class="text-h6 ellipsis">{{ target.name }}</div>
              <q-space />
              <q-badge :color="statusColor(target.status)" class="text-capitalize">
                {{ target.status || 'idle' }}
              </q-badge>
            </div>
            <div class="text-caption text-grey-7 q-mt-xs">
              <q-icon name="dns" size="xs" class="q-mr-xs" />{{ target.apiServer || '—' }}
            </div>
            <div class="text-caption text-grey-6">{{ target.id }}</div>
            <div v-if="target.message" class="text-caption q-mt-xs">{{ target.message }}</div>
          </q-card-section>

          <q-separator />

          <q-card-actions align="right" class="q-gutter-xs">
            <q-btn flat dense size="sm" color="primary" label="Configure"
              @click="$router.push({ name: 'generate', query: { targetId: target.id } })" />
            <q-btn flat dense size="sm" color="primary" label="Generate"
              :loading="genBusy === target.id" @click="doGenerate(target)" />
            <q-btn flat dense size="sm" color="primary" label="Load"
              :disable="!target.mapReady && target.status !== 'generated' && target.status !== 'loaded'"
              @click="$router.push({ name: 'load', query: { targetId: target.id, planId: target.planId } })" />
            <q-btn flat dense size="sm" color="primary" label="Test"
              :disable="!target.loaded && target.status !== 'loaded'"
              @click="runTest(target)" />
            <q-btn flat dense size="sm" color="primary" label="Results"
              @click="$router.push({ name: 'results-detail', params: { id: target.id } })" />
            <q-btn flat dense size="sm" color="negative" label="Delete" @click="doDelete(target)" />
          </q-card-actions>
        </q-card>
      </div>
    </div>

    <q-dialog v-model="createOpen">
      <q-card style="min-width: 420px">
        <q-card-section class="text-h6">Create Target</q-card-section>
        <q-card-section>
          <q-input v-model="form.name" filled label="Display name" class="q-mb-md" />
          <q-input v-model="form.apiServer" filled label="API server URL" hint="https://api....:6443" class="q-mb-md" />
          <q-input v-model="form.username" filled label="Username (hint only)" class="q-mb-md" />
          <q-input v-model.number="form.tolerancePercent" type="number" filled label="Tolerance ±%" hint="default 10" />
          <div class="text-caption text-grey-7 q-mt-md">
            Password is NOT stored — set <code>OC_PASSWORD</code> in the container/env.
          </div>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn color="primary" label="Create" :loading="creating" @click="doCreate" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { Dialog, Notify } from 'quasar'
import {
  listTargets, createTarget, generateTargetMap, deleteTarget, submitTest,
} from 'src/services/api'

const targets = ref([])
const loading = ref(true)
const error = ref('')
const createOpen = ref(false)
const creating = ref(false)
const genBusy = ref('')
const form = reactive({
  name: 'PROD-2',
  apiServer: 'https://api.2026-prod-2.ocp.dasmlab.org:6443',
  username: 'dasm',
  tolerancePercent: 10,
})

function statusColor(status) {
  return {
    created: 'grey-6',
    configured: 'blue-7',
    generated: 'blue-7',
    loading: 'orange-7',
    loaded: 'positive',
    testing: 'purple-7',
    reported: 'teal-7',
  }[status] || 'grey-6'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    targets.value = (await listTargets()) || []
  } catch (e) {
    error.value = e.message
    targets.value = []
  } finally {
    loading.value = false
  }
}

async function doCreate() {
  creating.value = true
  try {
    await createTarget({ ...form })
    createOpen.value = false
    Notify.create({ type: 'positive', message: 'Target created.' })
    await load()
  } catch (e) {
    Notify.create({ type: 'negative', message: e.response?.data || e.message })
  } finally {
    creating.value = false
  }
}

async function doGenerate(target) {
  genBusy.value = target.id
  try {
    const man = await generateTargetMap(target.id)
    Notify.create({
      type: 'positive',
      message: `Map ready: ${man.summary?.totalShards} shards / ${man.summary?.totalObjects} objects`,
    })
    await load()
  } catch (e) {
    Notify.create({
      type: 'negative',
      message: `Generate failed (configure seed first?): ${e.response?.data || e.message}`,
    })
  } finally {
    genBusy.value = ''
  }
}

async function doDelete(target) {
  Dialog.create({
    title: 'Delete Target?',
    message: `Removes ${target.id} from runtime data. Run Clean on the cluster first if it was loaded.`,
    cancel: true,
    persistent: true,
  }).onOk(async () => {
    try {
      await deleteTarget(target.id)
      Notify.create({ type: 'positive', message: 'Target deleted.' })
      await load()
    } catch (e) {
      Notify.create({ type: 'negative', message: e.response?.data || e.message })
    }
  })
}

async function runTest(target) {
  try {
    const { notImplemented } = await submitTest({ planId: target.planId, targetId: target.id })
    if (notImplemented) {
      Notify.create({ type: 'info', message: 'Testing is not implemented yet.' })
    }
  } catch (e) {
    Notify.create({ type: 'negative', message: `Test request failed: ${e.message}` })
  }
}

onMounted(load)
</script>
