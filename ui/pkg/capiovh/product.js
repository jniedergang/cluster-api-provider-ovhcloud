const PRODUCT = 'manager';

export function init(plugin, store) {
  const {
    virtualType,
    basicType,
  } = plugin.DSL(store, PRODUCT);

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

  basicType(['capiovh-dashboard']);
}
