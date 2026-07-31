<template>
  <q-page padding>
    <div class="text-h4 q-mb-sm">Generate</div>
    <p class="text-body1 text-grey-8 q-mb-md">
      Generation Seed: set a utilization target, then S/M/L namespace budgets
      (<code>N × size/ns</code>). Composition inside each namespace must fit that budget —
      impossible mixes are blocked before you generate.
      <span v-if="targetId" class="text-primary"> Target: {{ targetId }}</span>
    </p>

    <q-banner class="bg-blue-1 text-primary q-mb-lg" rounded>
      <template #avatar><q-icon name="info" color="primary" /></template>
      Budgets are hard-capped: <code>N × size/ns</code> across S/M/L cannot exceed the
      utilization target, and composition cannot exceed size/ns.
      Impossible values are clamped automatically (e.g. 40×250&nbsp;MiB under a 5&nbsp;GiB target).
    </q-banner>

    <q-card flat bordered class="q-mb-lg">
      <q-card-section>
        <div class="text-subtitle1 q-mb-sm">Utilization target</div>
        <div class="row items-center q-col-gutter-md">
          <div class="col-12 col-md-6">
            <q-slider
              v-model="seed.utilizationGiB"
              :min="0.5"
              :max="8"
              :step="0.1"
              label
              label-always
              color="primary"
              @update:model-value="onUtilChange"
            />
          </div>
          <div class="col-auto">
            <q-input
              v-model.number="seed.utilizationGiB"
              type="number"
              dense outlined
              suffix="GiB"
              style="width: 140px"
              @update:model-value="onUtilChange"
            />
          </div>
          <div class="col-auto">
            <q-btn flat dense color="primary" label="Reset defaults" @click="loadDefaults" />
          </div>
        </div>

        <div class="q-mt-md">
          <div class="row items-center q-mb-xs">
            <div class="text-caption text-grey-7">Tier budgets vs target</div>
            <q-space />
            <div class="text-caption" :class="budgetOk ? 'text-positive' : 'text-negative'">
              {{ fmtGiB(preview?.tierBudgetsTotalGiB) }} / {{ fmtGiB(seed.utilizationGiB) }} GiB
              <span v-if="preview">(Δ {{ fmtDelta(preview.budgetDeltaPct) }})</span>
            </div>
          </div>
          <q-linear-progress
            :value="budgetProgress"
            :color="budgetOk ? 'positive' : 'negative'"
            size="10px"
            rounded
          />
        </div>
      </q-card-section>
    </q-card>

    <div class="row q-col-gutter-md q-mb-lg">
      <div class="col-12 col-md-4" v-for="(tier, ti) in seed.tiers" :key="tier.name">
        <q-card flat bordered :class="{ 'bg-red-1': tierPreview(tier.name) && !tierPreview(tier.name).fits }">
          <q-card-section>
            <div class="text-subtitle1 text-weight-bold">{{ tier.name }}</div>
            <div class="text-caption text-grey-7 q-mb-sm">
              {{ tier.namespaceCount }} × {{ humanBytes(tier.bytesPerNamespace) }}/ns
              = {{ humanBytes(tier.namespaceCount * tier.bytesPerNamespace) }}
            </div>

            <div class="text-caption">Namespaces <span class="text-grey-6">(max {{ maxNS(tier) }})</span></div>
            <q-slider
              :model-value="tier.namespaceCount"
              :min="1"
              :max="maxNS(tier)"
              label color="primary"
              @update:model-value="(v) => setNamespaceCount(tier, v)"
            />

            <div class="text-caption q-mt-sm">
              Size per namespace (MiB)
              <span class="text-grey-6">(max {{ maxSizeMiB(tier) }})</span>
            </div>
            <q-slider
              :model-value="bytesToMiB(tier.bytesPerNamespace)"
              :min="1"
              :max="maxSizeMiB(tier)"
              label
              color="primary"
              @update:model-value="(v) => setSizeMiB(tier, v)"
            />

            <div class="q-mt-sm">
              <div class="row items-center">
                <div class="text-caption">Composition vs ns budget</div>
                <q-space />
                <div class="text-caption" :class="tierPreview(tier.name)?.fits ? 'text-positive' : 'text-negative'">
                  {{ pct(tierPreview(tier.name)?.compositionUsedPct) }}
                </div>
              </div>
              <q-linear-progress
                :value="Math.min(1, (tierPreview(tier.name)?.compositionUsedPct || 0) / 100)"
                :color="tierPreview(tier.name)?.fits ? 'primary' : 'negative'"
                size="8px"
                class="q-mb-sm"
              />
            </div>

            <q-expansion-item dense dense-toggle label="Composition (object kinds)" header-class="text-caption">
              <div class="q-mb-sm">
                <q-select
                  dense outlined multiple use-chips
                  :options="kindOptions"
                  label="Enabled kinds"
                  :model-value="enabledKinds(tier)"
                  @update:model-value="(v) => setEnabledKinds(tier, v)"
                />
              </div>
              <div v-for="k in enabledSpecs(tier)" :key="k.kind" class="q-mb-md">
                <div class="text-weight-medium text-caption">{{ k.kind }}</div>
                <div class="text-caption text-grey-7">
                  records/ns · max ~{{ maxRec(tier, k) }} at avg {{ humanBytes(avgBytes(k)) }}
                </div>
                <q-slider
                  v-model="k.recordsPerNamespace"
                  :min="0"
                  :max="Math.max(maxRec(tier, k), k.recordsPerNamespace, 1)"
                  label color="secondary"
                  @update:model-value="schedulePreview"
                />
                <div class="row q-col-gutter-sm">
                  <div class="col-6">
                    <q-input
                      dense outlined type="number" label="SmallX (KiB)"
                      :model-value="bytesToKiB(k.smallX)"
                      @update:model-value="(v) => { k.smallX = kiBToBytes(v); schedulePreview() }"
                    />
                  </div>
                  <div class="col-6">
                    <q-input
                      dense outlined type="number" label="LargeX (KiB)"
                      :model-value="bytesToKiB(k.largeX)"
                      @update:model-value="(v) => { k.largeX = kiBToBytes(v); schedulePreview() }"
                    />
                  </div>
                </div>
              </div>
            </q-expansion-item>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <q-banner
      v-for="(iss, i) in (preview?.issues || [])"
      :key="i"
      class="q-mb-sm"
      rounded
      :class="iss.level === 'error' ? 'bg-red-1 text-negative' : 'bg-orange-1 text-orange-9'"
    >
      <template #avatar>
        <q-icon :name="iss.level === 'error' ? 'error' : 'warning'" />
      </template>
      {{ iss.message }}
    </q-banner>

    <div class="row justify-end q-gutter-sm q-mb-lg">
      <q-btn flat label="Reset" @click="loadDefaults" />
      <q-btn
        color="primary"
        label="Generate plan"
        :loading="submitting"
        :disable="!preview?.ok"
        @click="onSubmit"
      />
    </div>

    <q-banner v-if="error" class="bg-orange-1 text-orange-9 q-mb-lg" rounded>
      <template #avatar><q-icon name="cloud_off" color="orange-9" /></template>
      {{ error }}
    </q-banner>

    <template v-if="plan">
      <div class="text-h5 q-mb-md">Plan preview</div>
      <div class="row q-col-gutter-md q-mb-lg">
        <div class="col-12 col-sm-6 col-md-3" v-for="stat in summaryStats" :key="stat.label">
          <q-card flat bordered>
            <q-card-section class="text-center">
              <div class="text-h5 text-primary">{{ stat.value }}</div>
              <div class="text-caption text-grey-7">{{ stat.label }}</div>
            </q-card-section>
          </q-card>
        </div>
      </div>
      <div class="row q-col-gutter-md q-mb-lg">
        <div class="col-12 col-md-4" v-for="tier in tiers" :key="tier.name">
          <q-card flat bordered>
            <q-card-section>
              <div class="text-subtitle1 text-uppercase text-weight-bold q-mb-sm">{{ tier.name }}</div>
              <q-list dense>
                <q-item>
                  <q-item-section>Namespaces × size/ns</q-item-section>
                  <q-item-section side>
                    {{ fmt(tier.namespaceCount) }} × {{ humanBytes(tier.bytesPerNamespace) }}
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>Tier budget</q-item-section>
                  <q-item-section side>{{ humanBytes(tier.tierBudgetBytes) }}</q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>Records / size</q-item-section>
                  <q-item-section side>{{ fmt(tier.totalRecords) }} / {{ humanBytes(tier.totalSizeBytes) }}</q-item-section>
                </q-item>
              </q-list>
              <div class="text-caption text-weight-medium q-mt-md q-mb-xs">Composition</div>
              <q-list dense>
                <q-item v-for="c in tier.composition || []" :key="c.kind">
                  <q-item-section>
                    <q-item-label>{{ fmt(c.recordCount) }} {{ c.kind }}</q-item-label>
                    <q-item-label caption>
                      SmallX: {{ humanBytes(c.sizeRange?.smallX) }},
                      LargeX: {{ humanBytes(c.sizeRange?.largeX) }}
                    </q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>
            </q-card-section>
          </q-card>
        </div>
      </div>
      <q-card-actions align="right">
        <q-btn color="primary" icon-right="arrow_forward" label="Continue to Load" @click="continueToLoad" />
      </q-card-actions>
    </template>
  </q-page>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Notify } from 'quasar'
import { generatePlan, getSeedDefaults, previewSeed, listSeedKinds, configureTarget } from 'src/services/api'

const route = useRoute()
const router = useRouter()
const targetId = computed(() => route.query.targetId || '')

const KiB = 1024
const MiB = 1024 * KiB

const seed = reactive({ utilizationGiB: 5.0, assumedQuotaGiB: 8.0, tiers: [] })
const kindOptions = ref([])
const preview = ref(null)
const plan = ref(null)
const submitting = ref(false)
const error = ref('')
let previewTimer = null

const budgetOk = computed(() => preview.value?.ok !== false && Math.abs(preview.value?.budgetDeltaPct || 0) <= 10)
const budgetProgress = computed(() => {
  const t = seed.utilizationGiB || 1
  const g = preview.value?.tierBudgetsTotalGiB || 0
  return Math.min(1.5, g / t) / 1.5
})

function tierPreview(name) {
  return (preview.value?.tiers || []).find((t) => t.name === name)
}

function bytesToMiB(b) { return Math.round(Number(b) / MiB) }
function miBToBytes(m) { return Math.round(Number(m) * MiB) }
function bytesToKiB(b) { return Math.round(Number(b) / KiB) }
function kiBToBytes(k) { return Math.round(Number(k) * KiB) }
function avgBytes(k) {
  const lo = Number(k.smallX) || 1
  const hi = Math.max(lo, Number(k.largeX) || lo)
  return Math.floor((lo + hi) / 2)
}
function maxRec(tier, k) {
  const avg = avgBytes(k)
  if (!avg || !tier.bytesPerNamespace) return 0
  return Math.floor(tier.bytesPerNamespace / avg)
}
function enabledKinds(tier) {
  return (tier.composition || []).filter((k) => k.enabled).map((k) => k.kind)
}
function enabledSpecs(tier) {
  return (tier.composition || []).filter((k) => k.enabled)
}
function setEnabledKinds(tier, selected) {
  const set = new Set(selected || [])
  for (const k of tier.composition || []) {
    const on = set.has(k.kind)
    k.enabled = on
    if (on && (!k.recordsPerNamespace || k.recordsPerNamespace < 1)) {
      k.recordsPerNamespace = Math.min(10, Math.max(1, maxRec(tier, k)))
    }
  }
  schedulePreview()
}

function humanBytes(n) {
  if (n === undefined || n === null) return '—'
  const v = Number(n)
  if (v >= 1024 * 1024 * 1024) return `${(v / (1024 * 1024 * 1024)).toFixed(2)} GiB`
  if (v >= 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MiB`
  if (v >= 1024) return `${(v / 1024).toFixed(1)} KiB`
  return `${v} B`
}
function fmt(v) {
  if (v === undefined || v === null) return '—'
  if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2)
  return v
}
function fmtGiB(v) {
  if (v === undefined || v === null) return '—'
  return Number(v).toFixed(2)
}
function fmtDelta(v) {
  if (v === undefined || v === null) return '—'
  const n = Number(v)
  return `${n >= 0 ? '+' : ''}${n.toFixed(1)}%`
}
function pct(v) {
  if (v === undefined || v === null) return '—'
  return `${Number(v).toFixed(0)}%`
}

function maxSizeMiB(tier) {
  const tp = tierPreview(tier.name)
  const fromServer = tp?.maxBytesPerNamespace ? bytesToMiB(tp.maxBytesPerNamespace) : 0
  // Client-side remaining budget so the slider can't slide into overshoot before preview returns.
  const target = miBToBytes((seed.utilizationGiB || 5) * 1024)
  let others = 0
  for (const t of seed.tiers || []) {
    if (t.name === tier.name) continue
    others += (t.namespaceCount || 0) * (t.bytesPerNamespace || 0)
  }
  const remain = Math.max(MiB, target - others)
  const ns = Math.max(1, tier.namespaceCount || 1)
  const clientMax = Math.max(1, Math.floor(remain / ns / MiB))
  const cap = fromServer > 0 ? Math.min(fromServer, clientMax) : clientMax
  return Math.max(1, Math.min(cap, tier.name === 'LARGE' ? 2048 : 512))
}

function maxNS(tier) {
  const tp = tierPreview(tier.name)
  const fromServer = tp?.maxNamespaceCount || 0
  const target = miBToBytes((seed.utilizationGiB || 5) * 1024)
  let others = 0
  for (const t of seed.tiers || []) {
    if (t.name === tier.name) continue
    others += (t.namespaceCount || 0) * (t.bytesPerNamespace || 0)
  }
  const remain = Math.max(MiB, target - others)
  const per = Math.max(1, tier.bytesPerNamespace || MiB)
  const clientMax = Math.max(1, Math.floor(remain / per))
  const cap = fromServer > 0 ? Math.min(fromServer, clientMax) : clientMax
  return Math.max(1, Math.min(cap, 500))
}

function setNamespaceCount(tier, v) {
  const max = maxNS(tier)
  tier.namespaceCount = Math.min(Math.max(1, Number(v) || 1), max)
  // Re-clamp size/ns under the new ns count.
  const maxMiB = maxSizeMiB(tier)
  if (bytesToMiB(tier.bytesPerNamespace) > maxMiB) {
    tier.bytesPerNamespace = miBToBytes(maxMiB)
  }
  clampComposition(tier)
  schedulePreview()
}

function setSizeMiB(tier, v) {
  const max = maxSizeMiB(tier)
  const miB = Math.min(Math.max(1, Number(v) || 1), max)
  tier.bytesPerNamespace = miBToBytes(miB)
  clampComposition(tier)
  schedulePreview()
}

function clampComposition(tier) {
  for (const k of tier.composition || []) {
    if (!k.enabled) continue
    const max = maxRec(tier, k)
    if (k.recordsPerNamespace > max) k.recordsPerNamespace = max
  }
}

function schedulePreview() {
  clearTimeout(previewTimer)
  previewTimer = setTimeout(runPreview, 200)
}

async function runPreview() {
  try {
    const res = await previewSeed(JSON.parse(JSON.stringify(seed)))
    // API may return { preview, seed, clamped } after auto-clamp.
    if (res.preview) {
      preview.value = res.preview
      if (res.clamped && res.seed?.tiers) {
        seed.tiers = res.seed.tiers
        seed.utilizationGiB = res.seed.utilizationGiB
      }
    } else {
      preview.value = res
    }
  } catch (e) {
    preview.value = { ok: false, issues: [{ level: 'error', message: e.message }] }
  }
}

async function onUtilChange() {
  // Keep current tier shape; only re-preview (user can Reset for scaled defaults).
  schedulePreview()
}

async function loadDefaults() {
  error.value = ''
  plan.value = null
  try {
    const d = await getSeedDefaults(seed.utilizationGiB || 5.0)
    seed.utilizationGiB = d.utilizationGiB
    seed.assumedQuotaGiB = d.assumedQuotaGiB || 8
    seed.tiers = d.tiers || []
    await runPreview()
  } catch (e) {
    error.value = e.message
  }
}

async function onSubmit() {
  submitting.value = true
  error.value = ''
  plan.value = null
  try {
    if (targetId.value) {
      await configureTarget(targetId.value, JSON.parse(JSON.stringify(seed)))
      Notify.create({ type: 'positive', message: 'Seed saved & validated on target.' })
    }
    plan.value = await generatePlan({
      targetId: targetId.value || undefined,
      utilizationGiB: seed.utilizationGiB,
      seed: JSON.parse(JSON.stringify(seed)),
    })
    Notify.create({ type: 'positive', message: `Plan ${plan.value.metadata?.id || ''} generated.` })
  } catch (e) {
    error.value = e.response?.data || e.message
  } finally {
    submitting.value = false
  }
}

const tiers = computed(() => plan.value?.tiers || [])
const summaryStats = computed(() => {
  const s = plan.value?.summary || {}
  return [
    { label: 'Namespaces', value: fmt(s.totalNamespaces) },
    { label: 'Records', value: fmt(s.totalRecords) },
    { label: 'Estimated total (GiB)', value: fmt(s.totalSizeGiB) },
    { label: 'Plan ID', value: plan.value?.metadata?.id || '—' },
  ]
})

function continueToLoad() {
  router.push({
    name: 'load',
    query: { targetId: targetId.value || undefined, planId: plan.value?.metadata?.id },
  })
}

onMounted(async () => {
  try {
    const k = await listSeedKinds()
    kindOptions.value = k.kinds || []
  } catch {
    kindOptions.value = ['secrets', 'configmaps', 'services', 'rolebindings', 'serviceaccounts', 'egressfirewalls', 'routes']
  }
  await loadDefaults()
})
</script>
