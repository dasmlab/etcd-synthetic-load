<template>
  <q-layout view="lHh Lpr lFf">
    <q-header elevated class="bg-primary text-white">
      <q-toolbar>
        <q-btn
          flat dense round icon="menu"
          aria-label="Menu"
          @click="leftDrawerOpen = !leftDrawerOpen"
        />
        <q-icon name="bolt" size="sm" class="q-mx-sm" />
        <q-toolbar-title>etcd Synthetic Load</q-toolbar-title>
        <q-chip square dense color="white" text-color="primary" class="q-mr-sm">{{ versionLabel }}</q-chip>
      </q-toolbar>
      <div class="warning-banner text-center q-py-xs text-caption">
        <q-icon name="warning" size="xs" class="q-mb-xs" />
        NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT — this tool intentionally stresses etcd
        <q-icon name="warning" size="xs" class="q-mb-xs" />
      </div>
    </q-header>

    <q-drawer v-model="leftDrawerOpen" show-if-above bordered>
      <q-list>
        <q-item-label header>Navigation</q-item-label>
        <q-item clickable v-ripple :to="{ name: 'home' }" exact active-class="text-primary bg-grey-2">
          <q-item-section avatar><q-icon name="home" /></q-item-section>
          <q-item-section>
            <q-item-label>Home</q-item-label>
            <q-item-label caption>Overview &amp; actions</q-item-label>
          </q-item-section>
        </q-item>
        <q-item clickable v-ripple :to="{ name: 'targets' }" active-class="text-primary bg-grey-2">
          <q-item-section avatar><q-icon name="dns" /></q-item-section>
          <q-item-section>
            <q-item-label>Targets</q-item-label>
            <q-item-label caption>Clusters under test</q-item-label>
          </q-item-section>
        </q-item>
        <q-item clickable v-ripple :to="{ name: 'generate' }" active-class="text-primary bg-grey-2">
          <q-item-section avatar><q-icon name="tune" /></q-item-section>
          <q-item-section>
            <q-item-label>Generate</q-item-label>
            <q-item-label caption>Build a load profile</q-item-label>
          </q-item-section>
        </q-item>
        <q-item clickable v-ripple :to="{ name: 'load' }" active-class="text-primary bg-grey-2">
          <q-item-section avatar><q-icon name="bolt" /></q-item-section>
          <q-item-section>
            <q-item-label>Load</q-item-label>
            <q-item-label caption>Apply to a cluster</q-item-label>
          </q-item-section>
        </q-item>
        <q-item clickable v-ripple :to="{ name: 'results' }" active-class="text-primary bg-grey-2">
          <q-item-section avatar><q-icon name="assessment" /></q-item-section>
          <q-item-section>
            <q-item-label>Results</q-item-label>
            <q-item-label caption>Load summaries</q-item-label>
          </q-item-section>
        </q-item>

        <q-separator class="q-my-md" />

        <div class="q-pa-sm">
          <q-chip
            color="negative"
            text-color="white"
            icon="warning"
            square
            class="full-width"
            style="white-space: normal; height: auto; min-height: 32px"
          >
            Stress-test tool. Lab/dev clusters only.
          </q-chip>
        </div>
      </q-list>
    </q-drawer>

    <q-page-container>
      <router-view />
    </q-page-container>
  </q-layout>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { getHealth } from 'src/services/api'

const leftDrawerOpen = ref(false)
const versionLabel = ref('…')

onMounted(async () => {
  try {
    const h = await getHealth()
    versionLabel.value = h.version || 'dev'
  } catch {
    versionLabel.value = 'offline'
  }
})
</script>
