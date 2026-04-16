import { CAPI as RANCHER_CAPI } from '@shell/config/types';
import { CAPIOVH } from '../types/capiovh';

const CLUSTER_MGMT_PRODUCT = 'manager';

export function init($plugin: any, store: any) {
  const {
    basicType,
    virtualType,
    weightGroup,
  } = $plugin.DSL(store, CLUSTER_MGMT_PRODUCT);

  virtualType({
    label:      'OVH Cloud',
    icon:       'cluster',
    name:       'capiovh-dashboard',
    namespaced: false,
    weight:     98,
    route:      {
      name:   'c-cluster-manager-capiovh',
      params: { cluster: '_' }
    },
    overview: true,
    exact:    true,
  });

  basicType([
    'capiovh-dashboard',
    CAPIOVH.OVH_CLUSTER,
    CAPIOVH.OVH_MACHINE,
  ], 'OVHCloud');

  weightGroup('OVHCloud', 9, true);
}
