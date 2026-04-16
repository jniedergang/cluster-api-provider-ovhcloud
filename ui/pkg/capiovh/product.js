export function init(plugin, store) {
  const {
    product,
    basicType,
    virtualType,
  } = plugin.DSL(store, 'capiovh');

  product({
    inStore:             'management',
    icon:                'cluster',
    label:               'OVH Cloud',
    removable:           false,
    showClusterSwitcher: false,
  });

  virtualType({
    label:   'Clusters',
    name:    'capiovh-clusters',
    icon:    'cluster',
    weight:  100,
  });

  basicType(['capiovh-clusters']);
}
