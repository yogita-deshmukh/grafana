import { runToolkit } from './helpers';

describe('plugin:build', () => {
  it(
    'works',
    () =>
      runToolkit({
        argv: ['plugin:build'],
        fixture: 'plugin',
      }),
    30000
  );
});
