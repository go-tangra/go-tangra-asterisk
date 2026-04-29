import type { TangraModule } from './sdk';

import routes from './routes';
import { useAsteriskCdrStore } from './stores/asterisk-cdr.state';
import { useAsteriskStatsStore } from './stores/asterisk-stats.state';
import enUS from './locales/en-US.json';

const asteriskModule: TangraModule = {
  id: 'asterisk',
  version: '1.0.0',
  routes,
  stores: {
    'asterisk-cdr': useAsteriskCdrStore,
    'asterisk-stats': useAsteriskStatsStore,
  },
  locales: {
    'en-US': enUS,
  },
};

export default asteriskModule;
