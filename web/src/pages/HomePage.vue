<template>
  <q-page padding>
    <div class="text-h4 q-mb-sm">etcd Synthetic Load</div>
    <p class="text-body1 text-grey-8 q-mb-lg">
      Generate and apply synthetic Secret / ConfigMap / Namespace (and friends) load against a
      test cluster, to exercise etcd triage and monitoring tooling under realistic object volumes.
    </p>

    <q-banner class="bg-red-1 text-negative q-mb-xl" rounded>
      <template #avatar>
        <q-icon name="warning" color="negative" />
      </template>
      <strong>NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.</strong>
      This tool intentionally stresses etcd. Only ever point it at a disposable lab/dev cluster.
    </q-banner>

    <div class="row q-col-gutter-md">
      <div class="col-12 col-sm-6 col-md-3">
        <q-card flat bordered class="cursor-pointer action-card" @click="$router.push({ name: 'targets' })">
          <q-card-section>
            <div class="text-h6"><q-icon name="dns" class="q-mr-sm" />Targets</div>
            <div class="text-caption text-grey-7">Pick a cluster and see its load-test status</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-12 col-sm-6 col-md-3">
        <q-card flat bordered class="cursor-pointer action-card" @click="$router.push({ name: 'generate' })">
          <q-card-section>
            <div class="text-h6"><q-icon name="tune" class="q-mr-sm" />Generate</div>
            <div class="text-caption text-grey-7">Build a Small/Medium/Large load profile</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-12 col-sm-6 col-md-3">
        <q-card flat bordered class="cursor-pointer action-card" @click="$router.push({ name: 'load' })">
          <q-card-section>
            <div class="text-h6"><q-icon name="bolt" class="q-mr-sm" />Load</div>
            <div class="text-caption text-grey-7">Apply a generated plan to a cluster</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-12 col-sm-6 col-md-3">
        <q-card flat bordered class="cursor-pointer action-card" @click="$router.push({ name: 'results' })">
          <q-card-section>
            <div class="text-h6"><q-icon name="assessment" class="q-mr-sm" />Results</div>
            <div class="text-caption text-grey-7">Review summaries from loaded targets</div>
          </q-card-section>
        </q-card>
      </div>
    </div>
  </q-page>
</template>

<script setup>
</script>
