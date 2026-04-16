<script>
import { CAPI as RANCHER_CAPI } from '@shell/config/types';
import { CAPIOVH } from '../types/capiovh';
import Banner from '@components/Banner/Banner.vue';

export default {
  name: 'CAPIOVHDashboard',

  components: { Banner },

  async fetch() {
    try {
      const capiClusters = await this.$store.dispatch('management/findAll', { type: RANCHER_CAPI.CAPI_CLUSTER });

      this.clusters = (capiClusters || []).filter((c) => {
        const cc = c.spec?.topology?.class || '';
        return cc.includes('ovhcloud');
      });

      try {
        this.ovhClusters = await this.$store.dispatch('management/findAll', { type: CAPIOVH.OVH_CLUSTER });
      } catch (e) {
        // OVH CRD may not be available
      }
    } catch (e) {
      this.error = e?.message || 'Failed to load clusters';
    }
  },

  data() {
    return {
      clusters:    [],
      ovhClusters: [],
      error:       null,
    };
  },

  computed: {
    clusterRows() {
      return this.clusters.map((c) => {
        const ovh = (this.ovhClusters || []).find(
          (o) => c.metadata?.name && o.metadata?.name?.startsWith(c.metadata.name)
        );

        return {
          name:      c.metadata?.name,
          namespace: c.metadata?.namespace,
          phase:     c.status?.phase || 'Unknown',
          version:   c.spec?.topology?.version || '-',
          cpReplicas: c.spec?.topology?.controlPlane?.replicas || 0,
          workerReplicas: c.spec?.topology?.workers?.machineDeployments?.[0]?.replicas || 0,
          region:    ovh?.spec?.region || '-',
          ready:     ovh?.status?.ready || false,
          age:       c.metadata?.creationTimestamp,
        };
      });
    },
  },

  methods: {
    phaseColor(phase) {
      if (phase === 'Provisioned') return 'bg-success';
      if (phase === 'Provisioning') return 'bg-warning';
      if (phase === 'Deleting') return 'bg-error';
      return 'bg-info';
    },
  },
};
</script>

<template>
  <div>
    <h1 class="mb-20">
      <img
        src="../assets/ovhcloud-logo.png"
        style="height: 32px; vertical-align: middle; margin-right: 10px;"
      />
      OVH Cloud Clusters
    </h1>

    <Banner
      v-if="error"
      color="error"
      :label="error"
    />

    <Banner
      v-if="!$fetchState.pending && clusters.length === 0 && !error"
      color="info"
      label="No OVH Cloud clusters found. Create one using the ClusterClass ovhcloud-rke2."
    />

    <table
      v-if="clusterRows.length > 0"
      class="sortable-table"
    >
      <thead>
        <tr>
          <th>Name</th>
          <th>Namespace</th>
          <th>Phase</th>
          <th>Version</th>
          <th>Region</th>
          <th>CP</th>
          <th>Workers</th>
          <th>Ready</th>
          <th>Age</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in clusterRows"
          :key="row.name"
        >
          <td>{{ row.name }}</td>
          <td>{{ row.namespace }}</td>
          <td>
            <span :class="['badge', phaseColor(row.phase)]">{{ row.phase }}</span>
          </td>
          <td>{{ row.version }}</td>
          <td>{{ row.region }}</td>
          <td>{{ row.cpReplicas }}</td>
          <td>{{ row.workerReplicas }}</td>
          <td>{{ row.ready ? 'Yes' : 'No' }}</td>
          <td>{{ row.age }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.badge {
  padding: 3px 8px;
  border-radius: 3px;
  color: white;
  font-size: 0.85em;
}
.bg-success { background: #4CAF50; }
.bg-warning { background: #FF9800; }
.bg-error { background: #F44336; }
.bg-info { background: #2196F3; }
</style>
