import { importTypes } from '@rancher/auto-import';
import { IPlugin } from '@shell/core/types';
import capiovhRouting from './routes/capiovh-routing';

export default function(plugin: IPlugin): void {
  importTypes(plugin);

  plugin.metadata = require('./package.json');

  plugin.addProduct(require('./config/capiovh'));

  plugin.addRoutes(capiovhRouting);
}
