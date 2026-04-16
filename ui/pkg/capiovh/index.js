console.log('[CAPIOVH] Extension loading...');

export default function(plugin) {
  console.log('[CAPIOVH] Plugin init called', plugin);
  plugin.addProduct(require('./product'));
  console.log('[CAPIOVH] Product added');
}
