import * as pulumi from '@pulumi/pulumi';

const config = new pulumi.Config('t013');
const include = config.requireBoolean('includeResource');
const sentinel = config.require('deleteSentinel');

class RetentionProvider {
  async create(inputs) { return { id: 't013-retention-fixture', outs: inputs }; }
  async diff(_id, olds, news) { return { changes: JSON.stringify(olds) !== JSON.stringify(news) }; }
  async update(_id, _olds, news) { return { outs: news }; }
  async delete(_id, props) {
    const { appendFileSync } = await import('node:fs');
    appendFileSync(props.sentinel, 'provider-delete\n', { encoding: 'utf8', mode: 0o600 });
  }
}

class RetentionResource extends pulumi.dynamic.Resource {
  constructor(name, props, options) { super(new RetentionProvider(), name, props, options); }
}

// Phase A keeps this state object and persists the retention option. Phase B
// omits it: Pulumi forgets it under a retained delete and must not call delete.
if (include) new RetentionResource('t013RetentionFixture', { marker: 'T-013', sentinel }, { retainOnDelete: true });
