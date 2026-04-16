import Vue from 'vue';

console.log('[CAPIOVH] Extension loading, Vue version:', Vue.version);

export default function(plugin) {
  console.log('[CAPIOVH] Plugin init', plugin);
  plugin.addProduct(require('./product'));
}
