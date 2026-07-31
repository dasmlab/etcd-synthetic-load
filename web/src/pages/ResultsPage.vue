<template>
  <q-page padding>
    <div class="text-h4 q-mb-sm">Results</div>
    <p class="text-body1 text-grey-8 q-mb-lg">
      Summary of a loaded target. Full test-execution results are not implemented yet — this is a
      placeholder view of what was applied.
    </p>

    <div v-if="!id" class="text-center text-grey-7 q-my-xl">
      <q-icon name="assessment" size="3em" class="q-mb-sm" />
      <div>No target selected.</div>
      <q-btn class="q-mt-md" color="primary" label="Go to Targets" :to="{ name: 'targets' }" />
    </div>

    <template v-else>
      <div v-if="loading" class="row justify-center q-my-xl">
        <q-spinner color="primary" size="3em" />
      </div>

      <q-banner v-else-if="error" class="bg-orange-1 text-orange-9 q-mb-lg" rounded>
        <template #avatar><q-icon name="cloud_off" color="orange-9" /></template>
        Could not load results ({{ error }}).
      </q-banner>

      <template v-else>
        <div class="row q-col-gutter-md q-mb-lg">
          <div class="col-6 col-md-3" v-for="stat in stats" :key="stat.label">
            <q-card flat bordered>
              <q-card-section class="text-center">
                <div class="text-h5 text-primary">{{ stat.value }}</div>
                <div class="text-caption text-grey-7">{{ stat.label }}</div>
              </q-card-section>
            </q-card>
          </div>
        </div>

        <q-card flat bordered>
          <q-card-section>
            <div class="text-subtitle1 q-mb-sm">Notes</div>
            <div class="text-body2 text-grey-8">
              {{ results?.notes || 'Load-time metrics only. Latency/throughput test execution against this target has not been run.' }}
            </div>
          </q-card-section>
        </q-card>
      </template>
    </template>
  </q-page>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { getResults } from 'src/services/api'

const props = defineProps({ id: { type: String, default: '' } })

const id = computed(() => props.id)
const results = ref(null)
const loading = ref(false)
const error = ref('')

function fmt(v) {
  if (v === undefined || v === null) return '—'
  if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2)
  return v
}

const stats = computed(() => {
  const s = results.value?.summary || results.value || {}
  return [
    { label: 'Namespaces', value: fmt(s.totalNamespaces) },
    { label: 'Records', value: fmt(s.totalRecords || s.done) },
    { label: 'Secrets', value: fmt(s.totalSecrets || s.created?.secrets) },
    { label: 'Estimated total (GiB)', value: fmt(s.estimatedTotalGiB || s.totalSizeGiB) },
  ]
})

async function load() {
  if (!id.value) return
  loading.value = true
  error.value = ''
  try {
    results.value = await getResults(id.value)
  } catch (e) {
    error.value = e.response?.data?.message || e.message
  } finally {
    loading.value = false
  }
}

watch(id, load)
onMounted(load)
</script>
