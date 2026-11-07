import type { E2EConfig } from 'e2e';
import { web } from '@e2e-dev/web';

export default {
  targets: [
    {
      name: 'web',
      engine: web({ browser: 'chromium' }),
      app: {
        url: 'http://127.0.0.1:18199',
        // start-app.sh builds the Go binary and serves it on a throwaway temp DB.
        command: { executable: 'bash', args: ['scripts/start-app.sh'], startupTimeout: 180_000, log: '.e2e/logs/app.log' },
      },
    },
  ],
  workers: 1,
  retries: 0,
  trace: 'retain-on-failure',
  output: '.e2e',
} satisfies E2EConfig;
